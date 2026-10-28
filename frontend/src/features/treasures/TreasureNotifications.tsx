import { useEffect, useRef, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { createPortal } from 'react-dom';
import { useQueryClient } from '@tanstack/react-query';
import { subscribeToSSE } from '@/app/hooks/useSSE';
import { SSE_EVENTS } from '@/shared/lib/constants';
import { acknowledge, collectionKey, thumbnail, tierNames, useCollection } from './api';
import s from './Treasures.module.css';

/** Uses the app's existing SSE. Persistent unseen rewards recover missed events. */
export function TreasureNotifications(){
 const query=useCollection(),client=useQueryClient(),location=useLocation();
 const [slot,setSlot]=useState<Element|null>(null);
 const cardRef=useRef<HTMLElement>(null);
 const [dismissed,setDismissed]=useState<Set<string>>(()=>new Set());
 const [error,setError]=useState('');const [phase,setPhase]=useState('fall');
 const active=query.data?.rewards.find(r=>!r.seen&&!dismissed.has(r.id));
 const item=query.data?.catalog.find(i=>i.id===active?.itemId);
 const onFinish=useRef<()=>void>(()=>{});
 onFinish.current=()=>{if(!active)return;void acknowledge(active.id).then(()=>{setDismissed(old=>new Set([...old,active.id]));setError('');void client.invalidateQueries({queryKey:collectionKey});}).catch(()=>{setPhase('fall');setError('宝物已保存，通知暂未收起。点击重试。');});};
 useEffect(()=>{
  const refresh=()=>{void client.invalidateQueries({queryKey:collectionKey});};
  const unsubs=[subscribeToSSE(SSE_EVENTS.treasureAwarded,refresh),subscribeToSSE(SSE_EVENTS.hello,refresh)];
  return()=>unsubs.forEach(fn=>fn());
 },[client]);
 useEffect(()=>{
  if(!active)return;setError('');
  const reduced=matchMedia('(prefers-reduced-motion: reduce)').matches;
  let fly:ReturnType<typeof setTimeout>,finish:ReturnType<typeof setTimeout>;
  const clear=()=>{clearTimeout(fly);clearTimeout(finish);};
  const show=()=>{clear();if(document.hidden)return;setPhase('fall');fly=setTimeout(()=>{const card=cardRef.current,target=document.querySelector('[data-collection-target]')??document.querySelector('a[href="/collection"]');if(card&&target){const a=card.getBoundingClientRect(),b=target.getBoundingClientRect();card.style.setProperty('--collect-x',`${b.x+b.width/2-a.x-a.width/2}px`);card.style.setProperty('--collect-y',`${b.y+b.height/2-a.y-a.height/2}px`);}setPhase('fly');},active.welcome?6500:reduced?4500:3000);finish=setTimeout(()=>onFinish.current(),active.welcome?7200:reduced?5000:3700);};
  show();document.addEventListener('visibilitychange',show);
  return()=>{clear();document.removeEventListener('visibilitychange',show);};
 },[active?.id]);
 useEffect(()=>{const find=()=>setSlot(document.querySelector('[data-treasure-slot]'));find();if(!location.pathname.startsWith('/project/'))return;const observer=new MutationObserver(find);observer.observe(document.body,{childList:true,subtree:true});return()=>observer.disconnect();},[location.pathname,active?.id]);
 if(!active||!item)return null;
 const card=<aside ref={cardRef} key={active.id} className={`${s.reward} ${slot?s.inlineReward:''} ${phase==='fly'?s.fly:''}`} aria-live="polite" aria-label="发现宝物">
  <button className={s.dismiss} onClick={()=>onFinish.current()} aria-label="收起宝物通知">×</button>
  {active.welcome&&<div className={s.welcomeShovel} aria-label="探索的小铲子"><svg viewBox="0 0 64 84" fill="none" aria-hidden="true"><path d="M23 6h18v8a9 9 0 01-18 0Z" stroke="#a58049" strokeWidth="5"/><path d="M32 24v29" stroke="#b99258" strokeWidth="6"/><path d="M18 50h28v15c0 9-14 16-14 16S18 74 18 65Z" fill="#c9d5c2" stroke="#71876b" strokeWidth="2"/><path d="M32 54v19" stroke="#8ba080" strokeWidth="2"/></svg></div>}
  <img className={s.fallingGem} src={thumbnail(item.id)} alt=""/>
  <div className={s.rewardCard}><span className={s.eyebrow}>{active.welcome?'从这里，开始探索':active.isNew?'新发现':'又一件小小的收获'}</span><h2>{item.name}</h2><p>{tierNames[item.tier-1]} · 已收入收藏{active.isNew?'':' · 数量 +1'}</p><small>{active.welcome?'一把小铲子，一颗宝石。愿好奇心带你走得更远。':`来自${active.kind==='feynman'?'讲给教师听':'这一次学习'}中的发现`}</small><Link to={`/collection/${item.id}`} onClick={()=>onFinish.current()}>去看看 ↗</Link>{error&&<button onClick={()=>onFinish.current()}>{error}</button>}</div>
 </aside>;
 return slot?createPortal(card,slot):card;
}
export function CollectionShortcut(){const query=useCollection();const unread=query.data?.rewards.some(r=>!r.seen);return <Link to="/collection" className={s.collectionShortcut} aria-label="我的收藏" title="我的收藏" data-collection-target><span aria-hidden="true">◇</span>{unread&&<i/>}</Link>;}
