import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react';

import { useMarkdown } from '@/shared/hooks/useMarkdown';
import { mountMermaidBlocks } from '@/shared/lib/mermaidRenderer';
import { mountInteractiveBlocks } from '@/shared/lib/interactiveMarkdown';
import { mountDiagramControls } from '@/shared/lib/diagramControls';
import { mountLearningFigures } from '@/shared/lib/learningVisual';
import s from './MarkdownView.module.css';

interface MarkdownViewProps {
  source: string;
  className?: string;
  /**
   * Streaming Markdown is intentionally rendered from a slower snapshot. It
   * keeps token feedback responsive without re-running marked, KaTeX and
   * DOMPurify for every provider frame. Mermaid is mounted only after the
   * message is complete because an unfinished diagram is not valid input.
   */
  streaming?: boolean;
}

const STREAM_MARKDOWN_INTERVAL_MS = 120;

export const MarkdownView = forwardRef<HTMLDivElement, MarkdownViewProps>(function MarkdownView({ source, className, streaming = false }, forwardedRef) {
  const bufferedSource = useBufferedStreamValue(source, streaming);
  const { html, mermaid, interactive } = useMarkdown(bufferedSource, streaming);
  const hostRef = useRef<HTMLDivElement>(null);
  useImperativeHandle(forwardedRef, () => hostRef.current as HTMLDivElement, []);

  useEffect(() => {
    if (streaming || !hostRef.current || mermaid.length === 0) return;
    return mountMermaidBlocks(hostRef.current, mermaid, 'markdown-view');
  }, [streaming, html, mermaid]);

  useEffect(() => {
    if (streaming || !hostRef.current) return;
    const stopGUI = mountInteractiveBlocks(hostRef.current, interactive);
    const stopDiagrams = mountDiagramControls(hostRef.current);
    return () => { stopGUI(); stopDiagrams(); };
  }, [streaming, html, interactive]);

  useEffect(() => {
    if (streaming || !hostRef.current) return;
    return mountLearningFigures(hostRef.current);
  }, [html, streaming]);

  return (
    <div ref={hostRef} className={`${s.body} ${className ?? ''}`} dangerouslySetInnerHTML={{ __html: html }} />
  );
});

function useBufferedStreamValue(value: string, streaming: boolean) {
  const [buffered, setBuffered] = useState(value);
  const latest = useRef(value);
  const timer = useRef<number | null>(null);

  latest.current = value;
  useEffect(() => {
    if (!streaming) {
      if (timer.current !== null) window.clearTimeout(timer.current);
      timer.current = null;
      setBuffered(value);
      return;
    }
    if (timer.current !== null) return;
    timer.current = window.setTimeout(() => {
      timer.current = null;
      setBuffered(latest.current);
    }, STREAM_MARKDOWN_INTERVAL_MS);
  }, [streaming, value]);

  useEffect(() => () => {
    if (timer.current !== null) window.clearTimeout(timer.current);
  }, []);

  return streaming ? buffered : value;
}
