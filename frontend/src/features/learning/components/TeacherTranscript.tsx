import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation } from 'react-router-dom';
import { SearchIcon } from '@/shared/primitive/Icons';
import type { ConversationMessage } from '../api/learningWorkspace';
import type { TeacherSession } from '../teacher/useTeacherSession';
import { projectTeacherMessages } from '../teacher/sessionState';
import { TeacherMessage, messageMarkdown } from './TeacherMessage';
import { QuickReplies, readLearningProgress } from './LearningProgress';
import s from './TeacherView.module.css';

export function TeacherTranscript({ slug, session, sourceNames, onSend }: {
  slug: string; session: TeacherSession; sourceNames: Map<string, string>; onSend: (text: string) => void;
}) {
  const { conversation, live } = session;
  const sessionRef = useRef(session); sessionRef.current = session;
  const viewport = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const {search}=useLocation();
  const requestedMessage=new URLSearchParams(search).get('message');
  const follow = useRef(!requestedMessage);
  const located=useRef('');
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [copied, setCopied] = useState('');
  const copyTimer = useRef<ReturnType<typeof setTimeout>>();
  const open = useCallback((id: string, value: boolean) => setExpanded(current => ({ ...current, [id]: value })), []);
  const ready = Boolean(conversation.data);
  const messages = projectTeacherMessages(conversation.data?.messages ?? [], live);
  const persistentIDs = useMemo(() => new Set(conversation.data?.messages.map(message => message.id)), [conversation.data?.messages]);
  const lastTeacher = [...(conversation.data?.messages ?? [])].reverse().find(message => message.role === 'teacher');
  const last = conversation.data?.messages.at(-1);
  const quickReply = !live && !conversation.data?.queue?.length && last?.role === 'teacher' && last.status === 'completed' ? last : null;

  useEffect(()=>{
    if(!requestedMessage||located.current===requestedMessage)return;
    const element=document.getElementById('message-'+requestedMessage);
    if(element){located.current=requestedMessage;follow.current=false;element.scrollIntoView({block:'center'});}
    else if(conversation.hasPrevious&&!conversation.isLoadingPrevious)void conversation.loadPrevious();
  },[requestedMessage,conversation.data,conversation.hasPrevious,conversation.isLoadingPrevious]);
  useEffect(() => {
    if (live?.teacherId) setExpanded(current => current['streaming-teacher'] ? { ...current, [live.teacherId!]: true, 'streaming-teacher': false } : current);
  }, [live?.teacherId]);
  const turnKey = live?.learner.operationId ?? live?.responseId ?? live?.learner.id;
  useEffect(() => { if (turnKey) follow.current = true; }, [turnKey]);
  useEffect(() => {
    if (!ready) return;
    follow.current = !requestedMessage;
    const scroll = () => { if (viewport.current && follow.current) viewport.current.scrollTop = viewport.current.scrollHeight; };
    scroll();
    const frame = requestAnimationFrame(scroll);
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(scroll);
    if (content.current) observer?.observe(content.current);
    return () => { cancelAnimationFrame(frame); observer?.disconnect(); };
  }, [ready]);
  useEffect(() => {
    const timer = setTimeout(() => { if (viewport.current && follow.current) viewport.current.scrollTop = viewport.current.scrollHeight; }, 96);
    return () => clearTimeout(timer);
  }, [conversation.data?.messages.length, live?.text, live?.reasoning, live?.notice, live?.learner.id]);
  useEffect(() => () => clearTimeout(copyTimer.current), []);
  const copy = useCallback(async (message: ConversationMessage) => {
    try {
      await navigator.clipboard.writeText(messageMarkdown(message));
      setCopied(message.id); clearTimeout(copyTimer.current); copyTimer.current = setTimeout(() => setCopied(''), 1600);
    } catch { sessionRef.current.reportError('复制失败：浏览器未授权剪贴板访问'); }
  }, []);
  const download = useCallback((message: ConversationMessage) => {
    const url = URL.createObjectURL(new Blob([messageMarkdown(message)], { type: 'text/markdown;charset=utf-8' }));
    const anchor = document.createElement('a'); anchor.href = url; anchor.download = `teacher-message-${message.id}.md`; anchor.click(); URL.revokeObjectURL(url);
  }, []);
  const regenerate = useCallback(() => { void sessionRef.current.regenerate(); }, []);
  const loadPrevious = async () => {
    const element = viewport.current;
    if (!element || conversation.isLoadingPrevious) return;
    const height = element.scrollHeight, top = element.scrollTop;
    follow.current = false;
    await conversation.loadPrevious();
    requestAnimationFrame(() => { if (viewport.current) viewport.current.scrollTop = top + viewport.current.scrollHeight - height; });
  };

  return <div className={s.transcript} ref={viewport} role="log" aria-label="教师对话" aria-live="off" aria-busy={Boolean(live)} onScroll={() => {
    const element = viewport.current;
    if (element) follow.current = element.scrollHeight - element.scrollTop - element.clientHeight < 160;
  }}><div className={s.transcriptContent} ref={content}>
    {conversation.isLoading && <div className={s.loading}>教师正在准备对话…</div>}
    {conversation.hasPrevious && <button type="button" className={s.loadPrevious} onClick={() => void loadPrevious()} disabled={conversation.isLoadingPrevious}>{conversation.isLoadingPrevious ? '正在加载…' : '加载更早对话'}</button>}
    {messages.map(message => {
      const streaming = message.role === 'teacher' && !persistentIDs.has(message.id) && Boolean(live);
      return <TeacherMessage key={message.id} slug={slug} message={message} sourceNames={sourceNames} reasoningOpen={Boolean(expanded[message.id])} setReasoningOpen={open} streaming={streaming} notice={streaming ? live?.notice : undefined} actions={{
        copied: copied === message.id, onCopy: copy, onDownload: download,
        canRegenerate: !live && message.id === lastTeacher?.id && Boolean(conversation.data?.latestResponseId), onRegenerate: regenerate,
      }} />;
    })}
    {live?.reading && <div className={s.searches} role="status">{live.reading.operation === 'search' ? '正在查找资料' : '正在查阅'}{live.reading.sourceId && <> · {sourceNames.get(live.reading.sourceId) ?? '资料'}</>}{live.reading.displayRange && <> · {live.reading.displayRange}</>}</div>}
    {!!live?.searches.length && <div className={s.searches} role="status">{live.searches.map((search, index) => <span key={index} className={s.searchChip} data-failed={search.failed || undefined}><SearchIcon size={12} />已搜索：{search.query}{typeof search.count === 'number' ? ` · ${search.count} 条结果` : search.failed ? ' · 暂不可用' : ' · 检索中…'}</span>)}</div>}
    {quickReply && <QuickReplies suggestions={readLearningProgress(messageMarkdown(quickReply))?.replies ?? []} onSend={onSend} />}
    {session.error && <div className={s.error} role="alert">{session.error}{session.live && session.connection === 'disconnected' && <button type="button" onClick={() => void session.reconnect()}>重新连接</button>}</div>}
    <div className={s.streamAnchor} aria-hidden="true" />
  </div></div>;
}
