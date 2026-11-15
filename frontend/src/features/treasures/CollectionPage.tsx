import { useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { Button } from '@/shared/primitive/Button';
import { Modal } from '@/shared/primitive/Modal';
import { thumbnail, tierNames, tierTextColor, useCollection, type ExchangeMode } from './api';
import { chanceText, itemChance } from './rarity';
import { ExchangeCabinet, ExchangeHistory, exchangeNumber } from './ExchangeCabinet';
import { DiscoveryCabinet } from './DiscoveryCabinet';
import { TreasureSourceLink } from './TreasureSourceLink';
import s from './Treasures.module.css';

function NewtonQuote() {
  return <figure className={s.newtonQuote} aria-label="牛顿：真理的海洋">
   <blockquote cite="https://www.fitzmuseum.cam.ac.uk/index.php/explore-our-collection/highlights/context/stories-and-histories/sir-isaac-newton">
    <p lang="en" className={s.quoteEnglish}>“I do not know what I may appear to the world; but to myself I seem to have been only like a boy playing on the seashore, and diverting myself in now and then finding a smoother pebble or a prettier shell than ordinary, whilst the great ocean of truth lay all undiscovered before me.”</p>
    <p lang="zh-CN" className={s.quoteChinese}>“我不知道世人会怎样看待我；但在我自己看来，我不过像一个在海边玩耍的男孩，偶尔发现一颗比平常更光滑的卵石，或一枚更美丽的贝壳，便为之欣喜。而真理的浩瀚海洋，依然未经探索，展现在我面前。”</p>
   </blockquote>
   <figcaption><span className={s.newtonSignature}>Isaac Newton</span><span className={s.newtonCredit}>艾萨克·牛顿<span aria-hidden="true"> · </span><a href="https://www.fitzmuseum.cam.ac.uk/index.php/explore-our-collection/highlights/context/stories-and-histories/sir-isaac-newton" target="_blank" rel="noopener noreferrer">引文出处 ↗</a></span></figcaption>
  </figure>;
}

export function CollectionPage() {
  const query = useCollection();
  const [kind, setKind] = useState('all');
  const [tier, setTier] = useState(0);
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState('');
  const [params, setParams] = useSearchParams();
  const [mode, setMode] = useState<ExchangeMode>('gem');
  const [detailOpen, setDetailOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [exchangeItem, setExchangeItem] = useState<string>();
  const exchange = query.data?.exchange;
  const store = !!exchange && params.get('layout') !== 'split';
  const shopping = !!exchange && params.get('view') === 'exchange';
  const updateView = (key: string, value: string) => setParams(current => {
    const next = new URLSearchParams(current);
    next.set(key, value);
    return next;
  }, { replace: true });
  const catalog = query.data?.catalog ?? [];
  const discovered = useMemo(() => new Set((query.data?.rewards ?? []).map(reward => reward.itemId)), [query.data]);
  const counts = useMemo(() => {
    const result = new Map<string, number>();
    for (const reward of query.data?.rewards ?? []) {
      if (!reward.redeemed) result.set(reward.itemId, (result.get(reward.itemId) ?? 0) + 1);
    }
    return result;
  }, [query.data]);
  const items = useMemo(() => {
    const filtered = catalog.filter(item => (kind === 'all' || item.kind === kind) && (!tier || item.tier === tier) &&
      `${item.name} ${item.english}`.toLowerCase().includes(search.trim().toLowerCase()));
    if (store && kind === 'all') filtered.sort((a, b) => Number(discovered.has(b.id)) - Number(discovered.has(a.id)) || Number(b.kind === 'gem') - Number(a.kind === 'gem'));
    return filtered;
  }, [catalog, kind, tier, search, store, discovered]);
  useEffect(() => {
    if (items.length && !items.some(item => item.id === selected)) setSelected(items.find(item => discovered.has(item.id))?.id ?? items[0].id);
  }, [items, selected, discovered]);
  const chosen = items.find(item => item.id === selected);
  const owned = chosen ? counts.get(chosen.id) ?? 0 : 0;
  const history = (query.data?.rewards ?? []).filter(reward => reward.itemId === chosen?.id).slice().reverse();
  const canExchange = !!exchange && chosen?.kind === 'gem' && exchange.policy.values[chosen.tier - 1] > 0 && history.some(reward => reward.eligible && !reward.redeemed);
  if (query.isLoading) return <div className={s.loading}>正在打开你的收藏…</div>;
  if (query.isError) return <div className={s.loading} role="alert">收藏暂时无法读取。<button onClick={() => void query.refetch()}>重新读取</button></div>;
  const comparison = !!exchange && ['store', 'split'].includes(params.get('layout') ?? '');
  if (query.data && !comparison) {
    return <DiscoveryCabinet collection={query.data} exchange={params.get('rewards') === 'preview' ? exchange : undefined} catalogFooter={<NewtonQuote />} />;
  }

  const detail = chosen && <section className={s.detail}>
    <div className={`${s.detailImage} ${discovered.has(chosen.id) ? '' : s.locked}`}><img src={thumbnail(chosen.id)} alt={chosen.name} /></div>
    <h2>{chosen.name}</h2>
    <p className={s.detailRarity} style={{ color: tierTextColor(chosen.tier) }}>◆ {tierNames[chosen.tier - 1]} · {chosen.kind === 'gem' ? '宝石' : '贝壳'}</p>
    <p className={s.english}>{chosen.english}</p>
    <p className={s.detailChance}>获得宝物时，抽中这一件的概率约为 <b>{chanceText(itemChance(chosen, catalog))}</b></p>
    {discovered.has(chosen.id) ? <><p>{owned ? <>持有 <b>{owned}</b> 件</> : '曾获得 · 当前持有 0 件'}</p>
      <Link className={s.primary} to={`/collection/${chosen.id}`}>✧ 展示这件宝物</Link>
      {canExchange && <Button className={s.exchangeItem} onClick={() => { setMode('credit'); setExchangeItem(chosen.id); setDetailOpen(false); updateView('view', 'exchange'); }}>用宝石换额度</Button>}
      <h3>发现的记忆</h3>{history.map(reward => reward.kind === 'exchange' ? <div className={s.source} key={reward.id}><small>{new Date(reward.createdAt).toLocaleDateString()} · 学习积分兑换{reward.redeemed ? ' · 已换额度' : ''}</small></div> : <TreasureSourceLink key={reward.id} reward={reward} />)}
    </> : <div className={s.waiting}><h3>尚未发现</h3><Link to="/collection/faq">了解如何获得 →</Link></div>}
  </section>;
  return <section className={s.collection}>
    <header className={`${s.collectionHeader} ${exchange ? s.collectionWithExchange : ''}`}>
      <div className={s.collectionIntro}><h1>我的收藏</h1><div className={s.progress}><span>已发现 <b>{discovered.size}</b> / {catalog.length}</span><progress value={discovered.size} max={catalog.length || 50} aria-label="收藏发现进度" /></div><Link className={s.faqLink} to="/collection/faq">如何发现宝物？ ↗</Link></div>
      {exchange ? <div className={s.wallet}>
        <div><span>学习积分</span><strong>{exchangeNumber.format(exchange.points)}</strong></div><div><span>模型额度</span><strong>{exchangeNumber.format(exchange.credits)}</strong></div>
        <Button size="sm" onClick={() => setHistoryOpen(true)}>兑换记录</Button>
      </div> : <NewtonQuote />}
    </header>
    {exchange && <div className={s.collectionNav}>
      <nav aria-label="收藏区域"><button aria-pressed={!shopping} onClick={() => updateView('view', 'cabinet')}>展示柜</button><button aria-pressed={shopping} onClick={() => { setExchangeItem(undefined); setMode('gem'); updateView('view', 'exchange'); }}>{store ? '商店' : '兑换'}</button></nav>
      <nav className={s.layoutPicker} aria-label="比较版式"><button aria-pressed={store} onClick={() => { setDetailOpen(false); updateView('layout', 'store'); }}>展示柜＋商店</button><button aria-pressed={!store} onClick={() => { setDetailOpen(false); updateView('layout', 'split'); }}>双栏版</button></nav>
    </div>}
    {shopping && exchange && query.data ? <ExchangeCabinet key={exchangeItem ?? 'all'} collection={query.data} exchange={exchange} mode={mode} setMode={setMode} initialItem={exchangeItem} store={store} /> : <>
      <div className={s.filters}><nav aria-label="藏品种类">{[['all', '全部'], ['gem', '宝石'], ['shell', '贝壳']].map(([key, label]) => <button key={key} className={kind === key ? s.tabActive : ''} onClick={() => setKind(key)} aria-pressed={kind === key}>{label}</button>)}</nav><input aria-label="搜索藏品" placeholder="⌕  搜索藏品" value={search} onChange={event => setSearch(event.target.value)} /><div className={s.tierFilters}>{['全部', ...tierNames].map((name, index) => <button key={name} aria-pressed={tier === index} className={tier === index ? s.filterActive : ''} style={tier === index || !index ? undefined : { color: tierTextColor(index) }} onClick={() => setTier(index)}>{name}</button>)}</div></div>
      {!discovered.size && <div className={s.firstDiscovery}>与教师讨论，或把学到的内容讲给教师听，发现你的第一件宝物。<Link to="/">去学习 →</Link></div>}
      <div className={`${s.collectionBody} ${store ? s.fullCabinet : ''}`}><div className={s.grid}>{items.map(item => {
        const count = counts.get(item.id) ?? 0;
        const found = discovered.has(item.id);
        const status = count ? `持有 ${count} 件` : found ? '曾获得' : '尚未发现';
        return <button key={item.id} className={`${s.specimen} ${!store && selected === item.id ? s.selected : ''} ${found ? '' : s.locked}`} onClick={() => { setSelected(item.id); setDetailOpen(true); }} aria-pressed={store ? undefined : selected === item.id} aria-label={`${item.name} · ${status}`}>
          <img src={thumbnail(item.id)} alt="" loading="lazy" /><span className={s.tierLabel} style={{ color: tierTextColor(item.tier) }}>◆ {tierNames[item.tier - 1]}</span><strong>{item.name}</strong>{!store && <span>抽中率 {chanceText(itemChance(item, catalog))}</span>}<span>{count ? `× ${count}` : status}</span>
        </button>;
      })}{!items.length && <p>没有找到这件藏品，试试其他名字或筛选条件。</p>}</div>{!store && detail}</div>
      {store && <Modal open={detailOpen && !!chosen} onOpenChange={setDetailOpen} title="藏品详情" description="查看藏品、持有数量和发现记录。" size="sm">{detail}</Modal>}
      {exchange && <footer className={s.quoteFooter}><NewtonQuote /></footer>}
    </>}
    {exchange && <ExchangeHistory exchange={exchange} open={historyOpen} close={() => setHistoryOpen(false)} />}
  </section>;
}
