import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { ApiError } from '@/shared/client';
import { Button } from '@/shared/primitive/Button';
import { MarkdownView } from '@/shared/primitive/MarkdownView';
import { learningWorkspaceKeys, saveAnswerFeedback, saveComparisonVote, type ComparisonVote, type ConversationMessage } from '../api/learningWorkspace';
import { teacherMarkdown } from './LearningProgress';
import s from './AnswerFeedback.module.css';

type Feedback = NonNullable<ConversationMessage['feedback']>;

export function AnswerFeedback({ slug, message }: { slug: string; message: ConversationMessage }) {
  const client = useQueryClient();
  const [saved, setSaved] = useState<Feedback>();
  const vote = useMutation({
    mutationFn: (value: Feedback) => saveAnswerFeedback(slug, message.id, value),
    onSuccess: (_, value) => {
      setSaved(value);
      void client.invalidateQueries({ queryKey: learningWorkspaceKeys.conversation(slug) });
    },
  });
  const value = saved ?? message.feedback;
  return <div className={s.feedback} role="group" aria-label="回答反馈">
    <button type="button" aria-label="喜欢这个回答" title="喜欢这个回答" aria-pressed={value === 'up'}
      disabled={vote.isPending} onClick={() => vote.mutate('up')}><Thumb /></button>
    <button type="button" aria-label="不喜欢这个回答" title="不喜欢这个回答" aria-pressed={value === 'down'}
      disabled={vote.isPending} onClick={() => vote.mutate('down')}><Thumb down /></button>
    {vote.isError ? <span role="alert">反馈未保存，请重试。</span> : value && <span role="status">感谢您的反馈</span>}
  </div>;
}

function Thumb({ down = false }: { down?: boolean }) {
  return <svg aria-hidden="true" viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor"
    strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" className={down ? s.down : undefined}>
    <path d="M7 10v10H3V10zm0 0 5-7a2 2 0 0 1 2 2l-1 5h5a2 2 0 0 1 2 2l-1.4 6.5A2 2 0 0 1 16.7 20H7" />
  </svg>;
}

export function AnswerComparison({ slug, message, streaming }: {
  slug: string; message: ConversationMessage & { comparison: NonNullable<ConversationMessage['comparison']> }; streaming: boolean;
}) {
  const comparison = message.comparison;
  const [choice, setChoice] = useState<ComparisonVote>();
  const client = useQueryClient();
  const vote = useMutation({
    mutationFn: (value: ComparisonVote | 'skipped') => saveComparisonVote(slug, message.id, value),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: learningWorkspaceKeys.conversation(slug) });
    },
  });
  const identityUnavailable = vote.error instanceof ApiError && vote.error.status === 410;
  return <section className={s.comparison} aria-label="比较两个回答" aria-busy={streaming}>
    <header className={s.heading}><strong>哪一个回答更好？</strong><span>本次对比不消耗积分</span></header>
    <div className={s.answers}>
      {(['a', 'b'] as const).map(arm => <div key={arm} className={s.answer} data-selected={choice === arm || undefined}>
        <div className={s.answerTitle}>回答 {arm.toUpperCase()}</div>
        {comparison[arm] ? <MarkdownView source={teacherMarkdown(comparison[arm])} streaming={streaming} />
          : <p className={s.waiting}>正在生成…</p>}
        {!streaming && <Button variant="outline" size="sm" aria-pressed={choice === arm}
          disabled={vote.isPending} onClick={() => setChoice(arm)}>选择 {arm.toUpperCase()}</Button>}
      </div>)}
    </div>
    {!streaming && <div className={s.choices}>
      <Button variant="ghost" size="sm" aria-pressed={choice === 'tie'} disabled={vote.isPending} onClick={() => setChoice('tie')}>一样好</Button>
      <Button variant="primary" size="sm" loading={vote.isPending} disabled={!choice} onClick={() => choice && vote.mutate(choice)}>提交选择</Button>
    </div>}
    {identityUnavailable ? <div className={s.choices}>
      <span role="alert">原参与身份已失效。</span>
      <Button variant="outline" size="sm" onClick={() => vote.mutate('skipped')}>保留 A 并继续</Button>
    </div> : vote.isError && <p role="alert">选择未保存，请重试。</p>}
  </section>;
}
