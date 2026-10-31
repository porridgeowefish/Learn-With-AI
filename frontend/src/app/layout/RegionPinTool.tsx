import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import { createPortal } from 'react-dom';
import { CropIcon } from '@radix-ui/react-icons';
import { cropFrame, selectionRect, type PixelViewport, type RegionRect } from '@/shared/lib/regionCapture';
import s from './RegionPinTool.module.css';

interface WindowCapture { dataUrl: string; viewport: PixelViewport }
type DesktopWindow = Window & { lllCaptureWindow?: () => Promise<WindowCapture> };
interface Pin { id: number; url: string; left: number; top: number; width: number; minimized: boolean }

const MIN_SELECTION = 12;
const nextPaint = () => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
const within = (value: number, low: number, high: number) => Math.max(low, Math.min(high, value));
function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '截图失败，请重试。';
}

export function RegionPinTool() {
  const [selecting, setSelecting] = useState(false);
  const [busy, setBusy] = useState(false);
  const [selection, setSelection] = useState<RegionRect | null>(null);
  const [selectionHint, setSelectionHint] = useState('');
  const [pins, setPins] = useState<Pin[]>([]);
  const [notice, setNotice] = useState('');
  const startPoint = useRef<{ x: number; y: number } | null>(null);
  const pinsRef = useRef<Pin[]>([]);
  const nextId = useRef(1);

  useEffect(() => { pinsRef.current = pins; }, [pins]);
  useEffect(() => {
    const clampPins = () => setPins(current => current.map(pin => ({ ...pin, left: within(pin.left, 8, Math.max(8, window.innerWidth - pin.width - 8)), top: within(pin.top, 8, Math.max(8, window.innerHeight - 48)) })));
    window.addEventListener('resize', clampPins);
    return () => window.removeEventListener('resize', clampPins);
  }, []);
  useEffect(() => {
    return () => {
      pinsRef.current.forEach(pin => URL.revokeObjectURL(pin.url));
    };
  }, []);

  const cancel = () => {
    setSelecting(false);
    setSelection(null);
    setSelectionHint('');
    startPoint.current = null;
  };
  useEffect(() => {
    if (!selecting) return;
    const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape') cancel(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [selecting]);

  const begin = () => {
    if (busy || selecting) return;
    setNotice('');
    setSelecting(true);
  };

  const capture = async (rect: RegionRect) => {
    setSelecting(false);
    setSelection(null);
    setBusy(true);
    try {
      await nextPaint();
      const viewport = { width: window.innerWidth, height: window.innerHeight };
      const native = (window as DesktopWindow).lllCaptureWindow;
      if (!native) throw new Error('桌面截图能力不可用，请重启应用。');
      const shot = await native();
      const image = new Image();
      image.src = shot.dataUrl;
      await image.decode();
      const blob = await cropFrame(image, rect, viewport, shot.viewport);
      const url = URL.createObjectURL(blob);
      const width = Math.min(380, Math.max(180, rect.width + 20), window.innerWidth - 16);
      const left = within(rect.x + rect.width + 12, 8, Math.max(8, window.innerWidth - width - 8));
      const height = 36 + Math.min(rect.height * width / rect.width, window.innerHeight * .55, 440);
      const top = within(rect.y, 8, Math.max(8, window.innerHeight - height - 8));
      setPins(current => [...current, { id: nextId.current++, url, left, top, width, minimized: false }]);
    } catch (error) {
      setNotice(errorMessage(error));
    } finally {
      setBusy(false);
    }
  };

  const pointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    startPoint.current = { x: event.clientX, y: event.clientY };
    setSelection(null);
    setSelectionHint('');
  };
  const pointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!startPoint.current) return;
    setSelection(selectionRect(startPoint.current, { x: event.clientX, y: event.clientY }, { width: window.innerWidth, height: window.innerHeight }));
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
    <div className={s.tool} style={{ visibility: busy ? 'hidden' : undefined }}>
      <button type="button" onClick={begin} disabled={busy || selecting} aria-label="框选画面" title="框选画面并固定为临时卡片"><CropIcon aria-hidden="true" /></button>
      {notice && <div className={s.notice} role="alert">{notice}<button type="button" onClick={() => setNotice('')} aria-label="关闭提示">×</button></div>}
    </div>
    {createPortal(<>
    {!selecting && !busy && pins.map(pin => <PinWindow key={pin.id} pin={pin} onClose={() => closePin(pin.id)} onChange={change => updatePin(pin.id, change)} />)}
    {selecting && <div className={s.overlay} onPointerDown={pointerDown} onPointerMove={pointerMove} onPointerUp={pointerUp} onPointerCancel={cancel} role="application" aria-label="框选应用画面">
      <div className={s.instructions}>{selectionHint || '拖动框选要保留的画面 · Esc 取消'} <button type="button" onPointerDown={event => event.stopPropagation()} onClick={cancel}>取消</button></div>
      {selection && <div className={s.selection} style={{ left: selection.x, top: selection.y, width: selection.width, height: selection.height }} />}
    </div>}
    </>, document.body)}
  </>;
}

function PinWindow({ pin, onClose, onChange }: { pin: Pin; onClose: () => void; onChange: (change: Partial<Pin>) => void }) {
  const drag = useRef<{ x: number; y: number; left: number; top: number } | null>(null);
  const move = (event: ReactPointerEvent<HTMLElement>) => {
    if (!drag.current) return;
    const width = Math.min(pin.width, window.innerWidth - 16);
    onChange({ left: Math.max(8, Math.min(window.innerWidth - width - 8, drag.current.left + event.clientX - drag.current.x)), top: Math.max(8, Math.min(window.innerHeight - 48, drag.current.top + event.clientY - drag.current.y)) });
  };
  return <aside className={s.pin} style={{ left: pin.left, top: pin.top, width: pin.width }} aria-label="临时框选卡片">
    <header className={s.pinHeader} onPointerDown={event => {
      if ((event.target as HTMLElement).closest('button')) return;
      event.currentTarget.setPointerCapture(event.pointerId);
      drag.current = { x: event.clientX, y: event.clientY, left: pin.left, top: pin.top };
    }} onPointerMove={move} onPointerUp={() => { drag.current = null; }} onPointerCancel={() => { drag.current = null; }}>
      <span>框选画面</span>
      <div><button type="button" onClick={() => onChange({ minimized: !pin.minimized })} aria-label={pin.minimized ? '恢复卡片' : '最小化卡片'} title={pin.minimized ? '恢复' : '最小化'}>{pin.minimized ? '□' : '−'}</button><button type="button" onClick={onClose} aria-label="关闭卡片" title="关闭">×</button></div>
    </header>
    {!pin.minimized && <img className={s.image} src={pin.url} alt="框选的应用画面" draggable={false} />}
  </aside>;
}
