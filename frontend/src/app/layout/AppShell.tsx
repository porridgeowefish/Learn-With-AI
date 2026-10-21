import { useEffect, useRef, useState } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { useAppearance } from '@/features/settings/appearance';
import { useSSE } from '@/app/hooks/useSSE';
import { useActivityRefresh } from '@/app/hooks/useActivityRefresh';
import { ScreenGuide } from './ScreenGuide';
import { Sidebar } from './Sidebar';
import s from './AppShell.module.css';
export function AppShell() {
 useSSE(); useActivityRefresh(); useAppearance();
 const [open, setOpen] = useState(false), location = useLocation();
 const trigger = useRef<HTMLButtonElement>(null), panel = useRef<HTMLDivElement>(null);
 const close = () => {setOpen(false); trigger.current?.focus();};
 useEffect(() => {setOpen(false);}, [location.pathname]);
 useEffect(() => {if (open) panel.current?.querySelector<HTMLButtonElement>('button')?.focus();}, [open]);
 return <div className={s.root}><button ref={trigger} className={s.menuButton} onClick={() => setOpen(true)} aria-label="打开侧栏" aria-expanded={open}>☰</button><div className={s.shell}>
  {open && <button className={s.backdrop} aria-label="关闭侧栏" onClick={close}/>}
  <div ref={panel} className={s.sidePanel} data-open={open} onKeyDown={event => {
   if (!open) return;
   if (event.key === 'Escape') {event.preventDefault();close();}
   if (event.key === 'Tab') {const nodes = [...event.currentTarget.querySelectorAll<HTMLElement>('button:not(:disabled),a[href],input,select')].filter(el=>el.offsetParent!==null);const first=nodes[0],last=nodes[nodes.length-1];if(event.shiftKey && document.activeElement===first){event.preventDefault();last?.focus();}else if(!event.shiftKey && document.activeElement===last){event.preventDefault();first?.focus();}}
  }}><button className={s.closeMenu} onClick={close} aria-label="关闭导航">×</button><Sidebar/></div>
  <main className={s.main}><Outlet/></main>
 </div><ScreenGuide/></div>;
}
