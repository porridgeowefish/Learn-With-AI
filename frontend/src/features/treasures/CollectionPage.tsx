import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { readSource, thumbnail, tierColors, tierNames, useCollection, type TreasureReward } from './api';
import s from './Treasures.module.css';
export function SourceLink({reward}:{reward:TreasureReward}){
 const navigate=useNavigate();const [checking,setChecking]=useState(false);const source=useQuery({queryKey:['treasure-source',reward.id],queryFn:()=>readSource(reward.id),staleTime:0});
 const open=async()=>{setChecking(true);try{const result=await source.refetch();if(result.data?.available)navigate(result.data.url);}finally{setChecking(false);}};
 return <div className={s.source}><small>{new Date(reward.createdAt).toLocaleDateString()} · {reward.kind==='feynman'?'讲给教师听':'教师对话'}</small><button disabled={checking||source.isLoading||source.data?.available===false} onClick={()=>void open()}>{source.data?.available===false?'原对话已不可用':source.isError?'来源读取失败，点击重试':reward.projectTitle+' ↗'}</button>{source.data?.available===false&&<small>宝物仍为你保留</small>}</div>;
}
export function CollectionPage(){
 const query=useCollection();const [kind,setKind]=useState('all'),[tier,setTier]=useState(0),[search,setSearch]=useState(''),[selected,setSelected]=useState('');
 const counts=useMemo(()=>{const result=new Map<string,number>();for(const r of query.data?.rewards??[])result.set(r.itemId,(result.get(r.itemId)??0)+1);return result;},[query.data]);
 const items=(query.data?.catalog??[]).filter(i=>(kind==='all'||i.kind===kind)&&(!tier||i.tier===tier)&&`${i.name} ${i.english}`.toLowerCase().includes(search.trim().toLowerCase()));
 useEffect(()=>{if(items.length&&!items.some(i=>i.id===selected))setSelected(items.find(i=>counts.has(i.id))?.id??items[0].id);},[items,selected,counts]);
 const chosen=items.find(i=>i.id===selected);const owned=chosen?counts.get(chosen.id)??0:0;const history=(query.data?.rewards??[]).filter(r=>r.itemId===chosen?.id).slice().reverse();
 if(query.isLoading)return <div className={s.loading}>正在打开你的收藏…</div>;
 if(query.isError)return <div className={s.loading} role="alert">收藏暂时无法读取，现有宝物不会丢失。<button onClick={()=>void query.refetch()}>重新读取</button></div>;
 return <section className={s.collection}>
  <header className={s.collectionHeader}>
   <div className={s.collectionIntro}><span className={s.eyebrow}>THE LITTLE DISCOVERIES</span><h1>我的收藏</h1><p>每一次思考，都有值得收藏的发现。</p><div className={s.progress}><span>已发现 <b>{counts.size}</b> / 50</span><progress value={counts.size} max={50} aria-label="收藏发现进度"/></div><Link className={s.faqLink} to="/collection/faq">如何发现宝物？ ↗</Link></div>
  <figure className={s.newtonQuote} aria-label="牛顿：真理的海洋">
   <blockquote cite="https://www.fitzmuseum.cam.ac.uk/index.php/explore-our-collection/highlights/context/stories-and-histories/sir-isaac-newton">
    <p lang="en" className={s.quoteEnglish}>“I do not know what I may appear to the world; but to myself I seem to have been only like a boy playing on the seashore, and diverting myself in now and then finding a smoother pebble or a prettier shell than ordinary, whilst the great ocean of truth lay all undiscovered before me.”</p>
    <p lang="zh-CN" className={s.quoteChinese}>“我不知道世人会怎样看待我；但在我自己看来，我不过像一个在海边玩耍的男孩，偶尔发现一颗比平常更光滑的卵石，或一枚更美丽的贝壳，便为之欣喜。而真理的浩瀚海洋，依然未经探索，展现在我面前。”</p>
   </blockquote>
   <figcaption><span className={s.newtonSignature}>Isaac Newton</span><span className={s.newtonCredit}>艾萨克·牛顿<span aria-hidden="true"> · </span><a href="https://www.fitzmuseum.cam.ac.uk/index.php/explore-our-collection/highlights/context/stories-and-histories/sir-isaac-newton" target="_blank" rel="noopener noreferrer">引文出处 ↗</a></span></figcaption>
  </figure>
  </header>
  <div className={s.filters}><nav aria-label="藏品种类">{[['all','全部'],['gem','宝石'],['shell','贝壳']].map(([key,label])=><button className={kind===key?s.tabActive:''} key={key} onClick={()=>setKind(key)} aria-pressed={kind===key}>{label}</button>)}</nav><input aria-label="搜索藏品" placeholder="⌕  搜索藏品" value={search} onChange={e=>setSearch(e.target.value)}/><div className={s.tierFilters}>{['全部',...tierNames].map((name,i)=><button key={name} aria-pressed={tier===i} className={tier===i?s.filterActive:''} onClick={()=>setTier(i)}>{name}</button>)}</div></div>
  {!counts.size&&<div className={s.firstDiscovery}>你的第一件宝物，或许就在下一次理解中。与教师讨论，也可以把学到的内容讲给教师听。<Link to="/">去学习 →</Link></div>}
  <div className={s.collectionBody}><div className={s.grid}>{items.map(item=>{const count=counts.get(item.id)??0;return <button key={item.id} className={`${s.specimen} ${selected===item.id?s.selected:''} ${count?'':s.locked}`} onClick={()=>setSelected(item.id)} aria-pressed={selected===item.id} aria-label={`${item.name} · ${count?'已收藏 '+count+' 件':'尚未发现'}`}>
   {!count&&<span className={s.lock} aria-hidden="true"><svg width="14" height="16" viewBox="0 0 14 16" fill="none"><rect x="2" y="7" width="10" height="8" rx="2" stroke="currentColor"/><path d="M4 7V4a3 3 0 016 0v3" stroke="currentColor"/></svg></span>}<img src={thumbnail(item.id)} alt="" loading="lazy"/><span className={s.tier} style={{color:tierColors[item.tier-1]}}>● {tierNames[item.tier-1]}</span><strong>{item.name}</strong><span>{count?`× ${count}`:'尚未发现'}</span>
  </button>;})}{!items.length&&<p>没有找到这件藏品，试试其他名字或筛选条件。</p>}</div>
  {chosen&&<aside className={s.detail}><div className={`${s.detailImage} ${owned?'':s.locked}`}><img src={thumbnail(chosen.id)} alt={chosen.name}/></div><h2>{chosen.name} <small style={{color:tierColors[chosen.tier-1]}}>● {tierNames[chosen.tier-1]}</small></h2><p className={s.english}>{chosen.english}</p>{owned?<><p>已拥有 <b>{owned}</b> 件</p><Link className={s.primary} to={`/collection/${chosen.id}`}>✧ 展示这件宝物</Link><h3>发现的记忆</h3>{history.map(r=><SourceLink key={r.id} reward={r}/>)}</>:<div className={s.waiting}><h3>尚未发现</h3><p>在提问、讨论和讲解中，等待属于你的相遇。</p><Link to="/collection/faq">了解如何获得 →</Link></div>}</aside>}
  </div>
 </section>;
}
