import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useLocation, useNavigate } from 'react-router-dom';
import { useProjects } from '@/features/projects/api/projects';
import { useAskAiSettings } from '@/features/settings';
import { useOnboardingStore } from '@/shared/store/slices/onboarding';
import s from './ScreenGuide.module.css';

const steps = [
 { title: '欢迎，先认识你的学习工作台', target: 'welcome', route: '/', body: '接下来会带你操作真实界面。你随时可以退出，之后从主页继续；离线体验示例也会保留。', next: '开始配置' },
 { title: '连接你的教师模型', target: 'model-connections', route: '/settings', body: '点击高亮区域的“添加连接”，选择提供方，填写你自己的模型与 Key，检测并保存。再确认教师使用的连接。向导不会读取或填写密钥。', next: '已保存连接，继续' },
 { title: '告诉教师，你希望怎样学习', target: 'preferences', route: '/preferences', body: '在下方填写你喜欢的讲解方式，例如“先讲直觉，多给例子”，再点击“保存偏好”。这是选填项，可以直接继续。', next: '去创建学习项目' },
 { title: '创建第一个系统学习项目', target: 'create-project', route: '/', body: '点击“新建项目”，选择“系统学习”，输入一个具体主题，然后点击“创建项目”。也可以从向导下方选择已有的系统学习项目继续。', next: '等待你进入学习项目' },
 { title: '向教师提出第一个问题', target: 'teacher-composer', body: '在高亮输入框写下问题并发送。左侧加号可上传附件，右侧可以选择模型或使用麦克风。教师回答期间，后续问题会进入等待队列。', next: '去看笔记与复习' },
 { title: '把对话变成自己的知识', target: 'notes-tabs', body: '这里可以查看复习稿、练习、笔记和“讲给教师听”。点击生成入口后，由你选择模型并确认；自动笔记会按设置积累。试着切换上方入口，再继续。', next: '去管理附件' },
 { title: '把学习材料放在同一个地方', target: 'source-upload', body: '这里管理上传的文档及解析进度。教师可按需查阅，正文里的引用可以追溯原文。图片和音频也可从对话输入框左侧上传；此步可以先跳过上传。', next: '了解学科地图' },
 { title: '从一个主题，走向一整门学科', target: 'create-project', route: '/', body: '想先梳理一个领域时，在“新建项目”选择“学科地图”；确认地图中的主题后，再进入系统学习。你已走完主要流程，回到项目继续学习吧。', next: '完成向导，开始学习' },
] as const;
interface Box { left: number; top: number; right: number; bottom: number }
interface Geometry { box: Box | null; width: number; height: number; modal: HTMLElement | null; answered: boolean }

export function ScreenGuide() {
 const tour = useOnboardingStore();
 const navigate = useNavigate(), location = useLocation();
 const projects = useProjects();
 const models = useAskAiSettings();
 const card = useRef<HTMLElement>(null);
 const creationEntry = useRef(false);
 const [geometry, setGeometry] = useState<Geometry>({box:null,width:window.innerWidth,height:window.innerHeight,modal:null,answered:false});
 const step = steps[tour.step];
 const projectPath = `/project/${encodeURIComponent(tour.slug)}`;
 const pause = () => { tour.pause(); requestAnimationFrame(()=>document.querySelector<HTMLButtonElement>('[data-tour="welcome"] button')?.focus()); };

 useEffect(() => {
  if (!tour.active) return;
  const route = 'route' in step ? step.route : `${projectPath}/${tour.step === 4 ? 'teacher' : tour.step === 5 ? 'assets' : 'sources'}`;
  navigate(route);
  // Navigation occurs on a guide step change; normal user navigation remains available.
 }, [tour.active, tour.step, tour.slug, navigate, step, projectPath]);

 useEffect(() => {
  if (!tour.active || tour.step !== 3) { creationEntry.current = false; return; }
  if (location.pathname === '/') { creationEntry.current = true; return; }
  if (!creationEntry.current) return;
  const slug = /^\/project\/([^/]+)/.exec(location.pathname)?.[1];
  const project = projects.data?.projects.find(item => item.slug === decodeURIComponent(slug ?? '') && item.projectType === 'system-learning');
  if (project) tour.selectProject(project.slug);
 }, [location.pathname, projects.data, tour.active, tour.step, tour.selectProject]);

 useEffect(() => {
  if (!tour.active) return;
  let frame = 0, target: Element | null = null;
  const measure = () => {
   frame = 0;
   const modal = document.querySelector<HTMLElement>('[role="dialog"][data-state="open"]');
   const node = document.querySelector(`[data-tour="${step.target}"]`);
   if(node && node !== target) { target = node; node.scrollIntoView?.({block:'nearest',inline:'nearest'}); }
   const rect = node?.getBoundingClientRect();
   const width = window.innerWidth, height = window.innerHeight;
   const box = rect && rect.width > 0 && rect.height > 0 && rect.bottom > 6 && rect.top < height-6 && rect.right > 6 && rect.left < width-6 ? {left:Math.max(6,rect.left-6),top:Math.max(6,rect.top-6),right:Math.min(width-6,rect.right+6),bottom:Math.min(height-6,rect.bottom+6)} : null;
   const answered = Boolean(document.querySelector('[data-tour="learner-message"]'));
   setGeometry(old => old.width===width && old.height===height && old.modal===modal && old.answered===answered && JSON.stringify(old.box)===JSON.stringify(box) ? old : {box,width,height,modal,answered});
  };
  const schedule = () => { if(!frame) frame=requestAnimationFrame(measure); };
  const observer = new MutationObserver(schedule);
  observer.observe(document.body,{subtree:true,childList:true,attributes:true,attributeFilter:['data-state','hidden']});
  const resize = new ResizeObserver(schedule); resize.observe(document.documentElement);
  window.addEventListener('resize',schedule); window.addEventListener('scroll',schedule,true);
  const keyboard = (event: KeyboardEvent) => { if(event.key==='Escape' && !document.querySelector('[role="dialog"][data-state="open"]')) { event.preventDefault(); useOnboardingStore.getState().pause(); } };
  document.addEventListener('keydown',keyboard);
  measure();
  return () => { observer.disconnect(); resize.disconnect(); cancelAnimationFrame(frame); window.removeEventListener('resize',schedule); window.removeEventListener('scroll',schedule,true); document.removeEventListener('keydown',keyboard); };
 }, [tour.active,tour.step,step]);

 useEffect(() => { if(tour.active && !geometry.modal) card.current?.focus({preventScroll:true}); }, [tour.active,tour.step,geometry.modal]);
 if (!tour.active) return null;
 const configured = models.data?.providers?.some(p=>p.id && p.baseURL && p.model);
 const blocked = tour.step===3 || (tour.step===1 && !configured) || (tour.step===4 && !geometry.answered);
 const next = () => {
  if(tour.step===7) { tour.finish(); if(tour.slug) navigate(`${projectPath}/teacher`); }
  else tour.move(tour.step+1);
 };
 if(geometry.modal) return createPortal(<p className={s.modalHint} role="status">屏幕向导 · 第 {tour.step+1} / 8 步<br/>{tour.step===3 ? '选择“系统学习”，填写主题并创建。' : tour.step===1 ? '填写连接、检测并保存；向导将在关闭弹窗后继续。' : '按需完成此窗口的操作，关闭后继续向导。'}</p>,geometry.modal);
 const {box,width,height} = geometry;
 const panelWidth = Math.min(344,width-24), panelHeight = card.current?.offsetHeight ?? 238;
 const left = box ? Math.min(width-panelWidth-12,Math.max(12,(box.left+box.right-panelWidth)/2)) : (width-panelWidth)/2;
 const top = box ? (box.bottom+12+panelHeight < height-12 ? box.bottom+12 : (box.top-panelHeight-12>=12 ? box.top-panelHeight-12 : Math.max(12,height-panelHeight-12))) : Math.max(12,(height-panelHeight)/2);
 return createPortal(<div className={s.root}>
  {box ? <>
   <div className={s.shade} style={{left:0,top:0,right:0,height:box.top}}/>
   <div className={s.shade} style={{left:0,top:box.top,width:box.left,height:box.bottom-box.top}}/>
   <div className={s.shade} style={{left:box.right,top:box.top,right:0,height:box.bottom-box.top}}/>
   <div className={s.shade} style={{left:0,top:box.bottom,right:0,bottom:0}}/>
   <div className={s.highlight} style={{left:box.left,top:box.top,width:box.right-box.left,height:box.bottom-box.top}}/>
  </> : <div className={s.shade} style={{inset:0}}/>}
  <aside ref={card} tabIndex={-1} role="region" aria-label="屏幕向导" className={s.card} style={{left,top,width:panelWidth,maxHeight:height-24}}>
   <div className={s.progress}><span>屏幕向导</span><span>{tour.step+1} / 8</span><button type="button" onClick={pause} aria-label="退出屏幕向导">×</button></div>
   <div className={s.track} aria-hidden="true">{steps.map((item,i)=><i key={item.title} data-complete={i<=tour.step || undefined}/>)}</div>
   <h2>{step.title}</h2><p>{step.body}</p>
   {!box && <p role="status" className={s.wait}>正在等待此页的入口。若你离开了当前步骤，可点击“重新定位”。</p>}
   {tour.step===3 && projects.data?.projects.some(p=>p.projectType==='system-learning') && <label className={s.projectPicker}>使用已有项目<select aria-label="选择已有学习项目" value="" onChange={event=>{if(event.target.value)tour.selectProject(event.target.value);}}><option value="">选择一个学习项目…</option>{projects.data.projects.filter(p=>p.projectType==='system-learning').map(p=><option key={p.slug} value={p.slug}>{p.title}</option>)}</select></label>}
   <div className={s.actions}>
    <button type="button" onClick={()=>tour.move(tour.step-1)} disabled={tour.step===0}>上一步</button>
    <button type="button" className={s.primary} disabled={blocked} onClick={next}>{step.next}</button>
   </div>
   <div className={s.secondary}><button type="button" onClick={pause}>稍后继续</button>{tour.step===1 && !configured && <button type="button" onClick={()=>tour.move(2)}>稍后配置</button>}{tour.step===4 && !geometry.answered && <button type="button" onClick={()=>tour.move(5)}>先了解其他功能</button>}{!box && <button type="button" onClick={()=>{ const target=document.querySelector(`[data-tour="${step.target}"]`); if(target) target.scrollIntoView({block:'center',inline:'nearest'}); else navigate('route' in step ? step.route : `${projectPath}/${tour.step===4?'teacher':tour.step===5?'assets':'sources'}`); }}>重新定位</button>}</div>
  </aside>
 </div>,document.body);
}
