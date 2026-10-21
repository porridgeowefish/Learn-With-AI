import { useCallback, useEffect, useRef, useState, type ClipboardEvent, type DragEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { learningWorkspaceKeys, MAX_SOURCE_FILE_BYTES } from '../api/learningWorkspace';
import { UploadIcon, MicrophoneIcon, StopIcon } from '@/shared/primitive/Icons';
import type { ReactNode } from 'react';
import { http } from '@/shared/client';
import { audioFileToWave, encodeWave } from './chatAudio';
import s from './ChatMediaInput.module.css';

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
  const [attachments, setAttachments] = useState<ChatAttachment[]>([]);
  const [language, setLanguage] = useState('zh-CN');
  const [pending, setPending] = useState(false);
  const [recording, setRecording] = useState(false);
  const [error, setError] = useState('');
  const [seconds, setSeconds] = useState(0);
  const count = useRef(0);
  const failedUpload = useRef<{ files: File[]; language: string } | null>(null);
  const uploadController = useRef<AbortController | null>(null);
  const generation = useRef(0);
  const capture = useRef<{ context: AudioContext; stream: MediaStream; source: MediaStreamAudioSourceNode; processor: ScriptProcessorNode; mute: GainNode; timer: number; chunks: Float32Array[]; rate: number; language: string } | null>(null);
  const releaseCapture = useCallback(() => {
    const current = capture.current; capture.current = null;
    if (current) {
      clearInterval(current.timer); current.processor.onaudioprocess = null;
      current.processor.disconnect(); current.source.disconnect(); current.mute.disconnect();
      current.stream.getTracks().forEach(track => track.stop()); void current.context.close();
    }
    return current;
  }, []);
  useEffect(() => {
    setAttachments([]); count.current = 0; failedUpload.current = null; setPending(false); setRecording(false); setError('');
    return () => { generation.current++; uploadController.current?.abort(); uploadController.current = null; releaseCapture(); };
  }, [slug, releaseCapture]);

  const addFiles = async (files: File[], selectedLanguage = language) => {
    if (uploadController.current || capture.current) return;
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
        const body = new FormData(); body.append('file', file); body.append('language', selectedLanguage);
        const response = await fetch(`/api/projects/${encodeURIComponent(slug)}/conversation/attachments`, { method: 'POST', body, signal: controller.signal });
        const data = await response.json();
        if (!response.ok) throw new Error(typeof data.error === 'string' ? data.error : '附件解析失败，请重试');
        if (token !== generation.current) return;
        count.current++; nextFile++; setAttachments(current => [...current, data as ChatAttachment]);
      }
    } catch (cause) { if (token === generation.current && !controller.signal.aborted) { failedUpload.current = { files: files.slice(nextFile), language: selectedLanguage }; setError((cause as Error).message || '附件解析失败'); } }
    finally { if (uploadController.current === controller) { uploadController.current = null; setPending(false); } }
  };

  const stopRecording = async () => {
    const current = releaseCapture(); setRecording(false);
    if (!current) return;
    try {
      const length = current.chunks.reduce((sum, chunk) => sum + chunk.length, 0);
      const samples = new Float32Array(length); let offset = 0;
      for (const chunk of current.chunks) { samples.set(chunk, offset); offset += chunk.length; }
      const wave = new File([encodeWave(samples, current.rate)], '录音.wav', { type: 'audio/wav' });
      await addFiles([wave], current.language);
    } catch (cause) { setError((cause as Error).message); }
  };
  const startRecording = async () => {
    if (pending || recording || uploadController.current || capture.current) return;
    if (count.current >= 4) { setError('每条消息最多4个附件'); return; }
    if (!window.confirm('是否允许使用系统麦克风录音？最长60秒，停止后仅在本机转写中文或英文，发送前可移除。')) return;
    if (!navigator.mediaDevices?.getUserMedia) { setError('当前环境无法访问麦克风，请检查系统和浏览器权限'); return; }
    const token = generation.current;
    failedUpload.current = null;
    setPending(true); setError('');
    let stream: MediaStream | null = null;
    let context: AudioContext | null = null;
    try {
      const capability = await http.get<{ languages: string[]; setupHint: string }>('/api/system/speech');
      if (token !== generation.current) return;
      if (!capability.languages?.includes(language)) throw new Error(capability.setupHint || '系统未安装所选语言的本地语音识别');
      stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true }, video: false });
      if (token !== generation.current) { stream.getTracks().forEach(track => track.stop()); return; }
      context = new AudioContext(); await context.resume();
      if (token !== generation.current) { stream.getTracks().forEach(track => track.stop()); await context.close(); return; }
      const source = context.createMediaStreamSource(stream);
      const processor = context.createScriptProcessor(4096, 1, 1);
      const mute = context.createGain(); mute.gain.value = 0;
      const chunks: Float32Array[] = []; let total = 0;
      processor.onaudioprocess = event => {
        const data = event.inputBuffer.getChannelData(0);
        const remaining = Math.max(0, Math.floor(context!.sampleRate * 60) - total);
        if (remaining) { const chunk = new Float32Array(data.subarray(0, remaining)); chunks.push(chunk); total += chunk.length; }
      };
      source.connect(processor); processor.connect(mute); mute.connect(context.destination);
      let elapsed = 0; setSeconds(0);
      const timer = window.setInterval(() => { elapsed++; setSeconds(elapsed); if (elapsed >= 60) void stopRecording(); }, 1000);
      capture.current = { stream, context, source, processor, mute, chunks, timer, rate: context.sampleRate, language };
      setRecording(true);
    } catch (cause) {
      stream?.getTracks().forEach(track => track.stop()); if (context && context.state !== 'closed') void context.close();
      if (token === generation.current) setError((cause as Error).name === 'NotAllowedError' ? '麦克风权限未获允许，请在系统或浏览器设置中授权后重试' : (cause as Error).message);
    } finally { if (token === generation.current) setPending(false); }
  };

  return {
    attachments, language, setLanguage, pending, recording, seconds, error, addFiles, startRecording, stopRecording,
    canRetry: Boolean(failedUpload.current),
    retryUpload: () => { const failed = failedUpload.current; if (failed) void addFiles(failed.files, failed.language); },
    clear: () => { count.current = 0; setAttachments([]); },
    restore: (items: ChatAttachment[]) => { setAttachments(current => { const next = [...items, ...current].filter((a, index, all) => all.findIndex(b => b.attachmentId === a.attachmentId) === index).slice(0, 4); count.current = next.length; return next; }); },
    remove: (id: string) => { count.current = Math.max(0, count.current - 1); setAttachments(current => current.filter(a => a.attachmentId !== id)); },
    onDrop: (event: DragEvent) => { if (event.dataTransfer.files.length) { event.preventDefault(); void addFiles(Array.from(event.dataTransfer.files)); } },
    onPaste: (event: ClipboardEvent) => { const files = Array.from(event.clipboardData.files).filter(file => file.type.startsWith('image/')); if (files.length) { event.preventDefault(); void addFiles(files); } },
  };
}

export function ChatMediaInput({ media, children }: { media: ReturnType<typeof useChatMedia>; children?: ReactNode }) {
  const input = useRef<HTMLInputElement>(null);
  return <div className={s.root}>
    {media.attachments.length > 0 && <div className={s.attachments}>{media.attachments.map(a => <div className={s.chip} key={a.attachmentId}>
      <span title={a.kind === 'source' ? '已上传，教师可按需查阅；解析进度见附件页' : a.name}>{a.name}</span><button type="button" aria-label={`移除 ${a.name}`} onClick={() => media.remove(a.attachmentId)}>×</button>
    </div>)}</div>}
    <div className={s.controls}>
      <input ref={input} hidden type="file" multiple accept="image/png,image/jpeg,image/gif,audio/*,.pdf,.md,.markdown,.txt,.pptx,.docx,.xlsx,.csv" aria-label="上传附件" onChange={event => { void media.addFiles(Array.from(event.target.files ?? [])); event.target.value = ''; }} />
      <button type="button" className={s.upload} aria-label="上传图片、音频或附件" title="上传图片、音频或附件" disabled={media.pending || media.recording} onClick={() => input.current?.click()}><UploadIcon size={21} /></button>
      {media.pending && <span role="status" className={s.status}>正在处理附件…</span>}
      <div className={s.actions}>
        <select aria-label="识别语言" title="识别语言" value={media.language} disabled={media.pending || media.recording} onChange={event => media.setLanguage(event.target.value)}><option value="zh-CN">中文</option><option value="en-US">English</option></select>
        {children}
      </div>
    </div>
    {media.error && <div><p role="alert" className={s.error}>{media.error}</p>{media.canRetry && <button type="button" disabled={media.pending} onClick={media.retryUpload}>重试解析</button>}</div>}
  </div>;
}

export function ChatMicrophone({ media }: { media: ReturnType<typeof useChatMedia> }) {
  const label = media.recording ? '停止录音' : '开始录音';
  return <button type="button" className={s.microphone} data-recording={media.recording || undefined} aria-label={label} title={label} disabled={media.pending} onClick={() => void (media.recording ? media.stopRecording() : media.startRecording())}>
    {media.recording ? <><StopIcon size={20} /><span role="status">{media.seconds}s</span></> : <MicrophoneIcon size={21} />}
  </button>;
}

export function ChatMediaBlock({ slug, id, kind, name, text }: { slug: string; id: string; kind: 'image' | 'audio'; name?: string; text?: string }) {
  return <div className={s.block}>
    {kind === 'image' ? <a href={chatAttachmentURL(slug, id)} target="_blank" rel="noreferrer"><img loading="lazy" src={chatAttachmentURL(slug, id)} alt={name || '聊天图片'} /></a> : <audio controls preload="none" src={chatAttachmentURL(slug, id)} aria-label={name || '聊天音频'} />}
    <span className={s.name}>{name}</span>
    {text && !text.startsWith('[图片未提取') && !text.startsWith('[音频未识别') && <details><summary>{kind === 'image' ? '提取文字' : '语音文字'}</summary><p>{text}</p></details>}
  </div>;
}
