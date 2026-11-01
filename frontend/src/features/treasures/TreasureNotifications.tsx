import { useEffect, useRef, useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { createPortal } from 'react-dom';
import { useQueryClient } from '@tanstack/react-query';
import { subscribeToSSE } from '@/app/hooks/useSSE';
import { SSE_EVENTS } from '@/shared/lib/constants';
import { acknowledge, collectionKey, thumbnail, tierNames, tierTextColor, useCollection } from './api';
import { chanceText, itemChance } from './rarity';
import s from './Treasures.module.css';

/** Rewards are persisted by the server before this celebration is shown. */
export function TreasureNotifications() {
  const query = useCollection(), client = useQueryClient(), navigate = useNavigate(), location = useLocation();
  const [dismissed, setDismissed] = useState<Set<string>>(() => new Set());
  const [error, setError] = useState('');
  const returnButton = useRef<HTMLButtonElement>(null);
  const closing = useRef(false);
  const active = query.data?.rewards.find(reward => !reward.seen && !dismissed.has(reward.id));
  const item = query.data?.catalog.find(candidate => candidate.id === active?.itemId);

  useEffect(() => {
    const refresh = () => { void client.invalidateQueries({ queryKey: collectionKey }); };
    const unsubs = [subscribeToSSE(SSE_EVENTS.treasureAwarded, refresh), subscribeToSSE(SSE_EVENTS.hello, refresh)];
    return () => unsubs.forEach(unsub => unsub());
  }, [client]);
  useEffect(() => { if (active) { closing.current = false; setError(''); returnButton.current?.focus(); } }, [active?.id]);
  useEffect(() => {
    if (!active) return;
    const oldOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => { document.body.style.overflow = oldOverflow; };
  }, [active?.id]);

  const finish = (destination?: string) => {
    if (!active || closing.current) return;
    closing.current = true;
    void acknowledge(active.id).then(() => {
      setDismissed(old => new Set(old).add(active.id));
      setError('');
      void client.invalidateQueries({ queryKey: collectionKey });
      if (destination) navigate(destination);
    }).catch(() => { closing.current = false; setError('宝物已保存，但提示暂时无法收起。请重试。'); });
  };
  if (!active || !item || !query.data) return null;
  const chance = chanceText(itemChance(item, query.data.catalog, !!active.welcome));
  return createPortal(<div className={s.rewardOverlay} role="presentation">
    <div className={s.rewardBackdrop} />
    <section className={s.rewardStage} role="dialog" aria-modal="true" aria-labelledby="reward-title" aria-describedby="reward-description" onKeyDown={event => { if (event.key !== 'Tab') return; const controls = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('a[href],button:not(:disabled)')); const first = controls[0], last = controls[controls.length - 1]; if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); } else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); } }}>
      <div className={s.rewardGlow} aria-hidden="true" />
      {active.welcome && <div className={s.welcomeShovel} aria-label="探索的小铲子"><svg viewBox="0 0 64 84" fill="none" aria-hidden="true"><path d="M23 6h18v8a9 9 0 01-18 0Z" stroke="#a58049" strokeWidth="5"/><path d="M32 24v29" stroke="#b99258" strokeWidth="6"/><path d="M18 50h28v15c0 9-14 16-14 16S18 74 18 65Z" fill="#c9d5c2" stroke="#71876b" strokeWidth="2"/><path d="M32 54v19" stroke="#8ba080" strokeWidth="2"/></svg></div>}
      <span className={s.rewardEyebrow}>{active.welcome ? '探索从这里开始' : active.isNew ? '发现一件新宝物' : '又一次美好的发现'}</span>
      <div className={s.rewardImage}><img src={thumbnail(item.id)} alt="" /></div>
      <div className={s.rewardCopy}>
        <h2 id="reward-title"><span className={s.rarityPrefix} style={{ color: tierTextColor(item.tier) }}>{tierNames[item.tier - 1]} · </span>{item.name}</h2>
        <p id="reward-description">已自动收入我的收藏{active.isNew ? '' : ' · 数量 +1'}</p>
        <p className={s.chance}>获得宝物时，抽中这一件的概率约为 <strong>{chance}</strong></p>
        <div className={s.rewardActions}><Link to={`/collection/${item.id}`} onClick={event => { event.preventDefault(); finish(`/collection/${item.id}`); }}>去展示柜查看</Link><button ref={returnButton} onClick={() => finish(location.pathname.startsWith('/project/') ? undefined : `/project/${active.projectId}`)}>返回学习</button></div>
        {error && <p role="alert" className={s.rewardError}>{error}</p>}
      </div>
    </section>
  </div>, document.body);
}

export function CollectionShortcut() { const query = useCollection(); const unread = query.data?.rewards.some(reward => !reward.seen); return <Link to="/collection" className={s.collectionShortcut} aria-label="我的收藏" title="我的收藏" data-collection-target><span aria-hidden="true">◇</span>{unread && <i />}</Link>; }
