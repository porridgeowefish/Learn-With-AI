import { Link } from 'react-router-dom';
import s from './LearningProgress.module.css';

export interface LearningProgressValue { goal: string; taught: number; total: number; achieved: boolean; replies: string[] }

/** Model-suggested replies are optional: invalid entries are dropped without
 * invalidating the progress itself. */
function readReplies(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  const seen = new Set<string>();
  const replies: string[] = [];
  for (const item of value) {
    if (typeof item !== 'string') continue;
    const text = item.replace(/\s+/g, ' ').trim();
    if (!text || text.length > 40 || seen.has(text)) continue;
    seen.add(text);
    replies.push(text);
    if (replies.length === 4) break;
  }
  return replies;
}

export function readLearningProgress(source: string): LearningProgressValue | undefined {
  const matches = [...source.matchAll(/(?:^|\n)```learning-progress[^\S\r\n]*\r?\n([\s\S]{1,2000}?)\r?\n```(?=\r?\n|$)/g)];
  if (!matches.length) return;
  try {
    const p = JSON.parse(matches[matches.length - 1][1]);
    if (typeof p.goal !== 'string' || !p.goal.trim() || p.goal.length > 200 || !Number.isSafeInteger(p.taught) || !Number.isSafeInteger(p.total) || p.total < 1 || p.taught < 0 || p.taught > p.total || typeof p.achieved !== 'boolean') return;
    return { goal: p.goal.trim(), taught: p.taught, total: p.total, achieved: p.achieved && p.taught === p.total, replies: readReplies(p.replies) };
  } catch { return; }
}
// New answers keep suggestions separate from progress. Older answers embed them.
const replyFooter = /(?:^|\n)```suggested-replies[^\S\r\n]*(?:\r?\n|$)/;

export function readSuggestedReplies(source: string): string[] {
  const footer = replyFooter.exec(source);
  if (!footer) return readLearningProgress(source)?.replies ?? [];
  const payload = source.slice(footer.index + footer[0].length).match(/^([\s\S]{1,2000}?)\r?\n```\s*$/);
  if (!payload) return [];
  try { return readReplies(JSON.parse(payload[1])); } catch { return []; }
}

export function withoutLearningProgress(source: string) {
  return source.replace(/(?:^|\n)```learning-progress[^\n]*\n[\s\S]*?(?:\n```|$)/g, '').trimEnd();
}

/** Historical metadata becomes ordinary prose; current answers supply their own first line. */
export function teacherMarkdown(source: string) {
  const progress = readLearningProgress(source);
  const body = withoutLearningProgress(source.split(replyFooter, 1)[0]);
  if (!progress || /^\s*进度[：:]/.test(body)) return body;
  const goal = progress.goal.replace(/[\r\n]/g, ' ').replace(/([\\`*_{}[\]<>])/g, '\\$1');
  return `进度：${progress.taught}/${progress.total} · ${goal}\n\n${body}`;
}

/** Progress is the model's own count, shown as text ("进度 2/4") so the learner
 * sees roughly how much is left. Missing or invalid metadata is never guessed. */
export function LearningProgress({ value, slug }: { value?: LearningProgressValue; slug: string }) {
  const remaining = value ? value.total - value.taught : 0;
  return <div className={s.progress} role="status" aria-label="学习进度">
    <p className={s.line}>
      <strong>进度 {value ? `${value.taught}/${value.total}` : '—'}</strong>
      {value && <span className={s.goal}>{value.goal}</span>}
      {value && !value.achieved && <span className={s.remaining}>{remaining > 0 ? `还剩约 ${remaining} 个要点` : '已讲完，等你验证理解'}</span>}
      {!value && <span className={s.remaining}>这条回答没有给出进度</span>}
    </p>
    {value?.achieved && <div className={s.complete}><span>恭喜，已达成本轮学习目标！</span><Link to={`/project/${encodeURIComponent(slug)}/assets`} onClick={() => localStorage.setItem(`lll.assets.view.${slug}`, 'review')}>去复习</Link><Link to={`/project/${encodeURIComponent(slug)}/assets`} onClick={() => localStorage.setItem(`lll.assets.view.${slug}`, 'practice')}>去练习</Link></div>}
  </div>;
}

const chartReply = '结合交互图表理解';
const shortcuts = ['举个例子', '太难，退一步', '太简单，快一点', chartReply];

export function QuickReplies({ replies = [], onSend }: { replies?: string[]; onSend: (text: string) => void }) {
  const options = replies.length >= 2 ? replies : shortcuts;
  return <div className={s.quick} aria-label="快捷回复">
    <div className={s.quickRow} role="group" aria-label="建议回复">
      {options.map(text => <button key={text} type="button" className={s.suggestion} onClick={() => onSend(text)}>
        {text === chartReply && <svg aria-hidden="true" width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M2 2v11h12M4 10l3-4 3 2 4-5" /></svg>}
        {text}
      </button>)}
    </div>
  </div>;
}
