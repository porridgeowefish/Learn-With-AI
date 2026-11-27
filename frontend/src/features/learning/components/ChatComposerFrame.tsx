import { useLayoutEffect, useRef, type ClipboardEventHandler, type DragEventHandler, type ReactNode, type RefObject } from 'react';
import { SendIcon } from '@/shared/primitive/Icons';
import s from './TeacherView.module.css';

export function ChatComposerFrame({ input, onInput, onSend, placeholder, label, tour, textareaRef, onComposition, onDrop, onPaste, children }: {
  input: string;
  onInput: (value: string) => void;
  onSend: () => void;
  placeholder: string;
  label?: string;
  tour: string;
  textareaRef?: RefObject<HTMLTextAreaElement>;
  onComposition?: (active: boolean) => void;
  onDrop?: DragEventHandler;
  onPaste?: ClipboardEventHandler;
  children: ReactNode;
}) {
  const internalRef = useRef<HTMLTextAreaElement>(null);
  const textbox = textareaRef ?? internalRef;
  const composing = useRef(false);
  useLayoutEffect(() => {
    const element = textbox.current;
    if (element) {
      element.style.height = 'auto';
      element.style.height = `${Math.min(150, Math.max(28, element.scrollHeight))}px`;
    }
  }, [input, textbox]);

  return <form data-tour={tour} className={s.composer}
    onSubmit={event => { event.preventDefault(); if (!composing.current) onSend(); }}
    onDragOver={event => { if (onDrop && event.dataTransfer.types.includes('Files')) event.preventDefault(); }}
    onDrop={onDrop} onPaste={onPaste}>
    <textarea ref={textbox} title="Enter 发送 · Shift + Enter 换行" value={input}
      aria-label={label} placeholder={placeholder} rows={1}
      onChange={event => onInput(event.target.value)}
      onCompositionStart={() => { composing.current = true; onComposition?.(true); }}
      onCompositionEnd={() => { composing.current = false; onComposition?.(false); }}
      onKeyDown={event => {
        if (event.key === 'Enter' && !event.shiftKey && !composing.current && !event.nativeEvent.isComposing && event.nativeEvent.keyCode !== 229) {
          event.preventDefault(); event.currentTarget.form?.requestSubmit();
        }
      }} />
    {children}
  </form>;
}

export function ChatSendButton({ label = '发送', title = label, disabled }: { label?: string; title?: string; disabled: boolean }) {
  return <button type="submit" className={s.send} aria-label={label} title={title} disabled={disabled}><SendIcon size={22} /></button>;
}
