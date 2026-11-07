import { useEffect, useRef, useState, type ClipboardEvent, type DragEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { learningWorkspaceKeys, MAX_SOURCE_FILE_BYTES } from '../api/learningWorkspace';
import { UploadIcon, MicrophoneIcon, StopIcon } from '@/shared/primitive/Icons';
import type { ReactNode } from 'react';
import { audioFileToWave } from './chatAudio';
import s from './ChatMediaInput.module.css';
import { useVoiceInput } from './useVoiceInput';

export interface ChatAttachment {
  attachmentId: string;
  kind: 'image' | 'audio' | 'source';
  name: string;
  status: 'ready' | 'empty';
  text: string;
}
export const chatAttachmentURL = (slug: string, id: string) => `/api/projects/${encodeURIComponent(slug)}/conversation/attachments/${encodeURIComponent(id)}/file`;

export function useChatMedia(slug: string) {
  const queryClient = useQueryClient();
  const voice = useVoiceInput(slug);
  const [attachments, setAttachments] = useState<ChatAttachment[]>([]);
  const language = 'auto';
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  const count = useRef(0);
  const failedUpload = useRef<{ files: File[]; language: string } | null>(null);
  const uploadController = useRef<AbortController | null>(null);
  const generation = useRef(0);
  useEffect(() => {
    setAttachments([]); count.current = 0; failedUpload.current = null; setPending(false); setError('');
    return () => { generation.current++; uploadController.current?.abort(); uploadController.current = null; };
  }, [slug]);

  const addFiles = async (files: File[], selectedLanguage = language) => {
    if (uploadController.current || voice.hasRecording || voice.pending) return;
    if (count.current + files.length > 4) { setError('每条消息最多4个附件'); return; }
    const token = generation.current;
    const controller = new AbortController(); uploadController.current = controller;
    let nextFile = 0;
    failedUpload.current = null;
    setPending(true); setError('');
    try {
      for (const original of files) {
        const isDocument = /\.(pdf|md|markdown|txt|pptx|docx|xlsx|csv)$/i.test(original.name);
        if (isDocument) {
          if (original.size > MAX_SOURCE_FILE_BYTES) throw new Error('文档不得超过100MiB');
          const body = new FormData(); body.set('operationId', crypto.randomUUID()); body.set('displayName', original.name); body.set('file', original);
          const response = await fetch(`/api/projects/${encodeURIComponent(slug)}/sources`, { method: 'POST', body, signal: controller.signal });
          const data = await response.json();
          if (!response.ok) throw new Error(typeof data.error === 'string' ? data.error : '附件上传失败，请重试');
          if (token !== generation.current) return;
          count.current++; nextFile++;
          setAttachments(current => [...current, { attachmentId: data.source.sourceId, kind: 'source', name: original.name, status: 'ready', text: '' }]);
          void queryClient.invalidateQueries({ queryKey: learningWorkspaceKeys.sources(slug) });
          continue;
        }
        if (original.size > 20 * 1024 * 1024) throw new Error('附件不得超过20MiB');
        const isAudio = original.type.startsWith('audio/') || /\.(wav|mp3|m4a|ogg|webm|flac)$/i.test(original.name);
        const file = isAudio ? await audioFileToWave(original) : original;
        if (token !== generation.current || controller.signal.aborted) return;
        const body = new FormData(); body.append('file', file); body.append('language', isAudio ? selectedLanguage : 'zh-CN');
        const response = await fetch(`/api/projects/${encodeURIComponent(slug)}/conversation/attachments`, { method: 'POST', body, signal: controller.signal });
        const data = await response.json();
        if (!response.ok) throw new Error(typeof data.error === 'string' ? data.error : '附件解析失败，请重试');
        if (token !== generation.current) return;
        count.current++; nextFile++; setAttachments(current => [...current, data as ChatAttachment]);
      }
    } catch (cause) { if (token === generation.current && !controller.signal.aborted) { failedUpload.current = { files: files.slice(nextFile), language: selectedLanguage }; setError((cause as Error).message || '附件解析失败'); } }
    finally { if (uploadController.current === controller) { uploadController.current = null; setPending(false); } }
  };

  return {
    attachments, pending: pending || voice.pending, recording: voice.recording, hasRecording: voice.hasRecording,
    voicePending: voice.pending, seconds: voice.seconds, error: error || voice.error, addFiles,
    startRecording: () => { if (uploadController.current) return Promise.resolve(); setError(''); return voice.startRecording(); },
    stopRecording: voice.stopRecording, cancelRecording: voice.cancelRecording,
    canRetry: Boolean(failedUpload.current),
    retryUpload: () => { const failed = failedUpload.current; if (failed) void addFiles(failed.files, failed.language); },
    clear: () => { count.current = 0; setAttachments([]); },
    restore: (items: ChatAttachment[]) => { setAttachments(current => { const next = [...items, ...current].filter((a, index, all) => all.findIndex(b => b.attachmentId === a.attachmentId) === index).slice(0, 4); count.current = next.length; return next; }); },
    remove: (id: string) => { count.current = Math.max(0, count.current - 1); setAttachments(current => current.filter(a => a.attachmentId !== id)); },
    onDrop: (event: DragEvent) => { if (event.dataTransfer.files.length) { event.preventDefault(); void addFiles(Array.from(event.dataTransfer.files)); } },
    onPaste: (event: ClipboardEvent) => { const files = Array.from(event.clipboardData.files).filter(file => file.type.startsWith('image/')); if (files.length) { event.preventDefault(); void addFiles(files); } },
  };
}

export function ChatMediaInput({ media, children, leadingActions }: { media: ReturnType<typeof useChatMedia>; children?: ReactNode; leadingActions?: ReactNode }) {
  const input = useRef<HTMLInputElement>(null);
  return <div className={s.root}>
    {media.attachments.length > 0 && <div className={s.attachments}>{media.attachments.map(a => <div className={s.chip} key={a.attachmentId}>
      <span title={a.kind === 'source' ? '已上传，教师可按需查阅；解析进度见附件页' : a.name}>{a.name}</span><button type="button" aria-label={`移除 ${a.name}`} onClick={() => media.remove(a.attachmentId)}>×</button>
    </div>)}</div>}
    <div className={s.controls}>
      <input ref={input} hidden type="file" multiple accept="image/png,image/jpeg,image/gif,audio/*,.pdf,.md,.markdown,.txt,.pptx,.docx,.xlsx,.csv" aria-label="上传附件" onChange={event => { void media.addFiles(Array.from(event.target.files ?? [])); event.target.value = ''; }} />
      <button type="button" className={s.upload} aria-label="上传图片、音频或附件" title="上传图片、音频或附件" disabled={media.pending || media.hasRecording} onClick={() => input.current?.click()}><UploadIcon size={21} /></button>
      {leadingActions}
      {media.pending && <span role="status" className={s.status}>{media.voicePending ? '正在转写…' : '正在处理附件…'}</span>}
      <div className={s.actions}>
        {children}
      </div>
    </div>
    {media.error && <div><p role="alert" className={s.error}>{media.error}</p>{media.canRetry && <button type="button" disabled={media.pending} onClick={media.retryUpload}>重试解析</button>}</div>}
  </div>;
}

export function ChatMicrophone({ media, onStop }: { media: ReturnType<typeof useChatMedia>; onStop?: () => void }) {
  const label = media.hasRecording ? media.recording ? '停止转写' : '重试转写' : '开始录音';
  const minutes = Math.floor(media.seconds / 60);
  const duration = `${minutes}:${String(media.seconds % 60).padStart(2, '0')}`;
  return <button type="button" className={s.microphone} data-recording={media.recording || undefined} aria-label={label} title={label} disabled={media.pending} onClick={() => void (media.hasRecording ? onStop ? onStop() : media.stopRecording() : media.startRecording())}>
    {media.recording ? <><StopIcon size={20} /><span role="status">{duration}</span></> : <MicrophoneIcon size={21} />}
  </button>;
}

export function ChatMediaBlock({ slug, id, kind, name, text }: { slug: string; id: string; kind: 'image' | 'audio'; name?: string; text?: string }) {
  return <div className={s.block}>
    {kind === 'image' ? <a href={chatAttachmentURL(slug, id)} target="_blank" rel="noreferrer"><img loading="lazy" src={chatAttachmentURL(slug, id)} alt={name || '聊天图片'} /></a> : <audio controls preload="none" src={chatAttachmentURL(slug, id)} aria-label={name || '聊天音频'} />}
    <span className={s.name}>{name}</span>
    {text && !text.startsWith('[图片未提取') && !text.startsWith('[音频未识别') && <details><summary>{kind === 'image' ? '提取文字' : '语音文字'}</summary><p>{text}</p></details>}
  </div>;
}
