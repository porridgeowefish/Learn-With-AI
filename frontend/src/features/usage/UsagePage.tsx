import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useProjectUsage, type ProjectUsage } from './api';
import { UsageHeatmap } from './UsageHeatmap';
import s from './UsagePage.module.css';
const total = (row: ProjectUsage) => row.inputTokens + row.outputTokens + row.noteInputTokens + row.noteOutputTokens;
const number = (value: number) => value.toLocaleString();
const reported = (value: number | undefined) => value === undefined ? '未报告' : `${number(value)} Token`;
function cacheTotal(rows: ProjectUsage[], key: 'cacheReadTokens' | 'cacheWriteTokens') {
 const known = rows.filter(row => row[key] !== undefined);
 return known.length ? known.reduce((sum, row) => sum + (row[key] ?? 0), 0) : undefined;
}
export function UsagePage() {
 const usage = useProjectUsage();
 const rows = useMemo(() => [...(usage.data?.projects ?? [])].sort((a,b) => total(b)-total(a)), [usage.data]);
 const [hovered, setHovered] = useState<string>(), [focused, setFocused] = useState<string>(), [selected, setSelected] = useState<string>();
 const current = rows.find(row => row.projectSlug === (hovered ?? focused ?? selected));
 const maximum = Math.max(1, ...rows.map(total));
 return <div className={s.page}>
  <header><span>USAGE</span><h1>Token 用量</h1><p>按项目统计教师对话与自动笔记的实际用量。悬浮、聚焦或选中柱子查看项目详情。</p></header>
  <UsageHeatmap />
  {!usage.isLoading && !usage.isError && <section className={s.cacheSummary} aria-label="服务商缓存用量">
   <div><span>缓存命中</span><strong>{reported(cacheTotal(rows, 'cacheReadTokens'))}</strong></div>
   <div><span>缓存写入</span><strong>{reported(cacheTotal(rows, 'cacheWriteTokens'))}</strong></div>
   <p>只统计服务商实际返回的缓存指标；未返回不代表未命中。缓存量已包含在输入用量中，不重复计入总量。</p>
  </section>}
  {usage.isLoading && <p className={s.muted}>正在读取用量…</p>}
  {usage.isError && <p className={s.error} role="alert">读取用量失败，请稍后重试。</p>}
  {!usage.isLoading && !usage.isError && rows.length === 0 && <p className={s.empty}>还没有服务商返回的 Token 用量。</p>}
  {rows.length > 0 && <section className={s.chart} aria-label="项目 Token 用量柱状图">
   <div className={s.chartHeader}><span>累计 {number(rows.reduce((sum,row) => sum+total(row),0))} Token</span><div className={s.chartLegend}><i data-kind="input"/>输入<i data-kind="output"/>输出</div></div>
   <div className={s.chartScroll}><div className={s.columns} style={{gridTemplateColumns:`repeat(${rows.length}, minmax(56px, 1fr))`,minWidth:rows.length*74}}>
    {rows.map(row => {const amount=total(row),input=row.inputTokens+row.noteInputTokens,output=row.outputTokens+row.noteOutputTokens;return <button key={row.projectSlug} className={s.column} aria-label={`${row.title}，总量 ${number(amount)} Token`} aria-pressed={selected===row.projectSlug} aria-describedby={current?.projectSlug===row.projectSlug?'token-project-detail':undefined} onMouseEnter={()=>setHovered(row.projectSlug)} onMouseLeave={()=>setHovered(undefined)} onFocus={()=>setFocused(row.projectSlug)} onBlur={()=>setFocused(undefined)} onClick={()=>setSelected(row.projectSlug)} onKeyDown={event=>{if(event.key==='Escape'){setSelected(undefined);setHovered(undefined);setFocused(undefined);}}}>
     <span className={s.barSpace}><span className={s.stack} style={{height:`${amount/maximum*100}%`}}><span data-kind="output" style={{height:`${output/Math.max(1,amount)*100}%`}}/><span data-kind="input" style={{height:`${input/Math.max(1,amount)*100}%`}}/></span></span>
     <span className={s.barLabel}>{row.title}</span>
    </button>;})}
   </div></div>
   {current && <div id="token-project-detail" className={s.tooltip} role="tooltip"><strong>{current.title}</strong><small>{current.projectSlug}</small><b>总量 {number(total(current))}</b><span>教师输入 {number(current.inputTokens)} · 输出 {number(current.outputTokens)}</span><span>笔记输入 {number(current.noteInputTokens)} · 输出 {number(current.noteOutputTokens)}</span><span>缓存命中 {reported(current.cacheReadTokens)}</span><span>缓存写入 {reported(current.cacheWriteTokens)}</span><Link to={`/project/${encodeURIComponent(current.projectSlug)}`}>进入项目</Link></div>}
  </section>}
 </div>;
}
