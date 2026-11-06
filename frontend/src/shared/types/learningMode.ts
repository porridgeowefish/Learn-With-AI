export type LearningMode = 'general' | 'history' | 'book' | 'concept';

export const learningModes: ReadonlyArray<{ value: LearningMode; label: string; description: string }> = [
  { value: 'general', label: '自由学习', description: '随问题和反馈灵活选择讲解方法。' },
  { value: 'history', label: '历史脉络', description: '用故事、时间线与关键转折理解来龙去脉。' },
  { value: 'book', label: '书籍导读', description: '结合书的背景、类型和目录结构建立阅读地图。' },
  { value: 'concept', label: '概念探究', description: '从原理、例子、边界和相近概念等角度理解。' },
];
