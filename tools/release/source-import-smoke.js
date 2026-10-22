// Exercise the parsers in a packaged/installed runtime, never a real workspace.
const assert=require('node:assert/strict');
const fs=require('node:fs');
const os=require('node:os');
const path=require('node:path');
const net=require('node:net');
const http=require('node:http');
const crypto=require('node:crypto');
const {spawn}=require('node:child_process');

async function main(binary){
 require('./temporary-root').prepareTemporaryRoot();
 const fetch=(url,options={})=>globalThis.fetch(url,{...options,signal:AbortSignal.timeout(30000)});
 const scratch=fs.mkdtempSync(path.join(os.tmpdir(),'learn-source-smoke-'));
 const workspace=path.join(scratch,'workspace');fs.mkdirSync(workspace);
 const listener=net.createServer();await new Promise(r=>listener.listen(0,'127.0.0.1',r));
 const port=listener.address().port;await new Promise(r=>listener.close(r));
 const absent=path.join(scratch,'no-cli');
 const child=spawn(path.resolve(binary),['--no-browser','--workspace',workspace,'--port',String(port)],{cwd:scratch,windowsHide:true,env:{...process.env,WORKSPACE:workspace,LLL_WORKSPACE_ROOT:workspace,CLAUDE_BIN:absent,CODEX_BIN:absent,CODEBUDDY_BIN:absent,WORKBUDDY_BIN:absent,HERMES_BIN:absent,TRAE_BIN:absent},stdio:['ignore','pipe','pipe']});
 let log='',exited=false;child.stdout.on('data',b=>log+=b);child.stderr.on('data',b=>log+=b);child.on('exit',()=>exited=true);
 const base=`http://127.0.0.1:${port}`;
 let fake;
 const pause=()=>new Promise(r=>setTimeout(r,100));
 try{
  let health;const deadline=Date.now()+30000;
  while(Date.now()<deadline&&!exited){try{const r=await fetch(base+'/api/health');if(r.ok){health=await r.json();break}}catch{}await pause()}
  assert(health?.ok,log);assert.equal(path.resolve(health.workspace),workspace);
  const created=await fetch(base+'/api/projects',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({slug:'parser-smoke',title:'Parser smoke',projectType:'system-learning'})});
  assert.equal(created.status,201,await created.text());
  const url=base+'/api/projects/parser-smoke/sources';
  const fixtures=path.resolve(__dirname,'../../backend-go/internal/modules/sources/internal/parser/testdata');
  async function upload(name,bytes){
   const form=new FormData();form.set('operationId',crypto.randomUUID());form.set('file',new Blob([bytes]),name);
   return fetch(url,{method:'POST',body:form});
  }
  async function parsed(name,bytes,status='ready',failure){
   const response=await upload(name,bytes);assert.equal(response.status,201,name);
   const {source}=await response.json();let current;
   const end=Date.now()+15000;
   while(Date.now()<end){current=(await (await fetch(url+'/'+source.sourceId)).json()).source;if(current.status===status)break;if(current.status==='failed'&&status==='ready')throw Error(name+': '+current.failureCode);await pause()}
   assert.equal(current.status,status,name);if(failure)assert.equal(current.failureCode,failure,name);
   if(status==='ready'){
    assert(current.extractedBytes>0&&current.parser,name);
    const text=await (await fetch(url+'/'+source.sourceId+'/revisions/'+source.currentRevisionId+'/content')).text();assert(text.trim().length>0,name);
   }
   return current;
  }
  const pdf=await parsed('embedded-cjk.pdf',fs.readFileSync(path.join(fixtures,'embedded-cjk.pdf')));
  for(const name of ['lesson.docx','lesson.pptx','lesson.xlsx'])await parsed(name,fs.readFileSync(path.join(fixtures,name)));
  await parsed('notes.md',Buffer.from('# 笔记\n词法环境与闭包。'));
  await parsed('table.csv',Buffer.from('概念,说明\n闭包,捕获词法环境\n'));
  await parsed('textless.pdf',fs.readFileSync(path.join(fixtures,'textless.pdf')),'failed','no_extractable_text');
  await parsed('broken.pdf',Buffer.from('not PDF'),'failed','invalid_document');
  const exact=await upload('boundary.zip',Buffer.alloc(100*1024*1024));assert.equal(exact.status,201,'100MiB must be accepted');await exact.arrayBuffer();
  const large=await upload('too-large.md',Buffer.alloc(100*1024*1024+1));assert.equal(large.status,413,'100MiB+1 must be rejected');await large.arrayBuffer();
  const items=(await (await fetch(url)).json()).sources;assert.equal(items.length,9,'oversize file left a source');
  // The runtime runs outside the repository: these calls prove its shipped
  // prompt resources and embedded PDF renderer are usable after installation.
  const promptRoot=path.resolve(__dirname,'../../learning-prompts');
  const review='# Packaged review\n## Concepts\nLexical environments retain captured bindings.\n';
  const practice='## 第1题\n题型：single-choice · 难度：1/5\nA callback runs after its outer function returns. What happens to a captured binding?\nA. The callback retains its lexical environment.\nB. The binding disappears when the outer call returns.\n### 参考答案\n答案：A\n### 解析\nA is correct because the captured binding outlives the outer call. B confuses the outer call lifetime with the retained lexical environment.\n';
  const map={overviewMarkdown:'# Test discipline\n## 这门学科研究什么\nObjects and evidence.\n## 学科边界\nA bounded discipline.\n## 主要研究领域与知识架构\n',topics:{schemaVersion:1,topics:[]}};
  for(let chapter=1;chapter<=4;chapter++){
   const chapterTitle='领域'+chapter;map.overviewMarkdown+='### '+chapterTitle+'\n';
   for(let topic=1;topic<=2;topic++){
    const title='主题'+chapter+'-'+topic;map.overviewMarkdown+='#### '+title+'\nObjects, prerequisites and relationships.\n';
    map.topics.topics.push({id:`topic-${chapter}-${topic}`,title,priority:topic===1?'I':'II',chapterTitle,goal:'Explain',teachingOutline:'Definitions, evidence and boundaries',inScope:[title],outOfScope:[],prerequisites:[],ownedConcepts:[title],reusedConcepts:[]});
   }
  }
  map.overviewMarkdown+='## 领域之间如何关联\nRelationships.\n## 常用研究方法与证据\nMethods.\n## 典型应用\nApplications.\n';
  const received=[],fakeErrors=[];let rejectNextMap=false;
  fake=http.createServer(async(req,res)=>{
   try{
    assert.equal(req.url,'/v1/chat/completions');assert.equal(req.method,'POST');
    assert.equal(req.headers.authorization,'Bearer installed-smoke-placeholder');
    const chunks=[];let receivedBytes=0;
    for await(const chunk of req){receivedBytes+=chunk.length;assert(receivedBytes<16*1024*1024,'fake request too large');chunks.push(chunk)}
    const request=JSON.parse(Buffer.concat(chunks).toString('utf8'));received.push(request.model);
    const kind=request.model.replace('smoke-','');
    if(kind==='vision'){
     assert(!request.stream,'vision must use page extraction protocol');
     const image=request.messages.flatMap(m=>Array.isArray(m.content)?m.content:[]).find(b=>b.type==='image_url');
     assert(image?.image_url?.url.startsWith('data:image/png;base64,'),'renderer did not deliver a PNG');
     const png=Buffer.from(image.image_url.url.split(',')[1],'base64');
     assert(png.subarray(0,8).equals(Buffer.from([137,80,78,71,13,10,26,10])),'invalid PNG signature');
     assert(png.readUInt32BE(16)>0&&png.readUInt32BE(20)>0,'empty rendered page');
     res.setHeader('Content-Type','application/json');
     res.end(JSON.stringify({choices:[{message:{content:'## Installed vision result\nRendered PDF page.\n\n$$x^2+y^2=z^2$$\n'},finish_reason:'stop'}],usage:{prompt_tokens:11,completion_tokens:12}}));return;
    }
    assert(['review','practice','discipline-map'].includes(kind),'unexpected model/default fallback: '+request.model);
    if(kind==='discipline-map'){
      const input=JSON.parse(request.messages.find(m=>m.role==='user').content);
      if(input.mapMode==='sources')assert(input.sourceVerificationSamples?.some(sample=>sample.body.includes('没有 Markdown 目录')),'source map omitted OCR body');
    }
    const system=request.messages.find(m=>m.role==='system')?.content;assert.equal(typeof system,'string');
    const resources=['common',kind];
    if(kind==='review')resources.push(...['research_question_frame','prerequisite_scaffold','misconception','boundary_map','analogy','mece_decompose','first_principles','concept_graph'].map(n=>'primitives/'+n));
    if(kind==='practice')resources.push('primitives/transfer');
    for(const resource of resources)assert(system.includes(fs.readFileSync(path.join(promptRoot,resource+'.md'),'utf8').trim()),'packaged prompt missing '+resource);
    assert.equal(request.stream,true);
    res.setHeader('Content-Type','text/event-stream');
    const content=kind==='review'?review:kind==='practice'?practice:rejectNextMap?(rejectNextMap=false,'{invalid JSON map draft}'):JSON.stringify(map);
    res.end('data: '+JSON.stringify({choices:[{delta:{content},finish_reason:'stop'}]})+'\n\ndata: '+JSON.stringify({choices:[],usage:{prompt_tokens:25,completion_tokens:50}})+'\n\ndata: [DONE]\n\n');
   }catch(e){fakeErrors.push(e.message);res.statusCode=500;res.end('installed smoke fake rejected request')}
  });
  await new Promise(r=>fake.listen(0,'127.0.0.1',r));
  async function json(endpoint,body,method='POST',expected=202){
   const response=await fetch(base+endpoint,{method,headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
   const raw=await response.text();assert.equal(response.status,expected,endpoint+': '+raw);return JSON.parse(raw);
  }
  const fakeURL=`http://127.0.0.1:${fake.address().port}/v1`;
  const provider={id:'installed-smoke',name:'Installed smoke',kind:'openai',baseURL:fakeURL,apiKey:'installed-smoke-placeholder',model:'must-not-use-default'};
  const bindings=Object.fromEntries([['review','review'],['practice','practice'],['disciplineMap','discipline-map'],['documentStructure','vision']].map(([purpose,kind])=>[purpose,{providerId:provider.id,model:'smoke-'+kind}]));
  await json('/api/settings/ask-ai',{default:provider.id,providers:[provider],bindings},'PUT',200);
  async function completed(endpoint){
   let job;const deadline=Date.now()+60000;
   while(Date.now()<deadline){const response=await fetch(base+endpoint);assert.equal(response.status,200,endpoint);job=(await response.json()).job;if(!['queued','running'].includes(job.status))break;await pause()}
   assert.equal(job?.status,'succeeded',JSON.stringify({job,fakeErrors}));return job;
  }
  for(const kind of ['review','practice']){
   const endpoint='/api/projects/parser-smoke/generation-jobs';
   const {job}=await json(endpoint,{operationId:crypto.randomUUID(),kind,sourceIds:[],...(kind==='practice'?{questionCount:1}:{})});
   assert.equal(job.providerId,provider.id);assert.equal(job.model,'smoke-'+kind);
   const done=await completed(endpoint+'/'+job.id);assert.equal(done.resultRefs.length,1);
   const assetResponse=await fetch(base+'/api/projects/parser-smoke/assets/'+kind);assert.equal(assetResponse.status,200);
   assert((await assetResponse.text()).includes(kind==='review'?'Packaged review':'captured binding'),'generation did not publish asset');
  }
  await json('/api/projects',{slug:'map-smoke',title:'Map smoke',projectType:'discipline-map'},'POST',201);
  const mapEndpoint='/api/projects/map-smoke/generation-jobs';
  const {job:mapJob}=await json(mapEndpoint,{operationId:crypto.randomUUID(),kind:'discipline-map',mapMode:'general'});
  const mapDone=await completed(mapEndpoint+'/'+mapJob.id);
  const projection=await (await fetch(base+'/api/projects/map-smoke/discipline-map')).json();
  assert.equal(projection.mapVersionId,mapDone.resultRefs[0]);assert.equal(projection.topics.topics.length,8);
  assert(projection.topics.topics.every(topic=>['I','II','III'].includes(topic.priority)),'map priorities were not published');
  const {revision}=await (await fetch(url+'/'+pdf.sourceId)).json();
  const structureEndpoint='/api/projects/parser-smoke/sources/'+pdf.sourceId+'/structure-jobs';
  const {job:structureJob}=await json(structureEndpoint,{operationId:crypto.randomUUID(),revisionId:revision.revisionId,baseDerivedVersionId:revision.derivedVersionId,pages:[1]});
  const structureDone=await completed(structureEndpoint+'/'+structureJob.id);
  assert(structureDone.resultDerivedVersionId&&structureDone.resultDerivedVersionId!==revision.derivedVersionId,'enhancement did not publish a new derived version');
  const enhanced=await (await fetch(url+'/'+pdf.sourceId+'/revisions/'+revision.revisionId+'/content')).text();
  assert(enhanced.includes('Installed vision result'),'rendered page recognition was not published');
  // Plain OCR-like text does not have a chapter heading tree. It must still
  // seed a source-based map with frozen citations in the installed runtime.
  const plainForm=new FormData();plainForm.set('operationId',crypto.randomUUID());plainForm.set('file',new Blob(['目录\n二次函数与一元二次方程\n圆、相似和概率\n没有 Markdown 目录的识别正文。']),'ocr-notes.txt');
  const plainResponse=await fetch(base+'/api/projects/map-smoke/sources',{method:'POST',body:plainForm});assert.equal(plainResponse.status,201);
  const plain=(await plainResponse.json()).source;
  const plainURL=base+'/api/projects/map-smoke/sources/'+plain.sourceId;
  for(let i=0;i<150;i++){const current=(await (await fetch(plainURL)).json()).source;if(current.status==='ready')break;await pause()}
  const {job:plainMap}=await json(mapEndpoint,{operationId:crypto.randomUUID(),kind:'discipline-map',mapMode:'sources',sourceIds:[plain.sourceId]});
  const plainDone=await completed(mapEndpoint+'/'+plainMap.id);
  assert(plainDone.coverage.readRanges.length>0,'plain text not actually read');assert(plainDone.coverage.gap,'source coverage not stated');
  const erase=await fetch(plainURL+'/permanent-delete',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({confirmation:'PERMANENT_DELETE',operationId:crypto.randomUUID()})});assert.equal(erase.status,200);
  assert.equal((await erase.json()).status,'deleted');
  assert(!fs.existsSync(path.join(workspace,'projects','map-smoke','sources',plain.sourceId,'revisions')),'parsed versions survived deletion');
  assert((await (await fetch(base+'/api/projects/map-smoke/discipline-map')).json()).mapVersionId===plainDone.resultRefs[0],'delete removed generated map');
  if(process.platform==='darwin'||process.platform==='linux'||process.env.LLL_SMOKE_NATIVE_OCR==='1'){
    const capability=(await (await fetch(base+structureEndpoint)).json()).localOCR;
    assert(capability.available,JSON.stringify(capability));assert.equal(capability.platform,{darwin:'darwin',linux:'linux',win32:'windows'}[process.platform]);
    // Use genuine visible Chinese glyphs, not the geometric ToUnicode fixture.
    // Remove only the synthetic cloud OCR binding before uploading this page;
    // the automatically captured native provider must remain local.
    const nativeBindings={...bindings};delete nativeBindings.documentStructure;
    await json('/api/settings/ask-ai',{default:provider.id,providers:[provider],bindings:nativeBindings},'PUT',200);
    const nativeResponse=await upload('ocr-chinese.pdf',fs.readFileSync(path.join(fixtures,'ocr-chinese.pdf')));assert.equal(nativeResponse.status,201);
    const nativeSource=(await nativeResponse.json()).source;
    const nativeEndpoint='/api/projects/parser-smoke/sources/'+nativeSource.sourceId+'/structure-jobs';
    let native;const nativeDeadline=Date.now()+60000;
    while(Date.now()<nativeDeadline){const jobs=(await (await fetch(base+nativeEndpoint)).json()).jobs;native=jobs[0];if(native)break;await pause()}
    assert(native,'automatic native OCR was not created');
    const nativeDone=await completed(nativeEndpoint+'/'+native.id);
    assert.equal(nativeDone.providerId,capability.providerId);assert(nativeDone.resultDerivedVersionId,'native OCR published no result');assert.equal((nativeDone.emptyPages??[]).length,0,'readable Chinese page treated as blank');
    const current=(await (await fetch(url+'/'+nativeSource.sourceId)).json()).source;
    const nativeText=await (await fetch(url+'/'+nativeSource.sourceId+'/revisions/'+current.currentRevisionId+'/content')).text();
    assert(nativeText.length>20&&/中文|学习/.test(nativeText)&&/函数|方程/.test(nativeText)&&!nativeText.includes('Installed vision result'),'native OCR failed to recognize genuine Chinese: '+nativeText);

  }
  assert.deepEqual(fakeErrors,[]);assert.deepEqual(received,['smoke-review','smoke-practice','smoke-discipline-map','smoke-vision','smoke-discipline-map']);
  rejectNextMap=true;
  const {job:badMap}=await json(mapEndpoint,{operationId:crypto.randomUUID(),kind:'discipline-map',mapMode:'general'});
  let rejected;for(let i=0;i<150;i++){rejected=(await (await fetch(base+mapEndpoint+'/'+badMap.id)).json()).job;if(rejected.status==='failed')break;await pause()}
  assert.equal(rejected.status,'failed');assert.equal(rejected.validation.code,'json-format');assert.equal(rejected.candidateAvailable,true);
  const requestCount=received.length;
  const savedDraft=await fetch(base+mapEndpoint+'/'+badMap.id+'/candidate');assert.equal(savedDraft.status,200);
  assert.equal((await savedDraft.json()).content,'{invalid JSON map draft}');assert.equal(received.length,requestCount,'draft read called model');
  assert.equal((await (await fetch(base+'/api/projects/map-smoke/discipline-map')).json()).mapVersionId,plainDone.resultRefs[0],'invalid response replaced good map');
  const taskResponse=await fetch(base+'/api/projects/parser-smoke/assistant-tasks');assert.equal(taskResponse.status,404);
  const stopped=await fetch(base+'/api/system/shutdown',{method:'POST'});assert.equal(stopped.status,202);
  for(let i=0;i<50&&!exited;i++)await pause();assert(exited,'shutdown failed');
  console.log('Installed runtime: PDF/MD/PPTX/DOCX/XLSX/CSV, textless/malformed errors, exact 100MiB/oversize rejection, packaged review/practice/map prompts, embedded WASM PNG enhancement, no assistant passed');
 }finally{
  if(fake){fake.closeAllConnections();await new Promise(r=>fake.close(r))}
  if(!exited){child.kill();await new Promise(r=>{child.once('exit',r);setTimeout(r,5000)})}
  const relative=path.relative(os.tmpdir(),scratch);if(relative.startsWith('learn-source-smoke-')&&!relative.includes(path.sep))fs.rmSync(scratch,{recursive:true,force:true,maxRetries:10,retryDelay:100});
 }
}
main(process.argv[2]).catch(e=>{console.error(e.stack);process.exitCode=1});
