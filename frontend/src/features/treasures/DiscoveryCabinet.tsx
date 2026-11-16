import { useMemo, useState, type ReactNode } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Button } from '@/shared/primitive/Button';
import { Modal } from '@/shared/primitive/Modal';
import { collectionKey, portrait, redeemTreasure, rewardActivityNames, submitRewardPractice, thumbnail, tierNames, type Collection, type ExchangeMode, type ExchangeQuote, type RewardExchange, type TreasureItem } from './api';
import { ExchangeCabinet, ExchangeHistory, exchangeNumber } from './ExchangeCabinet';
import { chanceText, itemChance } from './rarity';
import { TreasureSourceLink } from './TreasureSourceLink';
import s from './DiscoveryCabinet.module.css';

const quartz = ['rock-crystal', 'rose-quartz', 'smoky-quartz', 'amethyst', 'citrine'];
const goalStorage = 'lll.collection.goal';
const fmt = (value: number) => exchangeNumber.format(value);
type View = 'discovery' | 'catalog' | 'earn' | 'exchange';
const accountDay = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' });
const activityDescriptions = ['完成一份练习并提交，查看得分和解析。', '主动发出一条学习消息，并收到完整的教师回复。'];

function TreasurePortrait({ item, decorative = false }: { item: TreasureItem; decorative?: boolean }) {
  return <img key={item.id} src={portrait(item.id)} alt={decorative ? '' : item.name} width={1024} height={1024}
    onError={event => {
      if (!event.currentTarget.dataset.fallback) {
        event.currentTarget.dataset.fallback = 'true';
        event.currentTarget.src = thumbnail(item.id);
      }
    }} />;
}

export function DiscoveryCabinet({ collection, exchange, catalogFooter }: { collection: Collection; exchange?: RewardExchange; catalogFooter?: ReactNode }) {
  const { catalog, rewards } = collection;
  const { points = 0, credits = 0, policy, challenge, challengeResult } = exchange ?? {};
  const client = useQueryClient();
  const [params, setParams] = useSearchParams();
  const [goalId, setGoalId] = useState(() => { try { return localStorage.getItem(goalStorage) || 'citrine'; } catch { return 'citrine'; } });
  const [filter, setFilter] = useState<'all' | 'gem' | 'shell'>('all');
  const [tier, setTier] = useState(0);
  const [search, setSearch] = useState('');
  const [focus, setFocus] = useState<TreasureItem>();
  const [mode, setMode] = useState<ExchangeMode>('gem');
  const [historyOpen, setHistoryOpen] = useState(false);
  const [practiceOpen, setPracticeOpen] = useState(false);
  const [answers, setAnswers] = useState<number[]>([]);
  const [quote, setQuote] = useState<{ input: ExchangeQuote; name: string; price: number }>();
  const [notice, setNotice] = useState('');
  const requestedView = params.get('view');
  const view: View = requestedView === 'catalog' || (exchange && (requestedView === 'exchange' || requestedView === 'earn')) ? requestedView : 'discovery';
  const inventory = useMemo(() => {
    const found = new Set<string>();
    const held = new Map<string, number>();
    for (const reward of rewards) {
      found.add(reward.itemId);
      if (!reward.redeemed) held.set(reward.itemId, (held.get(reward.itemId) ?? 0) + 1);
    }
    return { found, held };
  }, [rewards]);
  const setItems = quartz.flatMap(id => catalog.find(item => item.id === id) ?? []);
  const setCount = setItems.filter(item => inventory.found.has(item.id)).length;
  const goal = catalog.find(item => item.id === goalId) ?? catalog.find(item => item.kind === 'gem' && !inventory.found.has(item.id)) ?? catalog[0];
  const goalFound = !!goal && inventory.found.has(goal.id);
  const price = goal?.kind === 'gem' ? policy?.prices[goal.tier - 1] ?? 0 : 0;
  const deficit = Math.max(0, price - points);
  const progress = price ? Math.min(100, 100 * points / price) : 0;
  const reward = policy?.dailyCounts[0] ? Math.min(policy.earn[0], policy.dailyLimit) : 0;
  const completedAt = challengeResult?.completedAt ? Date.parse(challengeResult.completedAt) : NaN;
  const earnedToday = challengeResult && Number.isFinite(completedAt) && accountDay.format(completedAt) === accountDay.format(Date.now()) ? challengeResult.award : 0;
  const recent = [...rewards].sort((a, b) => b.createdAt.localeCompare(a.createdAt)).filter((entry, index, list) => list.findIndex(other => other.itemId === entry.itemId) === index).slice(0, 3);
  const shownItems = catalog.filter(item => (filter === 'all' || item.kind === filter) && (!tier || item.tier === tier) &&
    `${item.name} ${item.english}`.toLowerCase().includes(search.trim().toLowerCase()));
  const practice = useMutation({ mutationFn: submitRewardPractice, onSuccess: next => client.setQueryData(collectionKey, next) });
  const redeem = useMutation({
    mutationFn: redeemTreasure,
    onSuccess: (next, input) => {
      client.setQueryData(collectionKey, next);
      const receipt = next.exchange?.history.find(item => item.id === input.id);
      if (receipt) setNotice(`已收录 ${receipt.name}`);
      setQuote(undefined);
    },
    onError: () => { void client.invalidateQueries({ queryKey: collectionKey }); },
  });

  function show(next: View) {
    setParams(current => { const search = new URLSearchParams(current); search.set('view', next); return search; }, { replace: true });
  }

  function chooseGoal(item: TreasureItem) {
    try { localStorage.setItem(goalStorage, item.id); } catch { /* Current visit keeps the target when storage is unavailable. */ }
    setGoalId(item.id);
    setFocus(undefined);
    setNotice('');
    show('discovery');
  }

  function openQuote() {
    if (!goal || !policy || !price || deficit || goalFound) return;
    redeem.reset();
    setQuote({ input: { id: crypto.randomUUID(), mode: 'gem', quantity: 1, itemId: goal.id, version: policy.version }, name: goal.name, price });
  }

  const tabs: [View, string][] = [['discovery', '总览'], ['catalog', '收藏图鉴']];
  if (exchange) tabs.push(['earn', '赚积分'], ['exchange', '兑换']);
  return <section className={s.page} aria-label="我的收藏" data-discovery-cabinet>
    <header className={s.header}>
      <div><h1>我的收藏</h1><p>已发现 <strong>{inventory.found.size}</strong> / {catalog.length} 件藏品</p></div>
      {exchange ? <div className={s.wallet}><div><span>学习积分</span><strong>{fmt(points)}</strong></div><div><span>模型额度</span><strong>{fmt(credits)}</strong></div><Button size="sm" onClick={() => setHistoryOpen(true)}>兑换记录</Button></div>
        : <Link className={s.inlineButton} to="/collection/faq">如何发现宝物 ↗</Link>}
    </header>
    <nav className={s.navigation} aria-label="收藏区域">{tabs.map(([id, label]) => <button key={id} type="button" aria-current={view === id ? 'page' : undefined} onClick={() => show(id)}>{label}</button>)}</nav>

    {view === 'discovery' && <>
      {notice && <div className={s.notice} role="status">{notice}，已计入收藏进度。</div>}
      <div className={s.stage}>
        {goal && <article className={s.hero}>
          <div className={s.heroArt} aria-hidden="true"><TreasurePortrait item={goal} decorative /></div>
          <div className={s.heroCopy}>
            <p className={s.kicker}>{goalFound ? '收藏目标已达成' : '当前收藏目标'}</p>
            <h2>{goal.name}</h2>
            <p className={s.subtitle}>{quartz.includes(goal.id) ? '水晶系列' : goal.kind === 'gem' ? '宝石图鉴' : '贝壳图鉴'}<span>·</span>{tierNames[goal.tier - 1]}</p>
            {goalFound ? <p className={s.goalNote}>已收录到收藏图鉴。</p> : price ?
              <div className={s.meter}><div><span>{deficit ? `还差 ${fmt(deficit)} 学习积分` : '积分已攒够'}</span><span>{fmt(Math.min(points, price))} / {fmt(price)}</span></div>
                <progress value={progress} max={100} aria-label="收藏目标积分进度" /></div> :
              <p className={s.goalNote}>在学习中的随机发现可以获得这件藏品。</p>}
            <div className={s.heroActions}>
              {goalFound ? <Button variant="primary" onClick={() => show('catalog')}>选择下一个目标</Button> : !price ?
                <Link className={s.heroLink} to="/">继续学习</Link> : !deficit ? <Button variant="primary" onClick={openQuote}>兑换并收录 · {fmt(price)} 积分</Button> :
                challenge && !challengeResult && reward ? <Button variant="primary" onClick={() => setPracticeOpen(true)}>完成一组练习 · +{fmt(reward)} 分</Button> :
                  <Button variant="primary" onClick={() => show('earn')}>查看积分任务</Button>}
              {!goalFound && <button className={s.textButton} type="button" onClick={() => show('catalog')}>换个目标</button>}
            </div>
          </div>
        </article>}
        <section className={s.set} aria-labelledby="quartz-set-heading">
          <div className={s.sectionHead}><h2 id="quartz-set-heading">水晶系列</h2><span><strong>{setCount}</strong> / {setItems.length}</span></div>
          <div className={s.setList}>{setItems.map(item => <button key={item.id} type="button" onClick={() => setFocus(item)} data-found={inventory.found.has(item.id)} data-target={goal?.id === item.id}>
            <img src={thumbnail(item.id)} alt="" /><span>{item.name}</span><small>{inventory.found.has(item.id) ? '已收录' : goal?.id === item.id ? '当前目标' : '待发现'}</small>
          </button>)}</div>
        </section>
      </div>
      <section className={s.recent} aria-labelledby="recent-findings">
        <div className={s.sectionHead}><h2 id="recent-findings">最近发现</h2><button type="button" className={s.inlineButton} onClick={() => show('catalog')}>查看图鉴 →</button></div>
        <div className={s.findings}>{recent.map(entry => {
          const item = catalog.find(candidate => candidate.id === entry.itemId);
          return item && <button type="button" key={entry.id} onClick={() => setFocus(item)}>
            <img src={thumbnail(item.id)} alt="" /><div><strong>{item.name}</strong><small>{entry.kind === 'exchange' ? '通过学习积分兑换' : entry.projectTitle || '学习中发现'}</small></div>
          </button>;
        })}{!recent.length && <p>第一件藏品会出现在这里。</p>}</div>
      </section>
    </>}

    {view === 'catalog' && <section className={s.library} aria-label="全部藏品">
      <div className={s.libraryToolbar}><div><h2>收藏图鉴</h2><p>点击藏品查看来源，或设为下一个收藏目标。</p></div>
        <nav className={s.filters} aria-label="藏品类别">{([['all', '全部'], ['gem', '宝石'], ['shell', '贝壳']] as const).map(([id, label]) => <button key={id} type="button" aria-pressed={filter === id} onClick={() => setFilter(id)}>{label}</button>)}</nav></div>
      <div className={s.catalogFilters}>
        <input value={search} onChange={event => setSearch(event.target.value)} aria-label="搜索藏品" placeholder="搜索藏品" />
        <label>稀有度 <select value={tier} onChange={event => setTier(Number(event.target.value))}>
          <option value={0}>全部</option>{tierNames.map((name, index) => <option key={name} value={index + 1}>{name}</option>)}
        </select></label>
      </div>
      <div className={s.catalog}>{shownItems.map(item => {
        const count = inventory.held.get(item.id) ?? 0;
        const status = count ? `持有 ${count} 件` : inventory.found.has(item.id) ? '曾获得' : '尚未发现';
        return <button key={item.id} type="button" data-found={inventory.found.has(item.id)} aria-label={`${item.name} · ${status}`} onClick={() => setFocus(item)}>
          <img src={thumbnail(item.id)} alt="" loading="lazy" /><strong>{item.name}</strong><small>{status}</small>
        </button>;
      })}</div>
      {!shownItems.length && <p>没有找到符合条件的藏品。</p>}
      {catalogFooter && <footer className={s.catalogFooter}>{catalogFooter}</footer>}
    </section>}

    {view === 'exchange' && exchange && <div className={s.exchange}><ExchangeCabinet collection={collection} exchange={exchange} mode={mode} setMode={setMode} store /></div>}

    {view === 'earn' && policy && <section className={s.earn} aria-labelledby="earn-title">
      <header className={s.earnHeader}>
        <div><h2 id="earn-title">赚学习积分</h2><p>{goal && !goalFound && price ? `收藏目标 · ${goal.name} · ${deficit ? `还差 ${fmt(deficit)} 分` : '积分已攒够'}` : '完成学习后，积分自动到账。'}</p></div>
        <div className={s.earnedToday}><span>今日已得</span><strong>{fmt(earnedToday)} / {fmt(policy.dailyLimit)} 分</strong></div>
      </header>
      <ol className={s.activities}>{rewardActivityNames.map((title, index) => {
        const active = policy.dailyCounts[index] > 0 && policy.earn[index] > 0 && policy.dailyLimit > 0;
        return <li key={title}>
          <div><h3>{title}</h3><p>{activityDescriptions[index]}</p></div>
          <strong className={s.activityPoints}>{active ? `+${fmt(policy.earn[index])} 分` : '—'}</strong>
          <div className={s.activityAction}>
            {index === 0 && challenge && (active || challengeResult) ? <Button type="button" size="sm" onClick={() => setPracticeOpen(true)}>{challengeResult ? '查看练习结果' : '开始练习'}</Button> :
              <span>{active ? '待接入' : '未开放'}</span>}
          </div>
        </li>;
      })}</ol>
      {challenge && <p className={s.earnNote}>样例练习可体验积分到账，当前分值仅用于预览。</p>}
    </section>}

    <Modal open={!!focus} onOpenChange={open => { if (!open) setFocus(undefined); }} title={focus?.name ?? '藏品详情'} description={focus ? `${tierNames[focus.tier - 1]} · ${focus.kind === 'gem' ? '宝石' : '贝壳'}` : undefined} size="sm"
      footer={focus && <><Button onClick={() => setFocus(undefined)}>关闭</Button>{!inventory.found.has(focus.id) && <Button variant="primary" onClick={() => chooseGoal(focus)}>设为收藏目标</Button>}</>}>
      {focus && <div className={s.specimenDetail}><TreasurePortrait item={focus} /><section>
        {inventory.found.has(focus.id) ? <><p>{inventory.held.get(focus.id) ? `持有 ${inventory.held.get(focus.id)} 件` : '曾获得 · 当前持有 0 件'}</p><Link to={`/collection/${focus.id}`}>打开三维展示 →</Link></> : <p>尚未发现</p>}
        <p>获得宝物时，抽中这一件的概率约为 {chanceText(itemChance(focus, catalog))}。</p>
        {rewards.filter(entry => entry.itemId === focus.id).map(entry => entry.kind === 'exchange' ? <p key={entry.id}>通过学习积分兑换</p> : <TreasureSourceLink key={entry.id} reward={entry} />)}
        {policy && focus.kind === 'gem' && policy.prices[focus.tier - 1] > 0 && <p>{fmt(policy.prices[focus.tier - 1])} 学习积分可兑换一颗</p>}
      </section></div>}
    </Modal>

    <Modal open={practiceOpen} onOpenChange={open => { if (!practice.isPending) setPracticeOpen(open); }} title={challenge?.title ?? '学习练习'} description={`${challenge?.subject ?? '学习'} · 预览练习`}
      footer={challengeResult ? <Button variant="primary" onClick={() => { setPracticeOpen(false); show('discovery'); }}>回到收藏目标</Button> : <><Button disabled={practice.isPending} onClick={() => setPracticeOpen(false)}>稍后再做</Button><Button variant="primary" loading={practice.isPending} disabled={!challenge?.questions.every((_, index) => Number.isInteger(answers[index]))} onClick={() => practice.mutate(answers)}>提交练习</Button></>}>
      {challengeResult && <p className={s.notice} role="status">答对 {challengeResult.score} / {challenge?.questions.length} 题 · 学习积分 +{fmt(challengeResult.award)}</p>}
      {challenge?.questions.map((question, index) => <fieldset key={question.prompt} className={s.question}>
        <legend>{index + 1}. {question.prompt}</legend>
        {question.options.map((option, choice) => <label key={option} data-correct={challengeResult?.correct[index] === choice || undefined}>
          <input type="radio" name={`question-${index}`} checked={(challengeResult?.answers[index] ?? answers[index]) === choice} disabled={!!challengeResult} onChange={() => setAnswers(current => {
            const next = current.slice(); next[index] = choice; return next;
          })} />{option}
        </label>)}
        {challengeResult && <small>{challengeResult.explanations[index]}</small>}
      </fieldset>)}
      {practice.isError && <p role="alert">{practice.error.message}</p>}
    </Modal>

    <Modal open={!!quote} onOpenChange={open => { if (!open && !redeem.isPending) setQuote(undefined); }} title="兑换并收录" description={quote ? `用 ${fmt(quote.price)} 学习积分兑换 ${quote.name}。` : undefined} size="sm"
      footer={<><Button onClick={() => setQuote(undefined)} disabled={redeem.isPending}>取消</Button><Button variant="primary" disabled={quote?.input.version !== policy?.version} loading={redeem.isPending} onClick={() => { if (quote) redeem.mutate(quote.input); }}>确认兑换</Button></>}>
      {quote && <p>兑换后余额 {fmt(Math.max(0, points - quote.price))} 学习积分。</p>}
      {quote && quote.input.version !== policy?.version && <p role="alert">价格已更新，请重新打开报价。</p>}
      {redeem.isError && <p role="alert">{redeem.error.message}</p>}
    </Modal>
    {exchange && <ExchangeHistory exchange={exchange} open={historyOpen} close={() => setHistoryOpen(false)} />}
  </section>;
}
