import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { TeacherMessage } from '../../src/features/learning/components/TeacherMessage';
import type { ConversationMessage } from '../../src/features/learning/api/learningWorkspace';
import '../../src/shared/styles/tokens.css';
import '../../src/shared/styles/globals.css';

const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
client.setQueryData(['learning-profile'], { settings: { showReasoning: true }, capabilities: {} });
function Preview() {
  const [mode, setMode] = useState('waiting');
  const [open, setOpen] = useState(false);
  const body = mode === 'body' || mode === 'done';
  const message: ConversationMessage = {
    id: 'preview-teacher', role: 'teacher', status: mode === 'failed' ? 'failed' : 'completed', createdAt: '',
    blocks: body ? [{ id: 'answer', type: 'markdown', source: '先从一个例子开始理解。正文开始后，标题旁的动画就会消失。' }]
      : mode === 'reasoning' ? [{ id: 'reasoning', type: 'reasoning-summary', source: '正在核对问题条件。' }] : [],
  };
  return <><nav aria-label="预览阶段">{[['waiting','等待'],['reasoning','思考'],['search','搜索'],['body','正文开始'],['done','结束'],['failed','失败'],['cancelled','取消']].map(([id,label]) => <button key={id} onClick={() => setMode(id)}>{label}</button>)}</nav>
    <TeacherMessage slug="preview" message={message} sourceNames={new Map()} reasoningOpen={open} setReasoningOpen={(_id,value) => setOpen(value)} streaming={!['done', 'failed', 'cancelled'].includes(mode)} notice={mode === 'search' ? '正在搜索相关资料…' : undefined} />
  </>;
}
createRoot(document.getElementById('message')!).render(<QueryClientProvider client={client}><MemoryRouter><Preview /></MemoryRouter></QueryClientProvider>);
