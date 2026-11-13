import { useEffect, useRef, useState } from 'react';
import { useFontSize } from '@/shared/lib/fontSize';
import type { SavedHighlight } from '@/shared/lib/summaryHighlights';
import { connectPreview } from '@/shared/preview/connectPreview';
import { MAX_REVIEW_HTML, type PreviewSelection } from '@/shared/preview/protocol';
import s from './BodyAnnotations.module.css';

const emptyHighlights: SavedHighlight[] = [];

type ReviewDocumentProps = {
  content: string;
  highlights?: SavedHighlight[];
  clearSelection?: number;
  onSelection?: (selection: PreviewSelection | null) => void;
};

export function ReviewDocument(props: ReviewDocumentProps) {
  const [attempt, retry] = useState(0);
  if (props.content.length > MAX_REVIEW_HTML) return <p role="alert">复习稿过长，无法预览。可生成一份更短的复习稿。</p>;
  return <ReviewFrame key={attempt} {...props} onRetry={() => retry(count => count + 1)} />;
}

function ReviewFrame({ content, highlights = emptyHighlights, clearSelection, onSelection, onRetry }: ReviewDocumentProps & { onRetry: () => void }) {
  const frame = useRef<HTMLIFrameElement>(null);
  const connection = useRef<ReturnType<typeof connectPreview>>();
  const selectionHandler = useRef(onSelection);
  selectionHandler.current = onSelection;
  const [status, setStatus] = useState<'loading' | 'ready' | 'failed'>('loading');
  const size = useFontSize(state => state.size);

  useEffect(() => {
    const element = frame.current!;
    setStatus('loading');
    const session = connectPreview(element, {
      init: { type: 'review:init', content, highlights: [], theme: {} },
      onFailure() {
        selectionHandler.current?.(null);
        setStatus('failed');
      },
      onMessage(message) {
        if (message.type === 'preview:rendered') setStatus('ready');
        if (message.type === 'preview:size' && typeof message.height === 'number' && Number.isFinite(message.height)) {
          element.style.height = `${Math.min(24_000, Math.max(40, Math.ceil(message.height) + 2))}px`;
        }
        if (message.type === 'preview:selection') {
          const selected = message.selection as PreviewSelection | null;
          const rect = element.getBoundingClientRect();
          selectionHandler.current?.(selected ? { ...selected, rect: {
            left: selected.rect.left + rect.left, bottom: selected.rect.bottom + rect.top, width: selected.rect.width,
          } } : null);
        }
      },
    });
    connection.current = session;
    return () => session.close();
  }, [content]);

  useEffect(() => {
    if (status === 'ready') connection.current?.send({ type: 'review:marks', highlights });
  }, [status, highlights]);

  useEffect(() => {
    if (status === 'ready') connection.current?.send({ type: 'review:clear' });
  }, [status, clearSelection]);

  useEffect(() => {
    if (status !== 'ready') return;
    const root = document.documentElement;
    const sendTheme = () => {
      const styles = getComputedStyle(root);
      const theme = Object.fromEntries(['--fg', '--border', '--surface-sunken', '--code-bg', '--code-fg'].map(token => [token, styles.getPropertyValue(token)]));
      connection.current?.send({ type: 'review:theme', theme: { ...theme, '--font-size': `${size}px` } });
    };
    const observer = new MutationObserver(sendTheme);
    observer.observe(root, { attributes: true, attributeFilter: ['data-theme'] });
    sendTheme();
    return () => observer.disconnect();
  }, [status, size]);

  if (status === 'failed') return <div><p role="alert">复习稿预览暂时无法显示。</p><button type="button" onClick={onRetry}>重新加载预览</button></div>;
  return <div className={s.document}>
    {status === 'loading' && <p role="status">正在加载复习稿…</p>}
    <iframe ref={frame} title="复习稿预览" />
  </div>;
}
