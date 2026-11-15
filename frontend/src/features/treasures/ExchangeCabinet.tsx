import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Button } from '@/shared/primitive/Button';
import { Modal } from '@/shared/primitive/Modal';
import { collectionKey, redeemTreasure, rewardActivityNames, thumbnail, tierNames, tierTextColor, type Collection, type ExchangeMode, type ExchangeQuote, type RewardExchange } from './api';
import s from './ExchangeCabinet.module.css';

export const exchangeNumber = new Intl.NumberFormat('zh-CN');

export function ExchangeHistory({ exchange, open, close }: { exchange: RewardExchange; open: boolean; close: () => void }) {
  return <Modal open={open} onOpenChange={value => { if (!value) close(); }} title="兑换记录" description="查看宝石与额度的到账记录。">
    {exchange.history.length === 0 ? <p className={s.muted}>暂无兑换记录。</p> : <ul className={s.history}>{exchange.history.map(row => <li key={row.id}>
      <div><strong>{row.name} ×{row.quantity}</strong><time dateTime={row.time}>{new Date(row.time).toLocaleString('zh-CN')}</time></div>
      <span>{row.mode === 'gem' ? `−${exchangeNumber.format(row.amount)} 学习积分` : `+${exchangeNumber.format(row.amount)} 模型额度`}</span>
    </li>)}</ul>}
  </Modal>;
}

export function ExchangeCabinet({ collection, exchange, mode, setMode, initialItem, store }: {
  collection: Collection; exchange: RewardExchange; mode: ExchangeMode; setMode: (mode: ExchangeMode) => void; initialItem?: string; store: boolean;
}) {
  const [selected, setSelected] = useState(initialItem ?? 'rose-quartz');
  const [quantity, setQuantity] = useState(1);
  const [quote, setQuote] = useState<(ExchangeQuote & { name: string; amount: number })>();
  const [notice, setNotice] = useState('');
  const [showItem, setShowItem] = useState(!!initialItem);
  const queryClient = useQueryClient();
  const { points, policy, used } = exchange;
  const purchases = mode === 'gem';
  const prices = purchases ? policy.prices : policy.values;
  const remaining = Math.max(0, policy.weeklyLimit - used);
  const available = new Map<string, number>();
  for (const reward of collection.rewards) {
    if (reward.eligible && !reward.redeemed) available.set(reward.itemId, (available.get(reward.itemId) ?? 0) + 1);
  }
  const items = collection.catalog.filter(item => item.kind === 'gem' && prices[item.tier - 1] > 0 && (purchases || available.has(item.id)));
  if (!purchases) items.sort((a, b) => (available.get(b.id) ?? 0) - (available.get(a.id) ?? 0));
  const chosen = items.find(item => item.id === selected) ?? items[0];
  const price = chosen ? prices[chosen.tier - 1] : 0;
  const owned = chosen ? available.get(chosen.id) ?? 0 : 0;
  const limit = Math.min(99, purchases ? (price ? Math.floor(points / price) : 0) : owned);
  const count = Math.min(quantity, Math.max(1, limit));
  const amount = count * price;
  const reason = purchases ? (points < amount ? `还差 ${exchangeNumber.format(amount - points)} 学习积分` : '') :
    (amount > remaining ? '超过本周剩余可兑额度。' : '');
  const mutation = useMutation({
    mutationFn: (selection: ExchangeQuote & { name: string; amount: number }) => {
      const { id, itemId, mode, quantity, version } = selection;
      return redeemTreasure({ id, itemId, mode, quantity, version });
    },
    onSuccess: (next, submitted) => {
      queryClient.setQueryData(collectionKey, next);
      const receipt = next.exchange?.history.find(row => row.id === submitted.id);
      if (receipt) setNotice(submitted.mode === 'gem' ? `已收藏 ${receipt.name} ×${receipt.quantity}` : `已到账 ${exchangeNumber.format(receipt.amount)} 模型额度`);
      setQuantity(1);
      setQuote(undefined);
    },
    onError: () => { void queryClient.invalidateQueries({ queryKey: collectionKey }); },
  });
  const quoteChanged = !!quote && quote.version !== policy.version;

  function selectMode(next: ExchangeMode) {
    setMode(next);
    setQuantity(1);
    setNotice('');
    setShowItem(false);
  }

  const receipt = chosen && <aside className={s.summary} aria-label="兑换明细">
    <div className={s.itemHero}><img src={thumbnail(chosen.id)} alt="" /></div>
    <div className={s.summaryHeading}><h2>{chosen.name}</h2><span style={{ color: tierTextColor(chosen.tier) }}>{tierNames[chosen.tier - 1]}</span></div>
    <label className={s.quantity}>数量<select value={count} onChange={event => setQuantity(Number(event.target.value))}>
      {Array.from({ length: Math.max(1, limit) }, (_, index) => index + 1).map(value => <option key={value} value={value}>{value} 颗</option>)}
    </select></label>
    <dl className={s.receipt}>
      <div><dt>消耗</dt><dd>{purchases ? `${exchangeNumber.format(amount)} 学习积分` : `${count} 颗${chosen.name}`}</dd></div>
      <div><dt>兑换后{purchases ? '积分' : '额度'}</dt><dd>{reason ? '—' : exchangeNumber.format(purchases ? points - amount : exchange.credits + amount)}</dd></div>
    </dl>
    {reason && <p className={s.muted}>{reason}</p>}
    <Button variant="primary" disabled={!!reason} onClick={() => { mutation.reset(); setQuote({ mode, itemId: chosen.id, quantity: count, version: policy.version, id: crypto.randomUUID(), name: chosen.name, amount }); setShowItem(false); }}>
      {purchases ? '兑换这款宝石' : `兑换 ${exchangeNumber.format(amount)} 模型额度`}
    </Button>
  </aside>;

  return <section className={s.cabinet} aria-label="宝石兑换">
    <div className={s.toolbar}>
      <nav className={s.modes} aria-label="兑换方式">
        <button type="button" aria-pressed={purchases} onClick={() => selectMode('gem')}>积分换宝石</button>
        <button type="button" aria-pressed={!purchases} onClick={() => selectMode('credit')}>宝石换额度</button>
      </nav>
      {!purchases && <span className={s.muted}>本周还可兑换 <strong>{exchangeNumber.format(remaining)}</strong> 模型额度</span>}
    </div>
    {notice && <p className={s.success} role="status">{notice}</p>}
    {chosen ? <div className={`${s.body} ${store ? s.store : ''}`}>
      <div className={s.shelf}>
        {items.map(item => <button type="button" key={item.id} className={s.specimen} aria-pressed={store ? undefined : chosen.id === item.id} onClick={() => { setSelected(item.id); setQuantity(1); setShowItem(true); }}>
          <img src={thumbnail(item.id)} alt="" /><span className={s.rarity} style={{ color: tierTextColor(item.tier) }}>{tierNames[item.tier - 1]}</span>
          <strong>{item.name}</strong><span className={s.price}>{exchangeNumber.format(prices[item.tier - 1])} {purchases ? '积分' : '额度 / 颗'}</span>
          {!purchases && <span>持有 {available.get(item.id)} 颗</span>}
        </button>)}
      </div>
      {!store && receipt}
    </div> : <p className={s.empty}>{purchases ? '目前没有开放积分兑换的宝石。' : '目前没有可以换额度的宝石。'}</p>}
    {store && <Modal open={showItem && !!chosen} onOpenChange={setShowItem} title="兑换明细" description="选择数量并核对兑换后的余额。" size="sm">{receipt}</Modal>}
    <details className={s.rules}><summary>学习积分怎么获得</summary>
      <ul>{rewardActivityNames.map((name, index) => <li key={name}>{name} · +{policy.earn[index]} 分 · 每天最多 {policy.dailyCounts[index]} 次</li>)}</ul>
      <p>每天最多获得 {policy.dailyLimit} 学习积分。</p>
    </details>
    <Modal open={!!quote} onOpenChange={open => { if (!open && !mutation.isPending) setQuote(undefined); }} title="确认兑换" description="兑换后会更新余额与收藏数量。" size="sm"
      footer={<><Button disabled={mutation.isPending} onClick={() => setQuote(undefined)}>取消</Button><Button variant="primary" disabled={quoteChanged} loading={mutation.isPending} onClick={() => { if (quote) mutation.mutate(quote); }}>确认兑换</Button></>}>
      <p>{quote && (quote.mode === 'gem' ? `使用 ${exchangeNumber.format(quote.amount)} 学习积分，兑换 ${quote.name} ×${quote.quantity}。` :
        `使用 ${quote.name} ×${quote.quantity}，兑换 ${exchangeNumber.format(quote.amount)} 模型额度。图鉴保留发现记录。`)}</p>
      {quoteChanged && <p role="alert">规则已更新，请关闭后按最新价格重新确认。</p>}
      {mutation.isError && <p role="alert">{mutation.error.message}</p>}
    </Modal>
  </section>;
}
