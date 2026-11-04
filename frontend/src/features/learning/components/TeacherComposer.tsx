import { forwardRef, useEffect, useImperativeHandle, useLayoutEffect, useRef, useState } from 'react';
import { useAskAiSettings, useLearningProfile, effortNames } from '@/features/settings';
import { SendIcon, StopIcon } from '@/shared/primitive/Icons';
import { ChatMediaInput, ChatMicrophone, useChatMedia } from './ChatMediaInput';
import type { TeacherSession, TeacherSubmission } from '../teacher/useTeacherSession';
import s from './TeacherView.module.css';

export interface TeacherComposerHandle { sendText: (text: string) => void }

export const TeacherComposer = forwardRef<TeacherComposerHandle, {
  slug: string; title: string; session: TeacherSession; sourceCount: number;
}>(function TeacherComposer({ slug, title, session, sourceCount }, ref) {
  const media = useChatMedia(slug);
  const settings = useAskAiSettings();
  const profile = useLearningProfile();
  const [chosenEffort, setChosenEffort] = useState(() => localStorage.getItem(`lll.teacher.effort.${slug}`) ?? '');
  const [webSearch, setWebSearch] = useState(false);
  const models = (settings.data?.providers ?? []).filter(provider => provider.id && provider.model && (provider.kind === 'siwc' || provider.baseURL));
  const [providerId, setProviderId] = useState(() => localStorage.getItem(`lll.teacher.provider.${slug}`) ?? '');
  const capabilities = profile.data?.capabilities[providerId] ?? [];
  const preferredEffort = chosenEffort || profile.data?.settings.effort || 'medium';
  const effort = capabilities.includes(preferredEffort) ? preferredEffort : capabilities.includes('medium') ? 'medium' : '';
  const searchEnabled = !!profile.data?.settings.search.enabled;
  useEffect(() => { if (!searchEnabled) setWebSearch(false); }, [searchEnabled]);
  useEffect(() => { session.configure({ providerId, options: { effort, webSearch: searchEnabled && webSearch } }); }, [session.configure, providerId, effort, webSearch, searchEnabled]);
  const [input, setInput] = useState('');
  const [failed, setFailed] = useState<TeacherSubmission>();
  const [pending, setPending] = useState(false);
  const submitting = useRef(false);
  const submittingVersion = useRef(-1);
  const pendingOperation = useRef('');
  const version = useRef(0);
  const composing = useRef(false);
  const textarea = useRef<HTMLTextAreaElement>(null);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => {
    if (!models.some(provider => provider.id === providerId) && models.length) setProviderId(settings.data?.bindings?.teacher?.providerId || settings.data?.default || models[0].id);
  }, [providerId, settings.data, models]);
  useLayoutEffect(() => {
    const element = textarea.current;
    if (element) { element.style.height = 'auto'; element.style.height = `${Math.min(150, Math.max(28, element.scrollHeight))}px`; }
  }, [input]);

  const send = async (text: string, retry?: TeacherSubmission) => {
    if ((submitting.current && submittingVersion.current === version.current) || media.pending || media.recording || composing.current) return;
    const content = text.trim() || (media.attachments.some(a => a.kind === 'source') ? '请结合附件继续讲解。' : '');
    if (!content && !media.attachments.length && !retry?.attachments.length) return;
    const snapshot = version.current;
    const selectedProviderId = providerId || undefined;
    const sameFailure = failed && failed.content === content && failed.attachments.map(a => a.attachmentId).join() === media.attachments.map(a => a.attachmentId).join();
    const previous = retry ?? (sameFailure ? failed : undefined);
    const item = previous && previous.providerId === selectedProviderId ? previous : {
      operationId: crypto.randomUUID(),
      content: previous?.content ?? content,
      attachments: previous?.attachments ?? media.attachments,
      providerId: selectedProviderId,
      options: { effort, webSearch: searchEnabled && webSearch },
    };
    submitting.current = true; submittingVersion.current = snapshot; pendingOperation.current = item.operationId; setPending(true);
    let accepted = false;
    const release = () => { if (pendingOperation.current === item.operationId) { submitting.current = false; if (mounted.current) setPending(false); } };
    const result = await session.send(item, () => {
      accepted = true; release();
      if (!mounted.current) return;
      if (version.current === snapshot && input.trim() === item.content) { version.current++; setInput(''); }
      // Only consume attachments sealed into this request.
      item.attachments.forEach(a => media.remove(a.attachmentId));
      setFailed(current => current?.operationId === item.operationId ? undefined : current);
    });
    if (!mounted.current) return;
    if (!accepted) {
      release();
      if (!result) setFailed(item);
    }
  };
  useImperativeHandle(ref, () => ({ sendText: text => { void send(text); } }));

  return <>
    {failed && <div className={s.failedSubmission} role="status">
      <span>未发送：{failed.content || failed.attachments.map(a => a.name).join('、')}</span>
      <button type="button" disabled={pending} onClick={() => void send(failed.content, failed)}>重试发送</button>
      <button type="button" onClick={() => setFailed(undefined)}>关闭</button>
    </div>}
    <form data-tour="teacher-composer" className={s.composer}
      onSubmit={event => { event.preventDefault(); void send(input); }}
      onDragOver={event => { if (event.dataTransfer.types.includes('Files')) event.preventDefault(); }} onDrop={media.onDrop} onPaste={media.onPaste}>
      <textarea ref={textarea} title="Enter 发送 · Shift + Enter 换行" value={input}
        onChange={event => { version.current++; setPending(false); setInput(event.target.value); }}
        onCompositionStart={() => { composing.current = true; }} onCompositionEnd={() => { composing.current = false; }}
        onKeyDown={event => {
          if (event.key === 'Enter' && !event.shiftKey && !composing.current && !event.nativeEvent.isComposing && event.nativeEvent.keyCode !== 229) {
            event.preventDefault(); event.currentTarget.form?.requestSubmit();
          }
        }} placeholder={session.live ? '教师正在回答…发送将进入等待队列' : `和 ${title} 的教师继续讨论…`} rows={1} />
      <ChatMediaInput media={media} leadingActions={searchEnabled && <label className={s.searchToggle}>
        <span>联网搜索</span>
        <input type="checkbox" role="switch" aria-label="联网搜索" checked={webSearch} onChange={event => setWebSearch(event.target.checked)} />
        <span className={s.searchTrack} aria-hidden="true" />
      </label>}>
        {models.length > 0 && <select className={s.modelSelect} aria-label="选择教师模型" title="选择教师模型" value={providerId} onChange={event => {
          setProviderId(event.target.value); localStorage.setItem(`lll.teacher.provider.${slug}`, event.target.value);
        }}>{models.map(provider => <option key={provider.id} value={provider.id}>{provider.name ? `${provider.name} · ${provider.model}` : provider.model}</option>)}</select>}
        <span className={s.reasoningControl}>
          <select className={s.modelSelect} aria-label="思考强度" title={capabilities.length ? '使用模型原生思考参数，从下次发送开始生效' : '当前模型不支持调节思考强度'} disabled={!capabilities.length || !profile.data} value={effort} onChange={event => {
            setChosenEffort(event.target.value); localStorage.setItem(`lll.teacher.effort.${slug}`, event.target.value);
          }}>{!capabilities.length && <option value="">思考：不可调节</option>}{capabilities.map(value => <option key={value} value={value}>思考：{effortNames[value]}</option>)}</select>
        </span>
        <ChatMicrophone media={media} />
        {session.live?.responseId && <button type="button" className={s.stop} aria-label="停止" title="停止回答并暂停队列" onClick={() => void session.stop()}><StopIcon size={21} /></button>}
        <button type="submit" className={s.send} aria-label={session.live || session.conversation.data?.queuePaused ? '排队' : '发送'} title={session.live ? '加入等待队列' : '发送'} disabled={!profile.data || pending || (!input.trim() && !media.attachments.length) || media.pending || media.recording}><SendIcon size={22} /></button>
      </ChatMediaInput>
      {profile.isError && <div className={s.composerMeta} role="alert">回复设置读取失败，请刷新后重试。</div>}
      {sourceCount > 0 && <div className={s.composerMeta}><span className={s.materialStatus}>有 {sourceCount} 份附件可查阅</span></div>}
    </form>
  </>;
});
