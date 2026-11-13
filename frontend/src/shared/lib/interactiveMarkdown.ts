import { connectPreview } from '../preview/connectPreview';

interface InteractiveBlock { id: string; code: string }
export interface InteractiveRuntime { source: string; css: string }

export function buildInteractiveDocument(code: string, nonce: string, runtime?: InteractiveRuntime): string {
  const doc = new DOMParser().parseFromString(code, 'text/html');
  doc.querySelectorAll('base,iframe,object,embed,form,link,meta[http-equiv]').forEach(node => node.remove());
  doc.querySelectorAll('script').forEach(node => { if (node.src) node.remove(); else { node.removeAttribute('type'); node.setAttribute('nonce', nonce); } });
  // Inline event handlers would bypass the nonce boundary. Models should bind
  // listeners from their explicitly nonced local script instead.
  doc.querySelectorAll('*').forEach(node => { for (const attr of Array.from(node.attributes)) {
    if (/^on/i.test(attr.name)) node.removeAttribute(attr.name);
    if ((attr.name === 'href' || attr.name === 'action') && !attr.value.startsWith('#')) node.removeAttribute(attr.name);
  } });
  const policy = `default-src 'none'; script-src 'nonce-${nonce}'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; frame-src 'none'; worker-src 'none'; object-src 'none'; form-action 'none'; base-uri 'none'`;
  const engine = runtime ? `<script nonce="${nonce}">${runtime.source.replace(/<\/script/gi, '<\\/script')}</script>` : '';
  const heartbeat = `<script nonce="${nonce}">(()=>{const send=parent.postMessage.bind(parent);setInterval(()=>send({type:'preview:alive'},'*'),1000);})();</script>`;
  return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${policy}"><meta name="referrer" content="no-referrer"><style>html{color-scheme:light}body{margin:0;padding:12px;font:14px/1.6 system-ui,sans-serif;overflow-wrap:anywhere}*{box-sizing:border-box}img,svg,canvas{max-width:100%}${runtime?.css ?? ''}</style>${heartbeat}${engine}${doc.head.innerHTML}</head><body>${doc.body.innerHTML}<script nonce="${nonce}">new ResizeObserver(()=>parent.postMessage({type:'lll-gui-size',height:Math.min(900,Math.max(240,document.body.scrollHeight+24))},'*')).observe(document.body);</script></body></html>`;
}

export function mountInteractiveBlocks(host: HTMLElement, blocks: InteractiveBlock[]): () => void {
  const cleanups: Array<() => void> = [];
  for (const block of blocks) {
    const target = host.querySelector<HTMLElement>(`[data-interactive-id="${block.id}"]`);
    if (!target) continue;
    const fullscreen = document.createElement('button'); fullscreen.type = 'button'; fullscreen.className = 'media-fullscreen-toggle';
    fullscreen.setAttribute('aria-label', '全屏图示'); fullscreen.setAttribute('title', '全屏图示');
    const setFullscreen = (enabled: boolean) => {
      target.classList.toggle('media-fullscreen', enabled);
      fullscreen.setAttribute('aria-pressed', String(enabled));
    };
    fullscreen.onclick = () => setFullscreen(!target.classList.contains('media-fullscreen'));
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') setFullscreen(false); };
    const start = () => {
      const iframe = document.createElement('iframe');
      iframe.title = '交互教学图示';
      iframe.setAttribute('allow', "camera 'none'; microphone 'none'; geolocation 'none'; clipboard-read 'none'; clipboard-write 'none'");
      iframe.style.height = '320px';
      window.addEventListener('keydown', escape);
      target.replaceChildren(fullscreen, iframe);
      const connection = connectPreview(iframe, {
        init: { type: 'interactive:init', code: block.code },
        onMessage(message) {
          if (message.type === 'preview:size' && typeof message.height === 'number') iframe.style.height = `${Math.ceil(message.height)}px`;
        },
        onFailure() {
          stop();
          const error = document.createElement('p');
          error.setAttribute('role', 'alert');
          error.textContent = '交互图示未响应。';
          const retry = document.createElement('button');
          retry.type = 'button';
          retry.textContent = '重新加载图示';
          retry.onclick = start;
          target.replaceChildren(error, retry);
        },
      });
      stop = () => {
        connection.close();
        window.removeEventListener('keydown', escape);
        setFullscreen(false);
        iframe.remove();
      };
    };
    let stop = () => {};
    start();
    cleanups.push(() => { stop(); target.replaceChildren(); });
  }
  return () => cleanups.forEach(fn => fn());
}
