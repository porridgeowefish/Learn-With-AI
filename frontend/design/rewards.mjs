import { defaultPolicy, exchange, finishChallenge, practiceChallenge } from '/__design/rewards-policy.mjs';

function week() {
  const day = new Date(Date.now() + 8 * 3600000);
  day.setUTCDate(day.getUTCDate() - (day.getUTCDay() + 6) % 7);
  return day.toISOString().slice(0, 10);
}

export async function rewardsExchange(ctx) {
  const { path, request, method, json, state } = ctx;
  if (!['/api/treasures', '/api/treasures/exchange', '/api/treasures/challenge'].includes(path)) return;
  const catalog = await (await fetch('/__design/treasures.json')).json();
  const policy = await fetch('/__design/rewards-policy', { cache: 'no-store' })
    .then(response => response.ok ? response.json() : defaultPolicy).catch(() => defaultPolicy);
  state.rewards ??= [];
  if (!state.rewardExchange) {
    const seeds = [['rose-quartz', 2], ['amethyst', 1], ['moonstone', 1], ['cockle', 1], ['blue-mussel', 1]];
    state.rewards.push(...seeds.flatMap(([itemId, quantity]) => Array.from({ length: quantity }, (_, index) => ({
      id: `exchange-demo:${itemId}:${index}`, itemId, kind: 'teacher', projectSlug: 'math', projectId: 'math', projectTitle: '线性代数',
      sourceId: 'preview', messageId: '', createdAt: new Date().toISOString(), seen: true, isNew: false, eligible: itemId !== 'cockle' && itemId !== 'blue-mussel',
    }))));
    state.rewardExchange = { points: 75, credits: 280, used: 0, week: week(), history: [] };
    ctx.dirty();
  }
  const wallet = state.rewardExchange;
  if (wallet.week !== week()) {
    wallet.week = week();
    wallet.used = 0;
    ctx.dirty();
  }
  if (path.endsWith('/exchange')) {
    if (method !== 'POST') return json({ error: '请选择兑换操作。' }, 405);
    const input = await request.json();
    const pending = { ...wallet, rewards: state.rewards };
    try { exchange(pending, policy, catalog.find(item => item.id === input.itemId), input); }
    catch (error) { return json({ error: error.message }, 409); }
    const { points, credits, used } = pending;
    Object.assign(wallet, { points, credits, used });
  }
  if (path.endsWith('/challenge')) {
    if (method !== 'POST') return json({ error: '请选择学习任务。' }, 405);
    const { answers } = await request.json();
    try { finishChallenge(wallet, policy, answers); }
    catch (error) { return json({ error: error.message }, 400); }
  }
  return json({ catalog, rewards: state.rewards, exchange: { ...wallet, policy, challenge: practiceChallenge } });
}
