import { useEffect, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import { createPortal } from 'react-dom';
import { CropIcon } from '@radix-ui/react-icons';
import { selectionRect, type RegionRect } from '@/shared/lib/regionCapture';
import s from './RegionPinTool.module.css';

interface CaptureRequest extends RegionRect { id: number; viewportWidth: number; viewportHeight: number }
interface CaptureResult { id: number; dataUrl?: string; width?: number; height?: number; error?: string }
type DesktopWindow = Window & { lllCaptureRegion?: (request: CaptureRequest) => Promise<void> };
interface Pin { id: number; url: string; left: number; top: number; width: number; height: number; minimized: boolean }

const MIN_SELECTION = 12;
const CAPTURE_EVENT = 'lll-region-capture-result';
const nextPaint = () => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
const within = (value: number, low: number, high: number) => Math.max(low, Math.min(high, value));
const errorMessage = (error: unknown) => error instanceof Error ? error.message : '截图失败，请重试。';

function requestCapture(request: CaptureRequest): Promise<Required<CaptureResult>> {
  const native = (window as DesktopWindow).lllCaptureRegion;
  if (!native) return Promise.reject(new Error('桌面截图能力不可用，请重启应用。'));
  return new Promise((resolve, reject) => {
    const cleanup = () => { window.removeEventListener(CAPTURE_EVENT, onResult); window.clearTimeout(timeout); };
    const onResult = (event: Event) => {
      const result = (event as CustomEvent<CaptureResult>).detail;
      if (result?.id !== request.id) return;
      cleanup();
      if (result.error) reject(new Error(result.error));
      else if (!result.dataUrl?.startsWith('data:image/png;base64,') || !result.width || !result.height) reject(new Error('截图数据无效，请重试。'));
      else resolve(result as Required<CaptureResult>);
    };
    const timeout = window.setTimeout(() => { cleanup(); reject(new Error('截图等待超时，请重试。')); }, 15000);
    window.addEventListener(CAPTURE_EVENT, onResult);
    native(request).catch(error => { cleanup(); reject(error); });
  });
}

export function RegionPinTool({ routeKey }: { routeKey: string }) {
  const [host, setHost] = useState<HTMLElement | null>(null);
  const [selecting, setSelecting] = useState(false);
  const [busy, setBusy] = useState(false);
  const [selection, setSelection] = useState<RegionRect | null>(null);
  const [selectionHint, setSelectionHint] = useState('');
  const [pins, setPins] = useState<Pin[]>([]);
  const [notice, setNotice] = useState('');
  const startPoint = useRef<{ x: number; y: number } | null>(null);
  const pinsRef = useRef<Pin[]>([]);
  const nextId = useRef(1);

  useLayoutEffect(() => {
    const syncHost = () => setHost(document.querySelector<HTMLElement>('[data-region-pin-slot]'));
    syncHost();
    // ProjectPage is lazy, so its toolbar may appear after AppShell's effect.
    const observer = new MutationObserver(syncHost);
    observer.observe(document.querySelector('main') ?? document.body, { childList: true, subtree: true });
    return () => observer.disconnect();
  }, [routeKey]);
  useEffect(() => { pinsRef.current = pins; }, [pins]);
  useEffect(() => {
    const clampPins = () => setPins(current => current.map(pin => {
      const width = Math.min(pin.width, Math.max(160, window.innerWidth - 16));
      const height = Math.min(pin.height, Math.max(120, window.innerHeight - 16));
      return { ...pin, width, height, left: within(pin.left, 8, Math.max(8, window.innerWidth - width - 8)), top: within(pin.top, 8, Math.max(8, window.innerHeight - (pin.minimized ? 36 : height) - 8)) };
    }));
    window.addEventListener('resize', clampPins);
    return () => window.removeEventListener('resize', clampPins);
  }, []);
  useEffect(() => () => { pinsRef.current.forEach(pin => URL.revokeObjectURL(pin.url)); }, []);

  const cancel = () => { setSelecting(false); setSelection(null); setSelectionHint(''); startPoint.current = null; };
  useEffect(() => { if (!host && selecting) cancel(); }, [host, selecting]);
  useEffect(() => {
    if (!selecting) return;
    const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape') cancel(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [selecting]);

  const begin = () => { if (!busy && !selecting) { setNotice(''); setSelecting(true); } };
  const capture = async (rect: RegionRect) => {
    setSelecting(false);
    setSelection(null);
    setBusy(true);
    try {
      await nextPaint();
      const shot = await requestCapture({ ...rect, id: nextId.current++, viewportWidth: window.innerWidth, viewportHeight: window.innerHeight });
      const blob = await (await fetch(shot.dataUrl)).blob();
      const url = URL.createObjectURL(blob);
      const width = Math.min(380, Math.max(180, rect.width + 20), window.innerWidth - 16);
      const height = Math.min(window.innerHeight - 16, 36 + Math.min(440, Math.max(90, shot.height * (width - 2) / shot.width)));
      const left = within(rect.x + rect.width + 12, 8, Math.max(8, window.innerWidth - width - 8));
      const top = within(rect.y, 8, Math.max(8, window.innerHeight - height - 8));
      setPins(current => [...current, { id: nextId.current++, url, left, top, width, height, minimized: false }]);
    } catch (error) { setNotice(errorMessage(error)); }
    finally { setBusy(false); }
  };

  const pointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    startPoint.current = { x: event.clientX, y: event.clientY };
    setSelection(null);
    setSelectionHint('');
  };
  const pointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (startPoint.current) setSelection(selectionRect(startPoint.current, { x: event.clientX, y: event.clientY }, { width: window.innerWidth, height: window.innerHeight }));
  };
  const pointerUp = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!startPoint.current) return;
    const rect = selectionRect(startPoint.current, { x: event.clientX, y: event.clientY }, { width: window.innerWidth, height: window.innerHeight });
    startPoint.current = null;
    if (rect.width < MIN_SELECTION || rect.height < MIN_SELECTION) { setSelection(null); setSelectionHint('框选区域太小，请重新拖动。'); return; }
    void capture(rect);
  };
  const closePin = (id: number) => setPins(current => {
    const pin = current.find(item => item.id === id);
    if (pin) URL.revokeObjectURL(pin.url);
    return current.filter(item => item.id !== id);
  });
  const updatePin = (id: number, change: Partial<Pin>) => setPins(current => current.map(item => item.id === id ? { ...item, ...change } : item));

  return <>
    {host && createPortal(<div className={s.tool} style={{ visibility: busy ? 'hidden' : undefined }}>
      <button type="button" onClick={begin} disabled={busy || selecting} aria-label="框选画面" title="框选画面并固定为临时卡片"><CropIcon aria-hidden="true" /></button>
      {notice && <div className={s.notice} role="alert">{notice}<button type="button" onClick={() => setNotice('')} aria-label="关闭提示">×</button></div>}
    </div>, host)}
    {createPortal(<>
      {host && !selecting && !busy && pins.map(pin => <PinWindow key={pin.id} pin={pin} onClose={() => closePin(pin.id)} onChange={change => updatePin(pin.id, change)} />)}
      {host && selecting && <div className={s.overlay} onPointerDown={pointerDown} onPointerMove={pointerMove} onPointerUp={pointerUp} onPointerCancel={cancel} role="application" aria-label="框选应用画面">
        <div className={s.instructions}>{selectionHint || '拖动框选要保留的画面 · Esc 取消'} <button type="button" onPointerDown={event => event.stopPropagation()} onClick={cancel}>取消</button></div>
        {selection && <div className={s.selection} style={{ left: selection.x, top: selection.y, width: selection.width, height: selection.height }} />}
      </div>}
    </>, document.body)}
  </>;
}

function PinWindow({ pin, onClose, onChange }: { pin: Pin; onClose: () => void; onChange: (change: Partial<Pin>) => void }) {
  const drag = useRef<{ x: number; y: number; left: number; top: number } | null>(null);
  const resize = useRef<{ x: number; y: number; width: number; height: number } | null>(null);
  const move = (event: ReactPointerEvent<HTMLElement>) => {
    if (!drag.current) return;
    onChange({ left: within(drag.current.left + event.clientX - drag.current.x, 8, Math.max(8, window.innerWidth - pin.width - 8)), top: within(drag.current.top + event.clientY - drag.current.y, 8, Math.max(8, window.innerHeight - (pin.minimized ? 36 : pin.height) - 8)) });
  };
  const resizeMove = (event: ReactPointerEvent<HTMLButtonElement>) => {
    if (!resize.current) return;
    onChange({ width: within(resize.current.width + event.clientX - resize.current.x, 160, Math.max(160, window.innerWidth - pin.left - 8)), height: within(resize.current.height + event.clientY - resize.current.y, 120, Math.max(120, window.innerHeight - pin.top - 8)) });
  };
  return <aside className={s.pin} style={{ left: pin.left, top: pin.top, width: pin.width, height: pin.minimized ? 36 : pin.height }} aria-label="临时框选卡片">
    <header className={s.pinHeader} onPointerDown={event => {
      if ((event.target as HTMLElement).closest('button')) return;
      event.currentTarget.setPointerCapture(event.pointerId);
      drag.current = { x: event.clientX, y: event.clientY, left: pin.left, top: pin.top };
    }} onPointerMove={move} onPointerUp={() => { drag.current = null; }} onPointerCancel={() => { drag.current = null; }}>
      <span>框选画面</span>
      <div><button type="button" onClick={() => onChange({ minimized: !pin.minimized })} aria-label={pin.minimized ? '恢复卡片' : '最小化卡片'} title={pin.minimized ? '恢复' : '最小化'}>{pin.minimized ? '□' : '−'}</button><button type="button" onClick={onClose} aria-label="关闭卡片" title="关闭">×</button></div>
    </header>
    {!pin.minimized && <div className={s.pinBody}><img className={s.image} src={pin.url} alt="框选的应用画面" draggable={false} /><button type="button" className={s.resizeHandle} aria-label="调整卡片大小" title="拖动调整卡片大小" onPointerDown={event => { event.stopPropagation(); event.currentTarget.setPointerCapture(event.pointerId); resize.current = { x: event.clientX, y: event.clientY, width: pin.width, height: pin.height }; }} onPointerMove={resizeMove} onPointerUp={() => { resize.current = null; }} onPointerCancel={() => { resize.current = null; }} onKeyDown={event => { if (event.key === 'ArrowRight' || event.key === 'ArrowLeft' || event.key === 'ArrowUp' || event.key === 'ArrowDown') { event.preventDefault(); onChange({ width: within(pin.width + (event.key === 'ArrowRight' ? 16 : event.key === 'ArrowLeft' ? -16 : 0), 160, Math.max(160, window.innerWidth - pin.left - 8)), height: within(pin.height + (event.key === 'ArrowDown' ? 16 : event.key === 'ArrowUp' ? -16 : 0), 120, Math.max(120, window.innerHeight - pin.top - 8)) }); } }}>↘</button></div>}
  </aside>;
}
