import { useEffect, useMemo, useState } from 'react';

import { useActivity, type ActivityDay } from '@/features/projects/api/activity';
import type { ProjectMeta } from '@/shared/types/domain';

import s from './LearningRhythm.module.css';

interface Props { projects: ProjectMeta[] }

export function LearningRhythm({ projects }: Props) {
  const [weeks, setWeeks] = useState<26 | 52>(26);
  const [project, setProject] = useState('');
  const activity = useActivity(weeks, project);
  const summary = activity.data;
  const [selectedDate, setSelectedDate] = useState('');

  useEffect(() => {
    if (!summary?.days.length) return;
    const latestActive = [...summary.days].reverse().find((day) => day.activity > 0);
    setSelectedDate((current) => summary.days.some((day) => day.date === current) ? current : (latestActive?.date ?? summary.rangeEnd));
  }, [summary]);

  const weekLabels = useMemo(() => buildWeekLabels(summary?.days ?? []), [summary?.days]);
  return (
    <section className={s.section} aria-labelledby="learning-rhythm-title">
      <header className={s.head}>
        <div>
          <h2 id="learning-rhythm-title">学习节律</h2>
          <p>颜色表示每日投入，连续天数记录坚持；成长值只来自可验证的学习成果。</p>
        </div>
        <div className={s.filters}>
          <select value={project} onChange={(event) => setProject(event.target.value)} aria-label="筛选学习路线">
            <option value="">全部学习内容</option>
            {projects.map((item) => <option key={item.slug} value={item.slug}>{item.title}</option>)}
          </select>
          <button className={weeks === 26 ? s.activeRange : ''} onClick={() => setWeeks(26)}>近26周</button>
          <button className={weeks === 52 ? s.activeRange : ''} onClick={() => setWeeks(52)}>近一年</button>
        </div>
      </header>

      <div className={s.rhythmBody} aria-busy={activity.isLoading}>
        <aside className={s.streak}>
          <div className={s.streakValue}><strong>{summary?.currentStreak ?? 0} 天</strong></div>
          <span>当前连续学习</span>
          <p><b>保持自己的节律</b>稳定回来学习，比追求单日高强度更重要。</p>
        </aside>
        <div className={s.calendarScroll}>
          <div className={s.months} style={{ gridTemplateColumns: `repeat(${weekLabels.length}, 15px)` }}>
            {weekLabels.map((label, index) => <span key={`${label}-${index}`}>{label}</span>)}
          </div>
          <div className={s.calendarLine}>
            <div className={s.weekdays} aria-hidden="true"><span>一</span><span>三</span><span>五</span></div>
            <div className={s.heatmap} role="grid" aria-label={`最近${weeks}周学习投入`}>
              {(summary?.days ?? []).map((day) => (
                <button
                  key={day.date}
                  type="button"
                  role="gridcell"
                  className={`${s.day} ${selectedDate === day.date ? s.selected : ''}`}
                  aria-selected={selectedDate === day.date}
                  data-level={activityLevel(day.activity)}
                  title={`${formatDate(day.date)} · 热度 ${day.activity} · ${day.actions} 次学习行动`}
                  aria-label={`${formatDate(day.date)}，投入热度 ${day.activity}，${day.actions} 次学习行动`}
                  onClick={() => setSelectedDate(day.date)}
                />
              ))}
            </div>
          </div>
          <div className={s.legend}><span>悬浮查看每日投入</span><span>少 <i data-level="0" /><i data-level="1" /><i data-level="2" /><i data-level="3" /><i data-level="4" /> 多</span></div>
        </div>
      </div>

      <div className={s.metrics}>
        <Metric label={`近${weeks}周活跃`} value={summary?.activeDays ?? 0} suffix="天" />
        <Metric label="最长连续" value={summary?.longestStreak ?? 0} suffix="天" />
        <Metric label="有效学习行动" value={summary?.totalActions ?? 0} suffix="次" />
        <Metric label="累计成长值" value={summary?.totalGrowth ?? 0} suffix="分" />
      </div>

    </section>
  );
}

function Metric({ label, value, suffix }: { label: string; value: number; suffix: string }) {
  return <div className={s.metric}><span>{label}</span><strong>{value}</strong><small>{suffix}</small></div>;
}

function buildWeekLabels(days: ActivityDay[]) {
  const labels: string[] = [];
  let previous = -1;
  for (let i = 0; i < days.length; i += 7) {
    const month = Number(days[i]?.date.slice(5, 7) ?? 0);
    labels.push(month && month !== previous ? `${month}月` : '');
    previous = month || previous;
  }
  return labels;
}

function activityLevel(value: number) { return value === 0 ? 0 : value <= 2 ? 1 : value <= 5 ? 2 : value <= 9 ? 3 : 4; }
function formatDate(value: string) { const [, month, day] = value.split('-'); return `${Number(month)}月${Number(day)}日`; }
