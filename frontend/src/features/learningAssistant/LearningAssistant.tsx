import { useEffect, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Icon } from '@/shared/primitive/Icon';
import { managedProviderPrefix, readCloudStatus } from '@/features/cloud-account';
import { useAskAiSettings } from '@/features/settings';
import { ChatComposerFrame, ChatMediaInput, ChatSendButton, TeacherMessage, TeacherModelPicker, chatStyles, type ConversationMessage } from '@/features/learning';
import { ApiError } from '@/shared/client';
import { useHealth } from '@/app/api/health';
import { qk } from '@/shared/queryKeys';
import {
  assistantTurn, calendarKey, confirmCourse, readAssistantSettlements, uploadStudyAttachment, useStudyCalendar,
  type AssistantCreditUsage, type CourseProposal, type CreatedCourse, type StudyAttachment, type StudyLink,
} from './api';
import s from './LearningAssistant.module.css';

interface Message {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  links?: StudyLink[];
  proposal?: CourseProposal;
  created?: CreatedCourse;
  reviewId?: string;
  reminder?: boolean;
  attachmentNames?: string[];
  creditUsage?: AssistantCreditUsage;
  failed?: boolean;
  failureCode?: string;
  errorDetails?: string;
}

const STORAGE_PREFIX = 'lwa.study-assistant.v2:';
const confirmation = /^(?:(?:好(?:的)?|没问题)[，,、 ]*)?(?:(?:我)?确认(?:这个|这份|该)?(?:学习)?(?:计划|方案)?|同意(?:这个|这份)?(?:计划|方案)?|(?:就)?按(?:照)?(?:这个|这份)?(?:计划|方案)?(?:来|安排|执行)|可以[，,、 ]*(?:创建|开始)(?:学习主题|课程)?|就这么定了)\s*[。！!]?$/;
const noSourceNames = new Map<string, string>();
const openReasoning = () => undefined;
const waitingMessage: ConversationMessage = { id: 'assistant-loading', role: 'teacher', status: 'completed', blocks: [], createdAt: '' };
const starters = ['我想学博弈论', '帮我安排一门课的学习计划', '我之前学过什么？'];

function loadThread(storageKey?: string): Message[] {
  if (!storageKey) return [];
  try {
    const stored = JSON.parse(localStorage.getItem(storageKey) ?? '[]');
    return Array.isArray(stored) ? stored : [];
  } catch {
    return [];
  }
}

function toConversationMessage(item: Message): ConversationMessage {
  return {
    id: item.id, role: item.role === 'user' ? 'learner' : 'teacher',
    status: item.failureCode ? 'failed' : 'completed', failureCode: item.failureCode,
    blocks: item.content ? [{ id: item.id, type: 'markdown', source: item.content }] : [],
    createdAt: '', creditUsage: item.creditUsage,
  };
}

const message = (role: Message['role'], content: string): Message => ({ id: crypto.randomUUID(), role, content });

export function LearningAssistant() {
  const health = useHealth();
  const workspace = health.data?.workspace;
  if (health.isPending) return <div className={chatStyles.loading} role="status">正在准备对话…</div>;
  return <StudyChat key={workspace} storageKey={workspace ? STORAGE_PREFIX + workspace : undefined} />;
}

function StudyChat({ storageKey }: { storageKey?: string }) {
  const [messages, setMessages] = useState(() => loadThread(storageKey));
  const [input, setInput] = useState('');
  const [attachments, setAttachments] = useState<Array<StudyAttachment & { attachmentId: string }>>([]);
  const [uploading, setUploading] = useState(false);
  const [activeReview, setActiveReview] = useState<string>();
  const [waiting, setWaiting] = useState<'reply' | 'course'>();
  const [error, setError] = useState('');
  const [retry, setRetry] = useState<() => void>();
  const request = useRef<AbortController>();
  const viewport = useRef<HTMLDivElement>(null);
  const follow = useRef(true);
  const [search, setSearch] = useSearchParams();
  const query = useStudyCalendar();
  const client = useQueryClient();
  const cloud = useQuery({ queryKey: ['cloud-account'], queryFn: readCloudStatus, retry: false });
  const settings = useAskAiSettings();
  const [selection, setSelection] = useState(() => {
    try { return storageKey ? localStorage.getItem(storageKey + ':model') ?? '' : ''; }
    catch { return ''; }
  });
  const providers = (settings.data?.providers ?? []).filter(provider => provider.id && provider.model && (provider.kind === 'siwc' || provider.baseURL));
  const defaultLocal = settings.data?.bindings?.teacher?.providerId || settings.data?.default;
  const selectedCloud = cloud.data?.signedIn && cloud.data.selectedModel ? managedProviderPrefix + cloud.data.selectedModel : '';
  const providerId = (selection.startsWith(managedProviderPrefix) || providers.some(provider => provider.id === selection) ? selection : '') ||
    selectedCloud || providers.find(provider => provider.id === defaultLocal)?.id || providers[0]?.id || '';
  const selectedProvider = useRef(providerId);
  selectedProvider.current = providerId;
  const pendingRequests = [...new Set(messages.flatMap(item => item.creditUsage?.pendingRequests ?? []))].slice(0, 128);
  const settlements = useQuery({
    queryKey: ['assistant-settlements', storageKey, pendingRequests],
    queryFn: () => readAssistantSettlements(pendingRequests),
    enabled: !!cloud.data?.signedIn && pendingRequests.length > 0,
    refetchInterval: 30_000, retry: false,
  });
  useEffect(() => {
    if (!settlements.data || !Object.keys(settlements.data).length) return;
    const settled = settlements.data;
    setMessages(previous => previous.map(item => {
      const credit = item.creditUsage;
      if (!credit?.pendingRequests?.some(id => id in settled)) return item;
      const pendingRequests = credit.pendingRequests.filter(id => !(id in settled));
      const units = credit.pendingRequests.reduce((total, id) => total + (settled[id] ?? 0), credit.units);
      return { ...item, creditUsage: { units, pendingRequests, pending: pendingRequests.length > 0 } };
    }));
    void client.invalidateQueries({ queryKey: ['cloud-account'] });
  }, [settlements.data, client]);
  const lastReply = [...messages].reverse().find(item => item.role === 'assistant' && !item.reminder && !item.failed);
  const proposal = lastReply?.proposal && !messages.some(item => item.id === lastReply.id + ':created') ? lastReply : undefined;

  useEffect(() => () => request.current?.abort(), []);
  useEffect(() => {
    try { if (storageKey) localStorage.setItem(storageKey, JSON.stringify(messages.slice(-200))); }
    catch { /* Keep the open conversation usable when storage is unavailable. */ }
    if (viewport.current && follow.current) viewport.current.scrollTop = viewport.current.scrollHeight;
  }, [messages, waiting, storageKey]);

  useEffect(() => {
    if (!query.data) return;
    const { today, items } = query.data;
    const selected = search.get('review');
    const due = items.filter(item => item.kind === 'review' && !item.completed && (
      item.id === selected || item.date <= today
    ));
    if (!due.length) return;
    setMessages(previous => {
      const seen = new Set(previous.map(item => item.id));
      const unseen = due.filter(item => !seen.has('reminder:' + item.id));
      if (!unseen.length) return previous;
      return [...previous, ...unseen.map(item => ({
        id: 'reminder:' + item.id, role: 'assistant' as const, reviewId: item.id, reminder: true,
        content: item.course
          ? `该复习「${item.course}」里的「${item.title}」了。先试着回忆，再用一个新例子检验自己能不能用上它。

${item.detail ?? ''}`
          : `有一项复习可以接着做。

${item.title}

${item.detail ?? ''}`,
      }))];
    });
    if (selected) setSearch(previous => { const next = new URLSearchParams(previous); next.delete('review'); return next; }, { replace: true });
  }, [query.data, search, setSearch]);

  async function run(kind: 'reply' | 'course', work: (signal: AbortSignal) => Promise<void>) {
    if (request.current) return;
    const controller = new AbortController();
    request.current = controller;
    setWaiting(kind);
    setError('');
    setRetry(undefined);
    try {
      await work(controller.signal);
    } catch (problem) {
      if (!controller.signal.aborted) {
        const detail = problem instanceof Error ? problem.message : '暂时无法继续，请重试。';
        if (kind === 'reply') {
          const failure = problem instanceof ApiError && problem.body && typeof problem.body === 'object'
            ? problem.body as { failureCode?: string; creditUsage?: AssistantCreditUsage } : undefined;
          setMessages(previous => [...previous, {
            ...message('assistant', ''), failed: true, failureCode: failure?.failureCode,
            errorDetails: failure?.failureCode ? undefined : detail, creditUsage: failure?.creditUsage,
          }]);
          if (failure?.creditUsage || failure?.failureCode === 'insufficient_credits') {
            void client.invalidateQueries({ queryKey: ['cloud-account'] });
          }
        } else {
          setError(detail);
        }
        setRetry(() => () => void run(kind, work));
      }
    } finally {
      if (request.current === controller) request.current = undefined;
      if (!controller.signal.aborted) setWaiting(undefined);
    }
  }

  const create = (item: Message) => {
    const course = item.proposal;
    if (!course) return;
    void run('course', async signal => {
      const created = await confirmCourse(course, item.id, signal);
      setMessages(previous => previous.some(entry => entry.id === item.id + ':created') ? previous : [
        ...previous, { id: item.id + ':created', role: 'assistant', created,
          content: `「${created.title}」已经加入学习库。我把 ${created.planned} 个单元排进了学习日历，你可以从目录进入课堂。` },
      ]);
      void client.invalidateQueries({ queryKey: qk.projects.all() });
      void client.invalidateQueries({ queryKey: qk.folders.all() });
      void client.invalidateQueries({ queryKey: calendarKey });
    });
  };

  const send = (text = input, reviewId = activeReview) => {
    const content = text.trim();
    if ((!content && !attachments.length) || request.current || uploading || cloud.isPending || settings.isPending) return;
    follow.current = true;
    const user = { ...message('user', content || '请参考我发来的资料，和我讨论怎样安排学习。'),
      attachmentNames: attachments.map(file => file.name) };
    const history = [...messages, user];
    setMessages(history);
    setInput('');
    if (proposal && !reviewId && confirmation.test(content)) {
      create(proposal);
      return;
    }
    void run('reply', async signal => {
      const result = await assistantTurn(history.filter(item => !item.failed), attachments, reviewId, selectedProvider.current, signal);
      setMessages(previous => [...previous, {
        ...message('assistant', result.reply), links: result.links, proposal: result.course, creditUsage: result.creditUsage,
      }]);
      if (result.creditUsage) void client.invalidateQueries({ queryKey: ['cloud-account'] });
      if (result.completedReview) {
        setActiveReview(undefined);
        void client.invalidateQueries({ queryKey: calendarKey });
      }
    });
  };

  const attach = async (files: File[]) => {
    if (!files.length || uploading) return;
    setUploading(true);
    setError('');
    setRetry(undefined);
    const capacity = 32 - attachments.length;
    const problems: string[] = [];
    if (files.length > capacity) problems.push('每个对话最多保留 32 份附件。');
    for (const file of files.slice(0, capacity)) {
      try {
        const parsed = await uploadStudyAttachment(file);
        setAttachments(previous => [...previous, { ...parsed, attachmentId: crypto.randomUUID() }]);
      } catch (problem) {
        problems.push(file.name + '：' + (problem instanceof Error ? problem.message : '读取失败'));
      }
    }
    setError(problems.join(' '));
    setUploading(false);
  };

  const reviews = new Map(query.data?.items.filter(item => item.kind === 'review').map(item => [item.id, item]));
  return <div className={chatStyles.teacher} data-tour="welcome"><div className={chatStyles.conversationPane}>
    <div className={chatStyles.transcript} ref={viewport} aria-busy={waiting === 'reply'} onScroll={event => {
      const element = event.currentTarget;
      follow.current = element.scrollHeight - element.scrollTop - element.clientHeight < 160;
    }}>
      <div className={chatStyles.transcriptContent}>
        {messages.length === 0 ? <section className={s.welcome}>
          <div className={s.emblem}><Icon name="book" size={29} /></div>
          <h1>学习助手</h1>
          <p>告诉我你想学什么。我们可以一起拆解课程、制定计划，再把每个学习单元排进日历。</p>
          <div className={s.starters}>{starters.map(text => <button type="button" key={text}
            disabled={cloud.isPending || settings.isPending} onClick={() => send(text)}>
            <Icon name="zap" size={15} />{text}<span aria-hidden="true">→</span>
          </button>)}</div>
        </section> : <div role="log" aria-label="学习助手对话" aria-live="off">
          {messages.map(item => <TeacherMessage key={item.id} slug="" message={toConversationMessage(item)} speaker="学习助手"
            sourceNames={noSourceNames} reasoningOpen={false} setReasoningOpen={openReasoning} feedback={false} notice={item.errorDetails}>
              {!!item.attachmentNames?.length && <small className={s.attachmentNames}>{item.attachmentNames.join('、')}</small>}
              {item.links?.map(link => <div key={link.url} className={s.evidence}>
                <Link to={link.url}>{link.title}</Link>{link.quote && <blockquote>{link.quote}</blockquote>}
              </div>)}
              {item.proposal && <section className={s.proposal} aria-label="课程方案">
                <h2>{item.proposal.title}</h2>
                <ol>{item.proposal.units.map(unit => <li key={unit.title}>
                  <time dateTime={unit.date}>{unit.date.slice(5).replace('-', '月')}日</time>
                  <div><strong>{unit.title}</strong><p>{unit.goal}</p></div>
                </li>)}</ol>
                {proposal?.id === item.id && <button className={s.primary} type="button" disabled={!!waiting} onClick={() => create(item)}>确认并创建学习主题</button>}
              </section>}
              {item.created && <div className={s.links}>
                <Link to={`/project/${item.created.slug}`}>打开课程目录 <span aria-hidden="true">↗</span></Link>
                <Link to="/learning-calendar">查看学习日历 <span aria-hidden="true">↗</span></Link>
              </div>}
              {item.reviewId && reviews.has(item.reviewId) && !reviews.get(item.reviewId)?.completed &&
                <button className={s.secondary} type="button" disabled={!!waiting} onClick={() => {
                  setActiveReview(item.reviewId);
                  send(`现在来复习「${reviews.get(item.reviewId!)?.title}」。请给我一个能用上它的短场景。`, item.reviewId);
                }}>现在复习</button>}
          </TeacherMessage>)}
          {waiting === 'reply' && <TeacherMessage slug="" message={waitingMessage} speaker="学习助手" streaming feedback={false}
            sourceNames={noSourceNames} reasoningOpen={false} setReasoningOpen={openReasoning} />}
        </div>}
        {waiting === 'course' && <div role="status" className={chatStyles.loading}>正在保存课程和日历…</div>}
      </div>
    </div>
    <div className={chatStyles.composerDock}>
      {error && !uploading && <div className={chatStyles.error} role="alert">{error}</div>}
      {retry && <div className={chatStyles.failureActions}><button type="button" onClick={retry}>重试</button></div>}
      <ChatComposerFrame tour="assistant-composer" input={input} onInput={setInput} onSend={() => send()}
        onDrop={event => { if (event.dataTransfer.files.length) { event.preventDefault(); void attach(Array.from(event.dataTransfer.files)); } }}
        label="给学习助手发消息" placeholder="今天想学点什么？">
        <ChatMediaInput label="上传附件" sourceTitle="已读取，学习助手会参考附件内容"
          accept=".pdf,.docx,.pptx,.xlsx,.csv,.md,.markdown,.txt"
          media={{
            attachments: attachments.map(item => ({ ...item, kind: 'source' as const })), pending: uploading, error: '',
            remove: id => setAttachments(previous => previous.filter(item => item.attachmentId !== id)), addFiles: files => void attach(files),
          }}>
          <TeacherModelPicker label="选择学习助手模型" status={cloud.data} selected={providerId}
            localModels={providers.map(provider => ({ id: provider.id, label: provider.name ? `${provider.name} · ${provider.model}` : provider.model }))}
            onSelect={id => {
              setSelection(id);
              try { if (storageKey) localStorage.setItem(storageKey + ':model', id); }
              catch { /* Keep this selection for the current visit. */ }
            }} />
          <ChatSendButton disabled={!!waiting || uploading || cloud.isPending || settings.isPending || (!input.trim() && !attachments.length)} />
        </ChatMediaInput>
      </ChatComposerFrame>
    </div>
  </div></div>;
}
