import { useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { tierNames, useCollection, thumbnail, type TreasureItem } from './api';
import type { SceneHandle } from './rendering/scene.js';
import s from './Treasures.module.css';
const beachNames=['碧海白沙','落日金沙','月光荧海','火山黑沙','地中海秘湾','粉沙晨曦'];
const beachFiles=['01-lagoon','02-sunset','03-moonlight','04-volcanic','05-mediterranean','06-pink-sand'];
function GemStage({item,beach}:{item:TreasureItem;beach:number}){
 const host=useRef<HTMLDivElement>(null),handle=useRef<SceneHandle>();const [error,setError]=useState('');
 const beachRef=useRef(beach);beachRef.current=beach;
 useEffect(()=>{let stopped=false;setError('');void import('./rendering/scene.js').then(({mountScene})=>{if(!stopped&&host.current)handle.current=mountScene(host.current,item,beachRef.current,setError);}).catch(()=>{if(!stopped)setError('当前设备无法打开三维显示。');});return()=>{stopped=true;handle.current?.dispose();handle.current=undefined;};},[item.id]);
 useEffect(()=>{handle.current?.setBeach(beach);},[beach]);
 return <div className={s.scene} ref={host}>{error&&<div className={s.renderError}><img src={thumbnail(item.id)} alt={item.name}/><p role="alert">{error}</p><button onClick={()=>location.reload()}>重新打开</button></div>}</div>;
}
export function TreasureDisplay(){
 const {itemId}=useParams();const query=useCollection();const [beach,setBeach]=useState(0);const container=useRef<HTMLDivElement>(null);
 const item=query.data?.catalog.find(i=>i.id===itemId);const count=query.data?.rewards.filter(r=>r.itemId===itemId).length??0;
 if(query.isLoading)return <div className={s.loading}>正在打开宝物…</div>;
 if(query.isError)return <div className={s.loading}>暂时无法读取收藏。<button onClick={()=>void query.refetch()}>重试</button></div>;
 if(!item||!count)return <div className={s.loading}><h1>{item?.name??'这件宝物'}</h1><p>尚未发现。在学习途中，等待一次相遇。</p><Link to="/collection">返回收藏 →</Link></div>;
 return <section ref={container} className={s.display} style={{backgroundImage:`url(/treasures/references/${beachFiles[beach]}.png)`}}>
  <GemStage item={item} beach={beach}/>
  <header className={s.displayHeader}><Link to="/collection">← 返回收藏</Link><button onClick={()=>{if(document.fullscreenElement)void document.exitFullscreen();else void container.current?.requestFullscreen().catch(()=>{});}} aria-label="切换全屏">⛶</button></header>
  <div className={s.gemCaption}><h1>{item.name}</h1><p>{item.english.toUpperCase()}</p><span>{tierNames[item.tier-1]}</span><span>已收藏 ×{count}</span></div>
  <footer className={s.beachPicker}><div>{beachFiles.map((file,i)=><button aria-label={beachNames[i]} aria-pressed={i===beach} className={i===beach?s.chosenBeach:''} key={file} onClick={()=>setBeach(i)}><img src={`/treasures/references/${file}.png`} alt=""/></button>)}</div><p>换一片海</p><small>拖动欣赏 · 滚轮缩放</small></footer>
 </section>;
}
