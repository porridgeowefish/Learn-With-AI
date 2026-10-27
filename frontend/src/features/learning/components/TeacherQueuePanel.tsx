import { useRef, useState } from 'react';
import { EditIcon, SteerIcon, TrashIcon } from '@/shared/primitive/Icons';
import type { TeacherSession } from '../teacher/useTeacherSession';
import s from './TeacherView.module.css';

export function TeacherQueuePanel({ session }: { session: TeacherSession }) {
  const [editing, setEditing] = useState<{ id: string; content: string }>();
  const composing = useRef(false);
  const [saving, setSaving] = useState(false);
  const save = async () => {
    if (!editing?.content.trim() || saving) return;
    setSaving(true);
    const saved = await session.edit(editing.id, editing.content.trim());
    setSaving(false); if (saved) setEditing(undefined);
  };
  const queue = session.conversation.data?.queue ?? [];
  const paused = session.conversation.data?.queuePaused;
  if (!queue.length && !paused) return null;
  return <div className={s.queueBar} aria-label="等待队列">
    {paused && <div className={s.queuePause}><span>已暂停，待发送内容已保留</span><button type="button" onClick={() => void session.resume()}>继续发送</button></div>}
    <div className={s.queueItems}>{queue.map(item => editing?.id === item.queueId ? <div key={item.queueId} className={s.queueEdit}>
      <textarea value={editing.content} rows={2} onChange={event => setEditing({ id: item.queueId, content: event.target.value })}
        onCompositionStart={() => { composing.current = true; }} onCompositionEnd={() => { composing.current = false; }}
        onKeyDown={event => {
          if (composing.current || event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229) return;
          if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); void save(); }
          if (event.key === 'Escape') setEditing(undefined);
        }} aria-label="编辑排队消息" autoFocus />
      <button type="button" disabled={saving} onClick={() => void save()}>保存</button>
      <button type="button" onClick={() => setEditing(undefined)}>取消</button>
    </div> : <div key={item.queueId} className={s.queueChip}>
      <span className={s.queueContent}>{item.content || item.mediaBlocks?.map(block => block.name).join('、')}</span>
      <span className={s.queueActions}>
        <button type="button" className={s.iconButton} title="编辑这条排队消息" aria-label={`编辑排队消息 ${item.content}`} onClick={() => setEditing({ id: item.queueId, content: item.content })}><EditIcon /></button>
        <button type="button" className={s.iconButton} title="立即引导：打断当前回答，先回应这条" aria-label={`立即引导 ${item.content}`} onClick={() => void session.steer(item)} disabled={Boolean(session.live && !session.live.responseId)}><SteerIcon /></button>
        <button type="button" className={s.iconButton} title="移出队列" aria-label={`移除排队消息 ${item.content}`} onClick={() => void session.discard(item.queueId)}><TrashIcon /></button>
      </span>
    </div>)}</div>
  </div>;
}
