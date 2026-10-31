import { useEffect, useRef, useState } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { useAppearance } from '@/features/settings/appearance';
import { useSSE } from '@/app/hooks/useSSE';
import { useActivityRefresh } from '@/app/hooks/useActivityRefresh';
import { TreasureNotifications } from '@/features/treasures';
import { ScreenGuide } from './ScreenGuide';
import { RegionPinTool } from './RegionPinTool';
import { Sidebar } from './Sidebar';
import { useUiStore } from '@/shared/store';
import s from './AppShell.module.css';
export function AppShell() {
 useSSE(); useActivityRefresh(); useAppearance();
 const [open, setOpen] = useState(false), location = useLocation();
 const sidebarCollapsed = useUiStore(state => state.sidebarCollapsed);
 const sidebarWidth = useUiStore(state => state.sidebarWidth);
 const setSidebarCollapsed = useUiStore(state => state.setSidebarCollapsed);
 const setSidebarWidth = useUiStore(state => state.setSidebarWidth);
 const resizing = useRef(false);
 const trigger = useRef<HTMLButtonElement>(null), panel = useRef<HTMLDivElement>(null);
 const close = () => {setOpen(false); trigger.current?.focus();};
 useEffect(() => {setOpen(false);}, [location.pathname]);
 useEffect(() => {if (open) panel.current?.querySelector<HTMLButtonElement>('button')?.focus();}, [open]);
 return <div className={s.root}><button ref={trigger} className={s.menuButton} onClick={() => setOpen(true)} aria-label="打开侧栏" aria-expanded={open}>☰</button><div className={s.shell}>
  {open && <button className={s.backdrop} aria-label="关闭侧栏" onClick={close}/>}
  <button type="button" className={s.collapseButton} style={{ left: sidebarCollapsed ? 8 : `min(${sidebarWidth - 14}px, calc(100vw - 374px))` }} onClick={() => setSidebarCollapsed(!sidebarCollapsed)} aria-label={sidebarCollapsed ? '展开侧栏' : '收起侧栏'} aria-expanded={!sidebarCollapsed}>{sidebarCollapsed ? '›' : '‹'}</button>
  <div ref={panel} className={s.sidePanel} data-open={open} data-collapsed={sidebarCollapsed} style={{ width: sidebarWidth }} onKeyDown={event => {
   if (!open) return;
   if (event.key === 'Escape') {event.preventDefault();close();}
   if (event.key === 'Tab') {const nodes = [...event.currentTarget.querySelectorAll<HTMLElement>('button:not(:disabled),a[href],input,select')].filter(el=>el.offsetParent!==null);const first=nodes[0],last=nodes[nodes.length-1];if(event.shiftKey && document.activeElement===first){event.preventDefault();last?.focus();}else if(!event.shiftKey && document.activeElement===last){event.preventDefault();first?.focus();}}
  }}><button className={s.closeMenu} onClick={close} aria-label="关闭导航">×</button><Sidebar/><div className={s.resizeHandle} role="separator" aria-label="拖拽调整侧栏宽度" aria-orientation="vertical" aria-valuenow={sidebarWidth} tabIndex={0}
    onKeyDown={event => { if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { event.preventDefault(); setSidebarWidth(sidebarWidth + (event.key === 'ArrowRight' ? 16 : -16)); } }}
    onPointerDown={event => { resizing.current = true; event.currentTarget.setPointerCapture(event.pointerId); event.preventDefault(); }}
    onPointerMove={event => { if (resizing.current) setSidebarWidth(Math.min(event.clientX, window.innerWidth - 360)); }}
    onPointerUp={() => { resizing.current = false; }} onLostPointerCapture={() => { resizing.current = false; }} /></div>
  <main className={s.main}>{typeof (window as Window & { lllCaptureWindow?: unknown }).lllCaptureWindow === 'function' && <div className={s.topTools}><RegionPinTool/></div>}<Outlet/></main>
 </div><ScreenGuide/><TreasureNotifications/></div>;
}
