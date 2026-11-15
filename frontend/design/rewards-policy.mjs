export const defaultPolicy = {
  version: 1, earn: [10, 1], dailyCounts: [1, 30], dailyLimit: 30, weeklyLimit: 200,
  prices: [40, 80, 0, 0, 0], values: [20, 40, 60, 100, 150],
};

export const practiceChallenge = {
  title: '向量与线性变换', subject: '线性代数',
  questions: [
    { prompt: '将向量 (2, 1) 乘以 −1，会得到什么？', options: ['(1, 2)', '(−2, −1)', '(2, −1)'] },
    { prompt: '下列哪一个操作一定是线性变换？', options: ['将每个向量放大 2 倍', '给每个向量加上 (1, 1)', '将每个分量取绝对值'] },
    { prompt: '用一个 2 × 2 矩阵左乘一个二维列向量，结果是？', options: ['一个数', '一个 2 × 2 矩阵', '一个二维列向量'] },
  ],
};

export function finishChallenge(wallet, policy, answers) {
  if (!Array.isArray(answers) || answers.length !== practiceChallenge.questions.length ||
      answers.some((answer, index) => !Number.isInteger(answer) || !practiceChallenge.questions[index].options[answer])) {
    throw new Error('请回答每一道题再提交。');
  }
  if (wallet.challengeResult) return;
  const correct = [1, 0, 2];
  const explanations = ['每一个分量都乘以 −1。', '按固定倍数缩放同时保留加法和数乘。', '2 × 2 矩阵乘 2 × 1 向量，结果为 2 × 1 向量。'];
  const score = answers.filter((answer, index) => answer === correct[index]).length;
  const award = policy.dailyCounts[0] ? Math.min(policy.earn[0], policy.dailyLimit) : 0;
  wallet.points += award;
  wallet.challengeResult = { score, award, answers, correct, explanations, completedAt: new Date().toISOString() };
}

export function changePolicy(current, draft) {
  if (draft.version !== current.version) throw new Error('规则已更新，请刷新后重试。');
  const { earn, dailyCounts, dailyLimit, weeklyLimit, prices, values } = draft;
  const arrays = [[earn, defaultPolicy.earn.length], [dailyCounts, defaultPolicy.dailyCounts.length], [prices, 5], [values, 5]];
  if (arrays.some(([values, length]) => !Array.isArray(values) || values.length !== length) ||
      [...earn, ...dailyCounts, ...prices, ...values, dailyLimit, weeklyLimit].some(value => !Number.isSafeInteger(value) || value < 0 || value > 1000000)) {
    throw new Error('请输入 0 至 1,000,000 之间的整数。');
  }
  return { version: current.version + 1, earn, dailyCounts, dailyLimit, weeklyLimit, prices, values };
}

export function exchange(state, policy, item, { mode, quantity, version, id }) {
  if (typeof id !== 'string' || !id || !['gem', 'credit'].includes(mode) || item?.kind !== 'gem' || !Number.isInteger(quantity) || quantity < 1 || quantity > 99) {
    throw new Error('请选择宝石和有效数量。');
  }
  const previous = state.history.find(entry => entry.id === id);
  if (previous) {
    if (previous.itemId !== item.id || previous.mode !== mode || previous.quantity !== quantity) throw new Error('兑换身份已使用。');
    return;
  }
  if (version !== policy.version) throw new Error('规则已更新，请重新确认价格。');
  const unit = (mode === 'gem' ? policy.prices : policy.values)[item.tier - 1];
  if (!unit) throw new Error('这款宝石当前未开放该类兑换。');
  const amount = unit * quantity;
  const time = new Date().toISOString();
  if (mode === 'gem') {
    if (state.points < amount) throw new Error('学习积分余额不足。');
    state.points -= amount;
    for (let index = 0; index < quantity; index++) {
      state.rewards.push({ id: `${id}:${index}`, itemId: item.id, kind: 'exchange', createdAt: time, seen: true, isNew: false, eligible: true });
    }
  } else {
    const owned = state.rewards.filter(reward => reward.itemId === item.id && reward.eligible && !reward.redeemed);
    if (owned.length < quantity) throw new Error('可兑换的宝石数量不足。');
    if (state.used + amount > policy.weeklyLimit) throw new Error('超过本周剩余可兑额度。');
    for (const reward of owned.slice(0, quantity)) reward.redeemed = true;
    state.used += amount;
    state.credits += amount;
  }
  state.history.unshift({ id, itemId: item.id, name: item.name, mode, amount, quantity, time, version });
}
