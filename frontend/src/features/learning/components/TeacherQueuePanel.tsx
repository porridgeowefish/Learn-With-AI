import { useRef, useState } from 'react';
import { EditIcon, TrashIcon } from '@/shared/primitive/Icons';
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
  if (!queue.length) return null;
  return <div className={s.queueBar} aria-label="等待队列">
    {paused && <div className={s.queuePause}><button type="button" onClick={() => void session.resume()}>发送待发消息</button></div>}
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
        <button type="button" className={s.queueSend} title={session.live ? '结束当前回答，发送这条消息' : '发送这条消息'} aria-label={`直接发送 ${item.content}`} onClick={() => void session.steer(item)} disabled={Boolean(session.live && !session.live.responseId)}><span aria-hidden="true">✉</span>直接发送</button>
        <button type="button" className={s.iconButton} title="移出队列" aria-label={`移除排队消息 ${item.content}`} onClick={() => void session.discard(item.queueId)}><TrashIcon /></button>
      </span>
    </div>)}</div>
  </div>;
}
