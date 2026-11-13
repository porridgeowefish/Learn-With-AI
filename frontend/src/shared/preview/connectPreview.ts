import { PREVIEW_HEARTBEAT_TIMEOUT, PREVIEW_START_TIMEOUT } from './protocol';

type PreviewOptions = {
  init: object;
  onFailure: () => void;
  onMessage?: (message: Record<string, unknown>) => void;
};

export function connectPreview(frame: HTMLIFrameElement, { init, onFailure, onMessage }: PreviewOptions) {
  const url = new URL('/html-preview.html', window.location.href);
  url.protocol = 'http:';
  // A fresh site prevents Chromium from reusing a renderer that is still hung.
  url.hostname = `lll-preview-${crypto.randomUUID().replace(/-/g, '')}.localhost`;
  url.searchParams.set('parent', window.location.origin);
  let active = true;
  let started = false;
  let lastAlive = performance.now();
  const send = (message: object) => {
    if (active) frame.contentWindow?.postMessage(message, url.origin);
  };
  const receive = (event: MessageEvent) => {
    if (event.source !== frame.contentWindow || event.origin !== url.origin || !event.data) return;
    if (event.data.type === 'preview:ready' && !started) send(init);
    if (event.data.type === 'preview:alive') {
      started = true;
      lastAlive = performance.now();
    }
    if (event.data.type === 'preview:failed') {
      close();
      onFailure();
      return;
    }
    onMessage?.(event.data);
  };
  const checkHeartbeat = () => {
    if (document.hidden) return;
    if (performance.now() - lastAlive <= (started ? PREVIEW_HEARTBEAT_TIMEOUT : PREVIEW_START_TIMEOUT)) return;
    close();
    onFailure();
  };
  const close = () => {
    active = false;
    window.removeEventListener('message', receive);
    document.removeEventListener('visibilitychange', checkHeartbeat);
    window.clearInterval(watchdog);
  };
  const watchdog = window.setInterval(checkHeartbeat, 500);
  window.addEventListener('message', receive);
  document.addEventListener('visibilitychange', checkHeartbeat);
  frame.setAttribute('sandbox', 'allow-scripts allow-same-origin');
  frame.referrerPolicy = 'no-referrer';
  frame.src = url.href;
  return { send, close };
}
