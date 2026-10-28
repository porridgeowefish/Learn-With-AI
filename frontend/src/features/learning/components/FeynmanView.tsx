import { FormEvent, useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useLocation } from 'react-router-dom';

import { MarkdownView } from '@/shared/primitive/MarkdownView';
import { FEYNMAN_FINISH, feynmanKeys, streamFeynman, useCreateFeynmanSession, useFeynmanSessions, type FeynmanMessage } from '../api/feynman';
import s from './FeynmanView.module.css';

interface Live { learner?: string; text: string }

/** 讲给教师听: the learner explains, the teacher plays a curious novice who
 * keeps asking, then reviews the explanation against the review asset. */
export function FeynmanView({ slug }: { slug: string }) {
  const client = useQueryClient();
  const sessions = useFeynmanSessions(slug);
  const create = useCreateFeynmanSession(slug);
  const list = sessions.data ?? [];
  const [index, setIndex] = useState<number | null>(null);
  const [input, setInput] = useState('');
  const [live, setLive] = useState<Live | null>(null);
  const [error, setError] = useState('');
  const controller = useRef<AbortController | null>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const {search}=useLocation();
  const requestedSession=new URLSearchParams(search).get('session');
  const requestedMessage=new URLSearchParams(search).get('message');
  const located=useRef('');
  const requestedIndex=list.findIndex(s=>s.sessionId===requestedSession);
  const current = index === null ? list.length - 1 : index;
  const session = list[current];
  const latest = current === list.length - 1;

  useEffect(() => () => controller.current?.abort(), []);
  useEffect(() => { setIndex(null); setLive(null); setError(''); setInput(''); }, [slug]);
  useEffect(() => { if(requestedIndex>=0){setIndex(requestedIndex);located.current='';} }, [requestedIndex,search]);
  useEffect(() => {
    if(requestedMessage && located.current!==requestedMessage){
      const element=document.getElementById('message-'+requestedMessage);
      if(element){element.scrollIntoView?.({block:'center'});located.current=requestedMessage;}
      return;
    }
    if(!requestedMessage || live)endRef.current?.scrollIntoView?.({ block: 'end' });
  }, [session?.messages.length, session?.sessionId, live?.text, requestedMessage]);

  const run = async (sessionId: string, content: string) => {
    const next = new AbortController();
    controller.current = next;
    setError('');
    setLive({ learner: content || undefined, text: '' });
    try {
      await streamFeynman(slug, sessionId, { content }, delta => setLive(value => value ? { ...value, text: value.text + delta } : value), next.signal);
    } catch (cause) {
      if (!next.signal.aborted) setError(readableError(cause));
    } finally {
      await client.invalidateQueries({ queryKey: feynmanKeys.sessions(slug) });
      if (controller.current === next) controller.current = null;
      setLive(null);
    }
  };

  const start = async () => {
    setError('');
    try {
      const created = await create.mutateAsync();
      setIndex(null);
      await run(created.sessionId, '');
    } catch (cause) {
      setError(readableError(cause));
    }
  };

  const send = async (content: string) => {
    const text = content.trim();
    if (!text || !session || live) return;
    setInput(current => current.trim() === text ? '' : current);
    await run(session.sessionId, text);
  };

  const submit = (event: FormEvent) => { event.preventDefault(); void send(input); };

  if (sessions.isLoading) return <div className={s.empty}>正在读取讲解记录…</div>;

  return <section className={s.feynman} aria-label="讲给教师听">
    <header className={s.head}>
      <div>
        <span>费曼学习</span>
        <h2>讲给教师听</h2>
        <p>你来讲，教师扮演一个完全不懂的新手不停追问；讲完点“我讲完了”，教师会对照复习稿指出讲漏和讲错的地方。</p>
      </div>
      <div className={s.headActions}>
        {list.length > 1 && <nav className={s.pager} aria-label="讲解轮次">
          <button type="button" onClick={() => setIndex(Math.max(0, current - 1))} disabled={current <= 0 || Boolean(live)}>上一次</button>
          <span>第 {current + 1} / {list.length} 次</span>
          <button type="button" onClick={() => setIndex(current + 1 >= list.length - 1 ? null : current + 1)} disabled={latest || Boolean(live)}>下一次</button>
        </nav>}
        {session && <button type="button" onClick={() => void start()} disabled={create.isPending || Boolean(live)}>开始新一轮</button>}
      </div>
    </header>

    {error && <div className={s.error} role="alert">{error}</div>}

    {!session && !live ? <div className={s.empty}>
      <p>还没有讲过。教师会先列出几个它“听说过但不懂”的主题，你挑一个开始讲。</p>
      <button type="button" className={s.primary} onClick={() => void start()} disabled={create.isPending}>开始讲解</button>
    </div> : <>
      <div className={s.transcript} role="log" aria-label="讲解对话">
        {(session?.messages ?? []).map(message => <Bubble key={message.id} message={message} />)}
        {live?.learner && <Bubble message={{ id: 'live-learner', role: 'learner', content: live.learner, status: 'completed', createdAt: '' }} />}
        {live && <div className={s.teacher}><span className={s.speaker}>教师（新手视角）</span>{live.text ? <MarkdownView source={live.text} streaming /> : <span className={s.thinking} role="status">教师正在想问题…</span>}</div>}
        <div ref={endRef} />
      </div>
      {latest ? <form className={s.composer} onSubmit={submit}>
        <textarea value={input} onChange={event => setInput(event.target.value)} onKeyDown={event => {
          if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); event.currentTarget.form?.requestSubmit(); }
        }} placeholder={live ? '教师正在回应…' : '用自己的话讲给教师听，或回答它的问题…'} aria-label="我的讲解" rows={2} disabled={Boolean(live)} />
        <div className={s.composerActions}>
          <button type="button" className={s.finish} onClick={() => void send(FEYNMAN_FINISH)} disabled={Boolean(live) || !(session?.messages.some(message => message.role === 'learner'))}>我讲完了，请点评</button>
          <button type="submit" className={s.primary} disabled={Boolean(live) || !input.trim()}>发送</button>
        </div>
      </form> : <p className={s.readonly}>这是之前的一次讲解，只能查看。点“开始新一轮”或翻到最新一次继续讲。</p>}
    </>}
  </section>;
}

function Bubble({ message }: { message: FeynmanMessage }) {
  if (message.role === 'learner') return <div id={'message-'+message.id} className={s.learner}><MarkdownView source={message.content} /></div>;
  return <div className={s.teacher}>
    <span className={s.speaker}>教师（新手视角）</span>
    <MarkdownView source={message.content} />
    {message.status !== 'completed' && <p className={s.notice}>这条回应没有完整生成，可以继续讲或重新发送。</p>}
  </div>;
}

function readableError(cause: unknown) {
  const body = (cause as { body?: { error?: { message?: string } } })?.body;
  const message = body?.error?.message || (cause as Error)?.message || '';
  if (/not configured|provider not found/i.test(message)) return '还没有可用的模型连接，请先到“设置 → 模型”配置，并绑定教师用途。';
  return message || '教师暂时无法回应，请稍后重试。';
}
