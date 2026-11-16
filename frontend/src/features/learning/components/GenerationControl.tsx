import { useEffect, useId, useRef, useState } from 'react';
import { defaultProjectModel, ProjectModelSelect, useProjectModelOptions } from '@/features/settings';
import { Modal } from '@/shared/primitive/Modal';
import { Button } from '@/shared/primitive/Button';
import { MarkdownView } from '@/shared/primitive/MarkdownView';
import { describeGenerationError } from '../api/generationError';
import { useSources } from '../api/learningWorkspace';
import { isGenerationActive, useGenerationAction, useGenerationCandidate, useGenerationJobs, type GenerationKind } from '../api/generation';
import { SourceImportControl } from './SourceImportControl';
import { readConversationModel } from '../teacher/modelSelection';
import s from './GenerationControl.module.css';

const names = {review: '复习稿', practice: '练习题', 'discipline-map': '地图'};
const phases = {waiting:'排队中',preparing:'准备中',generating:'生成中',validating:'校验中',publishing:'保存中',done:'完成'};
const finished = {succeeded:'已完成',failed:'生成失败',cancelled:'已取消',interrupted:'已中断'};
export function GenerationControl({slug, kind, initialProviderId, disabled = false}: {slug:string;kind:GenerationKind;initialProviderId?:string;disabled?:boolean}) {
 const [open,setOpen]=useState(false), [count,setCount]=useState(10), [uploading,setUploading]=useState(false);
 const map = kind === 'discipline-map';
 const modelId = useId();
 const [mapProvider,setMapProvider]=useState(initialProviderId || defaultProjectModel);
 const modelOptions=useProjectModelOptions(open && map,mapProvider);
 const sources=useSources(slug), jobs=useGenerationJobs(slug), action=useGenerationAction(slug);
 const readySources=(sources.data??[]).filter(item=>item.status==='ready');
 const parsingSources=(sources.data??[]).filter(item=>item.status==='processing'||item.status==='stored');
 const operation=useRef<{intent:string;id:string}>();
 const candidates=[...(jobs.data?.jobs??[])].filter(item=>item.kind===kind).sort((a,b)=>b.updatedAt.localeCompare(a.updatedAt));
 const job=candidates.find(isGenerationActive)??candidates[0];
 const active=Boolean(job&&isGenerationActive(job));
 const [candidateOpen,setCandidateOpen]=useState(false);
 const candidate=useGenerationCandidate(slug,job?.id,candidateOpen);
 useEffect(()=>{setCandidateOpen(false);},[slug,kind,job?.id]);
 const exportCandidate=()=>{
  if(!candidate.data)return;
  const url=URL.createObjectURL(new Blob([candidate.data.content],{type:'text/plain;charset=utf-8'}));
  const anchor=document.createElement('a');anchor.href=url;anchor.download=`map-draft-${job?.id}.txt`;anchor.click();URL.revokeObjectURL(url);
 };
 useEffect(()=>{if(open){operation.current=undefined;action.reset();setMapProvider(initialProviderId || defaultProjectModel);}},[open,initialProviderId]);
 const submit=()=>{
  if(map&&!modelOptions.canUse)return;
  const mapSources=readySources.slice(0,32).map(item=>item.sourceId);
  const providerId = map ? mapProvider : readConversationModel(slug);
  const request={kind,...(providerId?{providerId}:{}),...(kind==='practice'?{questionCount:count}:{}),...(kind==='discipline-map'?{mapMode:mapSources.length?'sources' as const:'general' as const,sourceIds:mapSources}:{})};
  const intent=JSON.stringify(request);
  if(operation.current?.intent!==intent)operation.current={intent,id:crypto.randomUUID()};
  action.mutate({request:{...request,operationId:operation.current.id}},{onSuccess:()=>setOpen(false)});
 };
 const measured=job&&!job.progress?.indeterminate&&Number(job.progress?.totalUnits)>0;
 return <div className={s.control}>
  {!active&&<Button size="sm" disabled={disabled} onClick={()=>setOpen(true)}>生成{names[kind]}</Button>}
  {job&&(job.status==='queued'||job.status==='running'?<div className={s.loading}>
   <div className={s.progress} role="status">
    <strong>{names[kind]} · {phases[job.phase]}</strong>
    <progress aria-label={`${names[kind]}处理进度`} max={measured?job.progress.totalUnits:undefined} value={measured?job.progress.completedUnits:undefined}/>
    {measured&&<small>{job.progress.completedUnits}/{job.progress.totalUnits} {job.progress.unitLabel}</small>}
   </div>
   <Button size="sm" variant="ghost" disabled={action.isPending} onClick={()=>action.mutate({id:job.id,action:'cancel'})}>取消生成</Button>
  </div>:<div className={s.result} data-status={job.status}>
   <div className={s.progress} role="status">
    <strong>{finished[job.status]}</strong>
    {job.errorCode&&<small>{job.validation?.message??describeGenerationError(job.errorCode)}</small>}
    {job.status==='succeeded'&&job.checkpoint?.hasMoreConversation&&<small>本批已保存，还有对话可继续{kind==='practice'?'生成下一轮':'总结'}。</small>}
   </div>
   {(job.status==='failed'||job.status==='interrupted')&&<Button size="sm" variant="ghost" disabled={disabled||action.isPending} onClick={()=>action.mutate({id:job.id,action:'retry',operationId:crypto.randomUUID()})}>重试</Button>}
   {job.candidateAvailable&&<Button size="sm" variant="ghost" onClick={()=>setCandidateOpen(true)}>查看失败草稿</Button>}
  </div>)}
  {!open&&action.isError&&<span role="alert">{describeGenerationError(action.error)}</span>}
  <Modal open={open} onOpenChange={setOpen} title={`生成${names[kind]}`} description={kind==='review'?'从上次保存的位置继续总结，新内容融入原稿。':kind==='practice'?'生成独立的新一轮练习，旧轮次保留，可分页阅读。':'有参考结构可以上传附件；没有要求可直接生成。生成期间旧地图仍可阅读。'} footer={<Button variant="primary" onClick={submit} loading={action.isPending} disabled={active||uploading||(map&&(!modelOptions.canUse||Boolean(parsingSources.length)))}>{action.isPending?'提交中…':'开始生成'}</Button>}>
   <div className={s.fields}>
    {kind==='practice'&&<label>题数<input type="number" min={1} max={50} value={count} onChange={event=>setCount(Math.max(1,Math.min(50,Number(event.target.value)||1)))}/></label>}
    {map&&<><div className={s.modelField}><ProjectModelSelect id={modelId} value={mapProvider} onChange={setMapProvider} options={modelOptions}/></div><div className={s.referenceBox}><strong>参考结构（选填）</strong><p>如果有期望的参考结构，可以上传附件指引地图。没有参考要求，略过不填即可。</p><SourceImportControl slug={slug} onBusyChange={setUploading}/>{readySources.length>0&&<p>将自动参考 {Math.min(readySources.length,32)} 份已解析附件。{readySources.length>32?'单次最多参考 32 份。':''}</p>}{parsingSources.length>0&&<p role="status">{parsingSources.length} 份附件正在解析，完成后即可生成。</p>}</div></>}
    <small>系统模型使用你的账号模型额度。自配 API 或已登录的模型服务使用对应服务的额度。</small>
    {action.isError&&<p role="alert">{describeGenerationError(action.error)}</p>}
   </div>
  </Modal>
  <Modal open={candidateOpen} onOpenChange={setCandidateOpen} title="失败草稿" description="已保留这次模型返回的正文，查看和导出不会调用模型。草稿未替换正式地图。" size="lg" footer={<Button disabled={!candidate.data} onClick={exportCandidate}>导出原始返回</Button>}>
   {job?.validation&&<p role="status">{job.validation.message}</p>}
   {candidate.isLoading?<p>正在读取本地草稿…</p>:candidate.isError?<p role="alert">草稿读取失败，请重试。<button type="button" onClick={()=>void candidate.refetch()}>重试读取</button></p>:candidate.data&&<div className={s.draft}>{candidate.data.overviewMarkdown?<MarkdownView source={candidate.data.overviewMarkdown}/>:<pre>{candidate.data.content}</pre>}</div>}
  </Modal>
 </div>;
}
