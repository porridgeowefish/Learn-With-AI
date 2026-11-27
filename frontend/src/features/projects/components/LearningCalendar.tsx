import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { qk } from '@/shared/queryKeys';
import { openStudyUnit, useStudyCalendar, type StudyEvent } from '@/features/learningAssistant';
import s from './LearningCalendar.module.css';

const weekdays = ['一', '二', '三', '四', '五', '六', '日'];
const fullDate = new Intl.DateTimeFormat('zh-CN', { month: 'long', day: 'numeric', weekday: 'long' });

function dayKey(date: Date) {
  return [date.getFullYear(), date.getMonth() + 1, date.getDate()]
    .map((value, index) => String(value).padStart(index === 0 ? 4 : 2, '0')).join('-');
}

export function LearningCalendar() {
  const calendar = useStudyCalendar();
  const navigate = useNavigate();
  const client = useQueryClient();
  const [selected, setSelected] = useState(() => dayKey(new Date()));
  const [month, setMonth] = useState(() => new Date());
  const today = calendar.data?.today ?? dayKey(new Date());
  const start = new Date(month.getFullYear(), month.getMonth(), 1);
  const offset = (start.getDay() + 6) % 7;
  const last = new Date(month.getFullYear(), month.getMonth() + 1, 0).getDate();
  const count = Math.ceil((offset + last) / 7) * 7;
  const byDate = new Map<string, StudyEvent[]>();
  for (const item of calendar.data?.items ?? []) {
    const previous = byDate.get(item.date) ?? [];
    byDate.set(item.date, [...previous, item]);
  }
  const todayTasks = byDate.get(selected) ?? [];
  const open = useMutation({
    mutationFn: (item: StudyEvent) => openStudyUnit(item.slug!, item.topicId!),
    onSuccess: ({ slug }) => {
      void client.invalidateQueries({ queryKey: qk.projects.all() });
      navigate(`/project/${slug}/teacher`);
    },
  });

  useEffect(() => { open.reset(); }, [selected]);

  function goToday() {
    setSelected(today);
    setMonth(new Date(today + 'T12:00:00'));
  }
  function turnMonth(step: number) {
    const target = new Date(start.getFullYear(), start.getMonth() + step, 1);
    setMonth(target);
    setSelected(dayKey(target));
  }

  return <main className={s.page} aria-label="学习日历" data-tour="welcome">
    <div className={s.content}>
      <header className={s.heading}><h1>学习日历</h1></header>
      {calendar.isError && <p role="alert">暂时无法读取学习日历。<button type="button" onClick={() => void calendar.refetch()}>重试</button></p>}
      <div className={s.layout}>
        <section className={s.month} aria-label="月历">
          <div className={s.toolbar}>
            <div className={s.monthTitle}><small>{start.getFullYear()} 年</small><h2>{start.getMonth() + 1} 月</h2></div>
            <div className={s.controls}>
              <button type="button" aria-label="上个月" onClick={() => turnMonth(-1)}>‹</button>
              <button type="button" onClick={goToday}>今天</button>
              <button type="button" aria-label="下个月" onClick={() => turnMonth(1)}>›</button>
            </div>
          </div>
          <div className={s.grid}>
            {weekdays.map(day => <div className={s.weekday} key={day}>周{day}</div>)}
            {Array.from({ length: count }, (_, index) => {
              const date = new Date(start.getFullYear(), start.getMonth(), index - offset + 1);
              const key = dayKey(date);
              const items = byDate.get(key) ?? [];
              return <button type="button" className={s.day} key={key} data-outside={date.getMonth() !== start.getMonth()}
                aria-label={key + (items.length ? `，${items.length} 项任务` : '，无任务')}
                aria-current={key === today ? 'date' : undefined} aria-pressed={key === selected}
                onClick={() => { setSelected(key); setMonth(date); }}>
                <time dateTime={key} className={s.dayNumber}>{date.getDate()}</time>
                {items.slice(0, 3).map(item => <span className={s.event} key={item.id}
                  data-kind={item.kind} data-completed={item.completed} title={item.title}>
                  <i aria-hidden="true" /><span>{item.title}</span>
                </span>)}
                {items.length > 3 && <small className={s.more}>另有 {items.length - 3} 项</small>}
              </button>;
            })}
          </div>
          <footer className={s.legend}>
            <span><i data-kind="study" />学习</span>
            <span><i data-kind="review" />复习</span>
          </footer>
        </section>
        <aside className={s.agenda} aria-label="当日安排">
          <header>
            <h2>{fullDate.format(new Date(selected + 'T12:00:00'))}</h2>
            <p>{selected === today ? '今天' : '当日单元'}</p>
          </header>
          {calendar.isPending ? <p className={s.empty}>正在读取日历…</p>
            : !todayTasks.length ? <p className={s.empty}>当天没有安排。</p>
              : <ul>{todayTasks.map(item => <li className={s.task} key={item.id} data-kind={item.kind}>
                <div className={s.taskKind}><i />{item.kind === 'review' ? '复习' : '学习'}{item.completed && <span> · 已完成</span>}</div>
                <h3>{item.title}</h3>
                {item.course && <small>{item.course}</small>}
                {item.detail && <p>{item.detail}</p>}
                {item.kind === 'review' && !item.completed &&
                  <Link to={'/?' + new URLSearchParams({ review: item.id })}>去复习 <span aria-hidden="true">→</span></Link>}
                {item.kind === 'study' && item.slug && item.topicId &&
                  <button type="button" disabled={open.isPending} onClick={() => open.mutate(item)}>
                    {open.isPending && open.variables?.id === item.id ? '正在打开…' : '进入课堂'} <span aria-hidden="true">→</span>
                  </button>}
                {item.kind === 'study' && item.slug && !item.topicId && <Link to={`/project/${item.slug}`}>打开目录 →</Link>}
              </li>)}</ul>}
          {open.error && <p role="alert">{open.error.message}</p>}
        </aside>
      </div>
    </div>
  </main>;
}
