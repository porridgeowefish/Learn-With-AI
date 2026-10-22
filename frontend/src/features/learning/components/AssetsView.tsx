import { useEffect, useState } from 'react';
import { useAsset, useAssets, usePracticeRounds, useSaveAsset } from '../api/learningWorkspace';
import { PracticeReader } from './PracticeReader';
import { GenerationControl } from './GenerationControl';
import { NotesView } from './NotesView';
import { ReviewReader } from './ReviewReader';
import { FeynmanView } from './FeynmanView';
import { usePracticeResults, useRetakePractice, useSavePracticeResult } from '../api/practiceResults';
import s from './AssetsView.module.css';

type AssetKey = 'review' | 'practice';
type AssetView = AssetKey | 'notes' | 'feynman';
const savedView=(slug:string):AssetView=>{let saved:string|null=null;try{saved=localStorage.getItem(`lll.assets.view.${slug}`);}catch{/* storage unavailable */}return saved==='practice'||saved==='notes'||saved==='feynman'?saved:'review';};
export function AssetsView({slug}:{slug:string}) {
 const [view,setViewState]=useState<AssetView>(()=>savedView(slug));
 const setView=(next:AssetView)=>{setViewState(next);try{localStorage.setItem(`lll.assets.view.${slug}`,next);}catch{/* storage unavailable */}};
 const key:AssetKey=view==='practice'?'practice':'review';
 const [editing,setEditing]=useState(false);
 const [draft,setDraft]=useState('');
 const assets=useAssets(slug);
 const asset=useAsset(slug,key);
 const rounds=usePracticeRounds(slug,key==='practice');
 const [roundId,setRoundId]=useState<string|null>(null);
 const roundItems=rounds.data??[];
 const selectedRound=roundItems.find(item=>item.versionId===roundId)??roundItems[roundItems.length-1];
 const roundIndex=selectedRound?roundItems.indexOf(selectedRound):-1;
 const historical=key==='practice'&&selectedRound&&selectedRound.versionId!==asset.data?.meta.currentVersionId;
 const save=useSaveAsset(slug,key);
 const results=usePracticeResults(slug);
 const saveResult=useSavePracticeResult(slug);
 const retake=useRetakePractice(slug);
 const practiceVersion=selectedRound?.versionId??asset.data?.meta.currentVersionId??'';
 const roundResult=results.data?.[practiceVersion];
 useEffect(()=>{if(!editing)setDraft(asset.data?.content??'');},[asset.data?.content,editing]);
 useEffect(()=>{setViewState(savedView(slug));setEditing(false);setDraft('');setRoundId(null);},[slug]);
 useEffect(()=>setRoundId(null),[asset.data?.meta.currentVersionId]);
 const commit=()=>{if(!asset.data)return;save.mutate({baseEditRevision:asset.data.meta.editRevision,content:draft},{onSuccess:()=>setEditing(false)});};
 return <section className={s.assets}>
  <div className={s.assetTabs} data-tour="notes-tabs">
   {(assets.data??[]).map(meta=><button key={meta.key} className={view===meta.key?s.active:''} onClick={()=>{setView(meta.key);setEditing(false);}}>{meta.title}</button>)}
   <button className={view==='notes'?s.active:''} onClick={()=>{setView('notes');setEditing(false);}}>笔记</button>
   <button className={view==='feynman'?s.active:''} onClick={()=>{setView('feynman');setEditing(false);}}>讲给教师听</button>
  </div>
  {view==='notes'?<NotesView slug={slug}/>:view==='feynman'?<div className={s.layout}><article className={s.document}><FeynmanView slug={slug}/></article></div>:<div className={`${s.layout} ${key==='review'?s.withNotes:''}`}>
   <article className={s.document}>
    <header><div><span>笔记与复习</span><h2>{asset.data?.meta.title??'加载中…'}</h2></div><div className={s.actions}>
     <GenerationControl slug={slug} kind={key} disabled={editing}/>
     {editing?<><button onClick={()=>setEditing(false)}>取消</button><button className={s.primary} onClick={commit} disabled={save.isPending}>保存</button></>:<button onClick={()=>setEditing(true)} disabled={!asset.data||Boolean(historical)}>编辑</button>}
    </div></header>
    {asset.isError&&<div className={s.error} role="alert">内容读取失败，请重试。</div>}
    {save.isError&&<div className={s.error} role="alert">保存失败：内容可能已经被更新，请刷新后再编辑。</div>}
    {key==='practice'&&roundItems.length>0&&!editing&&<nav className={s.roundPager} aria-label="练习轮次"><button onClick={()=>setRoundId(roundItems[roundIndex-1].versionId)} disabled={roundIndex<=0}>上一轮</button><span>第 {roundIndex+1} / {roundItems.length} 轮{roundResult?` · 得分 ${roundResult.score}/${roundResult.total}`:''}</span><button onClick={()=>setRoundId(roundItems[roundIndex+1].versionId)} disabled={roundIndex>=roundItems.length-1}>下一轮</button></nav>}
    {key==='practice'&&rounds.isError&&<div className={s.error} role="alert">旧轮次读取失败，请刷新重试。当前轮仍可阅读。</div>}
    {editing?<textarea className={s.editor} value={draft} onChange={event=>setDraft(event.target.value)} spellCheck={false}/>:asset.data&&(asset.data.content||(key==='practice'&&selectedRound?.content))?(key==='review'?<ReviewReader slug={slug} content={asset.data.content} assetVersionId={asset.data.meta.currentVersionId}/>:<PracticeReader content={selectedRound?.content??asset.data.content} versionId={practiceVersion} result={roundResult} onSubmit={graded=>saveResult.mutateAsync({versionId:practiceVersion,...graded})} onRetake={()=>retake.mutateAsync(practiceVersion)}/>):<div className={s.empty}>这份内容还是空的。可以直接编辑，或使用上方按钮生成。</div>}
   </article>
  </div>}
 </section>;
}
