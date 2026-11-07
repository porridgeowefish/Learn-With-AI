import { parseSnapshot } from './snapshot.mjs';
import { zipSync, unzipSync, strToU8, strFromU8 } from '/__design/fflate.mjs';

const snapshotName = 'lll-design-workspace.json';
const operations = new Map();
const defaults = {
  appearance: {theme:'lychee-paper'},
  teacher: {autoNotes:{enabled:true, everyTurns:3, providerId:'', model:''}},
  'ask-ai': {default:'preview',searchEngine:'google',bindings:{teacher:{providerId:'preview'}},
    providers:[{id:'preview',kind:'openai',name:'设计演示连接',baseURL:'',apiKey:'',model:'demo'}]},
};

const placeholders = new Set([
  '[图片未提取到文字。请告知学习者：当前仅支持文字提取，未识别到文字；无法判断图片内容。]',
  '[音频未识别到语音文字。请告知学习者并邀请重新录音。]',
]);
function portableLearning(learning) {
  const copy = structuredClone(learning);
  copy.media = {};
  for (const row of copy.sources || []) {
    delete row.file;
    row.source.originalUnavailable = true;
    delete row.source.retrieval;
    const rev = row.revision;
    rev.original = {sha256:rev.original?.sha256};
    rev.originalUnavailable = true;
    delete rev.parseTaskId;
    rev.privacy = {cloudDisclosureAccepted:false};
    rev.derivedFiles = (rev.derivedFiles || []).filter(f=>/(?:^|\/)(?:content\.md|structure\.json)$/.test(f.path));
  }
  for (const message of [...(copy.messages || []),...(copy.queue || [])]) {
    for (const field of ['blocks','mediaBlocks']) {
      if (!Array.isArray(message[field])) continue;
      message[field] = message[field].flatMap(block=>{
        if (!['chat-image','chat-audio','attachment'].includes(block.type)) return [block];
        return block.source?.trim() && !placeholders.has(block.source) ? [{id:block.id,type:'markdown',source:block.source}] : [];
      });
    }
  }
  return copy;
}
function exportSnapshot(state) {
  const snapshot = {version:1,projects:state.projects,folders:state.folders,activity:state.activity,
    learning:Object.fromEntries(state.projects.filter(p=>state.learning?.[p.slug]).map(p=>[p.slug,portableLearning(state.learning[p.slug])])),
    rewards:state.rewards || []};
  const manifest = JSON.stringify(snapshot);
  return new Response(zipSync({[snapshotName]:strToU8(manifest)}), {headers:{'Content-Type':'application/zip'}});
}
async function importSnapshot({state, request, json}) {
  const id = request.headers.get('X-LLL-Operation');
  operations.set(id, true);
  try {
    const bytes = await request.arrayBuffer();
    const limit = 64 * 1024 * 1024;
    let size = 0;
    if (bytes.byteLength > limit) throw new Error('预览支持 64 MB 以内的记录包。');
    const files = unzipSync(new Uint8Array(bytes), {filter(entry) {
      size += entry.originalSize;
      if (size > limit) throw new Error('记录包超出预览容量。');
      return entry.name === snapshotName;
    }});
    if (!files[snapshotName]) throw new Error('请选择由设计预览导出的学习记录 ZIP。');
    const data = parseSnapshot(strFromU8(files[snapshotName]));
    await new Promise(resolve=>setTimeout(resolve,650));
    if (!operations.get(id)) return json({cancelled:true},409);
    const names = new Map();
    for (const [source, workspace] of [['current',state],['incoming',data]]) {
      for (const p of workspace.projects) {
        const location = {source,projectSlug:p.slug,projectTitle:p.title,title:p.title};
        const entries = [['project',location]];
        if (p.projectType === 'discipline-map') {
          for (const topic of workspace.learning?.[p.slug]?.topics || []) {
            entries.push(['topic',{...location,title:topic.title,topicId:topic.id,chapter:topic.chapterTitle}]);
          }
        }
        for (const [kind,loc] of entries) {
          const key = kind + '/' + loc.title.trim();
          if (!names.has(key)) names.set(key,{kind,locations:[]});
          names.get(key).locations.push(loc);
        }
      }
    }
    const conflicts = [...names.values()].filter(group=>group.locations.length>1 && group.locations.some(loc=>loc.source==='incoming'));
    if (conflicts.length) return json({conflicts},409);
    const slugs = [...state.projects,...data.projects].map(p=>p.slug);
    if (new Set(slugs).size !== slugs.length) throw new Error('项目标识重复，请核对导入包。');
    const folders = (data.folders || []).filter(f=>!state.folders.some(other=>other.id===f.id));
    const imported = Object.fromEntries(data.projects.filter(p=>Object.hasOwn(data.learning || {},p.slug)).map(p=>[p.slug,portableLearning(data.learning[p.slug])]));
    const patch = {projects:[...state.projects,...data.projects],learning:{...state.learning,...imported},
      folders:[...state.folders,...folders],activity:[...state.activity,...data.activity],
      rewards:[...(state.rewards || []),...(data.rewards || []).filter(r=>!state.rewards?.some(other=>other.id===r.id))]};
    Object.assign(state,patch);
    return json({ok:true});
  } catch (err) {
    return json({error:err.message || '无法读取 ZIP。'},400);
  } finally {
    operations.delete(id);
  }
}
export async function settings(ctx) {
  const {url,path,method,request,state,json} = ctx;
  const key = path.replace('/api/settings/','');
  if (path === '/api/settings/learning') {
    state.settings ||= {};
    const saved = state.settings.learning ||= {length:'medium',effort:'medium',showReasoning:true,memoryEnabled:false,search:{enabled:false,provider:'',providerId:'',hasKey:false}};
    if (method === 'PUT') {
      const input = await request.json();
      if (input.search.enabled && !((input.search.provider==='chatgpt' && input.search.providerId) || (['zhipu','deepseek','yuanbao'].includes(input.search.provider) && (input.search.apiKey || (saved.search.provider===input.search.provider && saved.search.hasKey))) || (input.search.provider==='zhipu-plan' && (state.settings['ask-ai']?.providers || []).some(p=>p.baseURL?.startsWith('https://open.bigmodel.cn/api/coding/') || p.baseURL==='https://open.bigmodel.cn/api/anthropic')))) {
        saved.search.enabled=false;
        return json({error:'请完成搜索配置后再开启'},400);
      }
      input.search.hasKey=!!input.search.apiKey || (input.search.provider===saved.search.provider && saved.search.hasKey) || input.search.provider==='zhipu-plan';
      delete input.search.apiKey;
      state.settings.learning=input;
    }
    const providers = state.settings['ask-ai']?.providers || defaults['ask-ai'].providers;
    if (!state.settings.learning.search.provider) { const plan=providers.find(p=>p.baseURL?.startsWith('https://open.bigmodel.cn/api/coding/') || p.baseURL==='https://open.bigmodel.cn/api/anthropic'); if(plan) state.settings.learning.search={enabled:true,provider:'zhipu-plan',providerId:plan.id,hasKey:true}; }
    return json({settings:state.settings.learning,capabilities:Object.fromEntries(providers.map(p=>[p.id,['low','medium','high','xhigh']]))});
  }
  if (path.startsWith('/api/learning-memories')) {
    state.memories ||= [];
    const id = decodeURIComponent(path.split('/')[3] || '');
    if (method === 'DELETE') { state.forgottenMemories ||= []; state.forgottenMemories.push(...state.memories.filter(m=>id==='all'||m.id===id).map(m=>m.evidence)); state.memories=state.memories.filter(m=>id!=='all'&&m.id!==id); }
    if (method === 'PUT') {const input=await request.json();const m=state.memories.find(m=>m.id===id);if(m){m.text=input.text;m.edited=true;}}
    return json({memories:state.memories});
  }

  if (Object.hasOwn(defaults,key)) {
    state.settings ||= {};
    if (method === 'PUT') state.settings[key] = await request.json();
    const value = structuredClone(state.settings[key] || defaults[key]);
    if (key==='ask-ai') { for(const p of value.providers) { if(p.id==='preview'&&!p.baseURL)p.baseURL='https://preview.invalid'; } }
    return json(value);
  }
  if (path === '/api/preferences') {
    if (method === 'PUT') state.preferences = await request.text();
    return json({ok:true,path:'preferences.md',content:state.preferences ?? '请先用例子解释，再给出定义。',maxBytes:65536});
  }
  if (path === '/api/health') return json({ok:true,workspace:state.workspace || '/设计预览/Learn-With-AI',stats:{projects:state.projects.length}});
  if (path.startsWith('/api/system/update')) {
    state.update ||= {current:'0.8.14',latest:'0.8.15',available:true,canInstall:true,phase:'idle',downloadedBytes:0,totalBytes:48000000,
      notesUrl:'https://gitee.com/szu-andrew/learn-with-ai/releases/tag/v0.8.15',
      downloadUrl:'https://gitee.com/szu-andrew/learn-with-ai/releases/download/v0.8.15/learn-with-ai-0.8.15-windows-amd64.msi',
      releaseNotes:'设计预览：以 Gitee 为更新源，支持检查、下载、取消与安装交接。这里不会下载或执行安装程序。'};
    const update=state.update;
    if (path.endsWith('/start')) { update.autoInstall=true; update.error=''; update.phase=update.phase==='ready'?'installing':'downloading'; }
    if (path.endsWith('/prepare') && !['ready','installing'].includes(update.phase)) update.phase='downloading';
    if (path.endsWith('/cancel')) {update.phase='idle';update.downloadedBytes=0;}
    if (path.endsWith('/install') && update.phase==='ready') update.phase='installing';
    if (method==='GET' && update.phase==='downloading') {
      update.downloadedBytes=Math.min(update.totalBytes,update.downloadedBytes+12000000);
      if(update.downloadedBytes===update.totalBytes) update.phase=update.autoInstall?'installing':'ready';
    }
    return json(update);
  }
  if (path === '/api/system/speech') return json({languages:['zh-CN','en-US'],available:true,setupHint:'设计预览仅返回示例转写，不识别真实录音。'});
  if (path === '/api/model-connections/models') return json({models:[{id:'demo',contextWindowTokens:32000}],source:'preset',verifiedAt:new Date().toISOString()});
  if (key === 'ask-ai/probe') return json({ok:true});
  if (key === 'siwc') return json({connected:!!state.siwc,pending:false,email:'设计演示账户'});
  if (key === 'siwc/models') return json({models:['demo']});
  if (key === 'siwc/start' || key === 'siwc/disconnect') {
    state.siwc = key === 'siwc/start';
    return json(state.siwc ? {authorizationUrl:'https://auth.openai.com/design-preview'} : {ok:true});
  }
  if (path === '/api/usage/projects') return json({projects:state.projects.map((p,i)=>({projectSlug:p.slug,title:p.title,
    inputTokens:1200*(i+1),outputTokens:850*(i+1),noteInputTokens:340*(i+1),noteOutputTokens:105*(i+1),
    ...(i%3===0?{cacheReadTokens:800*(i+1),cacheWriteTokens:120*(i+1)}:i%3===1?{cacheReadTokens:0}:{} )}))});
  if (path === '/api/usage/teacher') return json({page:Number(url.searchParams.get('page')) || 1,pageSize:20,total:0,conversations:[]});
  if (path.startsWith('/api/treasures')) {
    state.rewards ??= [{id:'preview-welcome',itemId:'cockle',kind:'teacher',projectSlug:'math',projectId:'math',projectTitle:'线性代数',
      sourceId:'preview',messageId:'',createdAt:new Date().toISOString(),isNew:false,seen:true,welcome:true}];
    if (path === '/api/treasures') return json({catalog:await (await fetch('/__design/treasures.json')).json(), rewards:state.rewards});
    const [,id,action] = path.match(/\/rewards\/([^/]+)\/(source|seen)$/) || [];
    const reward = state.rewards.find(r=>r.id===id);
    if (!reward) return json({error:'找不到记录'},404);
    if (action==='seen') {reward.seen=true; reward.isNew=false; return json({ok:true});}
    return json({available:true,url:'/project/'+reward.projectSlug,content:'第一次学习留下的纪念。'});
  }
  if (path === '/api/workspace/directories') return json({cancelled:true});
  if (path === '/api/workspace/move') {
    state.workspace = (await request.json()).parent + '/Learn-With-AI';
    return json({path:state.workspace});
  }
  if (path === '/api/workspace/export') return exportSnapshot(state);
  if (path === '/api/workspace/import') return importSnapshot(ctx);
  if (path === '/api/workspace/import/cancel') {
    const id = request.headers.get('X-LLL-Operation');
    if (operations.has(id)) operations.set(id,false);
    return json({ok:true});
  }
}
