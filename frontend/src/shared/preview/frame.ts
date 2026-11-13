import './review.css';
import createDOMPurify from 'dompurify';
import { selectionEndpointRect } from '../lib/selectionGeometry';
import { applyHighlights, selectionToTextAnchor, type SavedHighlight } from '../lib/summaryHighlights';
import { MAX_REVIEW_HTML, PREVIEW_HOST_PATTERN, type PreviewSelection } from './protocol';
import { buildInteractiveDocument } from '../lib/interactiveMarkdown';

const root = document.querySelector<HTMLElement>('#content')!;
const purifier = createDOMPurify(window);
const themeTokens = ['--fg', '--border', '--surface-sunken', '--code-bg', '--code-fg', '--font-size'];

function review(content: string, highlights: SavedHighlight[], theme: Record<string, string>, send: (data: object) => void) {
  if (content.length > MAX_REVIEW_HTML) throw new Error('Review exceeds preview budget');
  root.dataset.reviewDocument = '';
  const fragment = purifier.sanitize(content, {
    ALLOWED_TAGS: ['article', 'section', 'header', 'footer', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'p', 'strong', 'em', 'b', 'i', 'small', 'sub', 'sup', 'blockquote', 'ul', 'ol', 'li', 'dl', 'dt', 'dd', 'table', 'caption', 'thead', 'tbody', 'tfoot', 'tr', 'th', 'td', 'pre', 'code', 'br', 'hr'],
    ALLOWED_ATTR: ['colspan', 'rowspan', 'scope', 'start', 'lang'],
    ALLOW_ARIA_ATTR: false,
    ALLOW_DATA_ATTR: false,
    RETURN_DOM_FRAGMENT: true,
  });
  const walker = document.createTreeWalker(fragment, NodeFilter.SHOW_ELEMENT);
  let elements = 0;
  while (walker.nextNode()) {
    if (++elements > 5_000) throw new Error('Review exceeds layout budget');
    const element = walker.currentNode as HTMLElement;
    for (const name of ['colspan', 'rowspan']) {
      const span = element.getAttribute(name);
      if (span !== null && (!/^\d{1,2}$/.test(span) || Number(span) < 1)) element.removeAttribute(name);
    }
  }
  root.replaceChildren(fragment);
  if (!root.textContent?.trim()) throw new Error('Empty review preview');
  const setTheme = (values: Record<string, string>) => {
    for (const token of themeTokens) document.documentElement.style.setProperty(token, values[token] ?? '');
  };
  setTheme(theme);
  applyHighlights(root, highlights);
  const reportSize = () => send({ type: 'preview:size', height: root.scrollHeight });
  new ResizeObserver(reportSize).observe(root);
  reportSize();
  send({ type: 'preview:rendered' });
  send({ type: 'preview:alive' });
  window.setInterval(() => send({ type: 'preview:alive' }), 1000);

  const capture = (event: PointerEvent | KeyboardEvent) => {
    const selection = window.getSelection();
    let result: PreviewSelection | null = null;
    if (selection && !selection.isCollapsed && selection.rangeCount) {
      const range = selection.getRangeAt(0);
      const anchor = selectionToTextAnchor(root, range);
      const pointer = event instanceof PointerEvent ? { x: event.clientX, y: event.clientY } : undefined;
      const rect = anchor && selectionEndpointRect(range, pointer);
      if (anchor && rect && anchor.text.length <= 4_000) {
        const text = root.textContent ?? '';
        result = { ...anchor, rect, prefix: text.slice(Math.max(0, anchor.start - 40), anchor.start), suffix: text.slice(anchor.end, anchor.end + 40) };
      }
    }
    send({ type: 'preview:selection', selection: result });
  };
  root.addEventListener('pointerup', capture);
  root.addEventListener('keyup', capture);
  document.addEventListener('scroll', () => send({ type: 'preview:selection', selection: null }));
  return (message: { type: string; highlights: SavedHighlight[]; theme: Record<string, string> }) => {
    if (message.type === 'review:clear') window.getSelection()?.removeAllRanges();
    if (message.type === 'review:marks') applyHighlights(root, message.highlights);
    if (message.type === 'review:theme') setTheme(message.theme);
  };
}

async function interactive(code: string, send: (data: object) => void) {
  const runtime = /\b(?:JXG|LLLMath)\b/.test(code) ? (await import('../lib/mathRuntime')).mathRuntime : undefined;
  const source = buildInteractiveDocument(code, crypto.randomUUID().replace(/-/g, ''), runtime);
  const frame = document.createElement('iframe');
  frame.title = '图示内容';
  frame.setAttribute('sandbox', 'allow-scripts');
  frame.referrerPolicy = 'no-referrer';
  frame.setAttribute('allow', "camera 'none'; microphone 'none'; geolocation 'none'; clipboard-read 'none'; clipboard-write 'none'");
  frame.style.cssText = 'display:block;width:100%;height:100vh;border:0';
  const url = URL.createObjectURL(new Blob([source], { type: 'text/html' }));
  window.addEventListener('pagehide', () => URL.revokeObjectURL(url), { once: true });
  window.addEventListener('message', event => {
    if (event.source !== frame.contentWindow || event.origin !== 'null') return;
    const data = event.data;
    if (data?.type === 'preview:alive') send({ type: 'preview:alive' });
    if (data?.type === 'lll-gui-size' && Number.isFinite(data.height) && data.height >= 240 && data.height <= 900) {
      send({ type: 'preview:size', height: data.height });
    }
  });
  frame.src = url;
  root.replaceChildren(frame);
}

const parentOrigin = new URLSearchParams(location.search).get('parent');
if (PREVIEW_HOST_PATTERN.test(location.hostname) && parent !== window && parentOrigin && parentOrigin !== location.origin) {
  let update: ReturnType<typeof review> | undefined;
  let started = false;
  const send = (data: object) => parent.postMessage(data, parentOrigin);
  window.addEventListener('message', event => {
    if (event.source !== parent || event.origin !== parentOrigin || !event.data) return;
    try {
      const message = event.data;
      if (started) {
        update?.(message);
      } else if (message.type === 'review:init' || message.type === 'interactive:init') {
        started = true;
        if (message.type === 'review:init') update = review(message.content, message.highlights, message.theme, send);
        else void interactive(message.code, send).catch(() => send({ type: 'preview:failed' }));
      }
    } catch {
      send({ type: 'preview:failed' });
    }
  });
  send({ type: 'preview:ready' });
}
