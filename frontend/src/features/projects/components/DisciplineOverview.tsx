import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';
import { Icon } from '@/shared/primitive/Icon';
import { MarkdownView } from '@/shared/primitive/MarkdownView';
import { qk } from '@/shared/queryKeys';
import { openStudyUnit } from '@/features/learningAssistant';
import { useDisciplineMap } from '../api/disciplineMap';
import { useCourseSummaries } from '../api/courseSummaries';
import { CourseSummaryModal } from './CourseSummaryModal';
import s from './DisciplineOverview.module.css';

export function DisciplineOverview({ slug, title }: { slug: string; title: string }) {
  const navigate = useNavigate();
  const client = useQueryClient();
  const projection = useDisciplineMap(slug);
  const summaries = useCourseSummaries(slug, projection.data?.mapVersionId);
  const [summaryTopic, setSummaryTopic] = useState<string>();
  const open = useMutation({
    mutationFn: (id: string) => openStudyUnit(slug, id),
    onSuccess: ({ slug: lesson }) => {
      void client.invalidateQueries({ queryKey: qk.projects.all() });
      navigate(`/project/${lesson}/teacher`);
    },
  });
  const topics = projection.data?.topics.topics ?? [];
  const overview = projection.data?.overviewMarkdown.trim();
  const lessons = summaries.data?.lessons ?? [];
  const chapters = new Map<string, typeof topics>();
  for (const unit of topics) chapters.set(unit.chapterTitle, [...(chapters.get(unit.chapterTitle) ?? []), unit]);

  return <main className={s.page}>
    <div className={s.content}>
      <header className={s.header}><div><p>学习主题</p><h1>{title}</h1></div>
        <Link to="/learning-calendar"><Icon name="clock" size={15} /> 学习日历</Link></header>
      {projection.isPending ? <p role="status">正在读取课程目录…</p>
        : projection.isError ? <p role="alert">课程目录暂时无法读取。</p>
          : topics.length ? <div className={s.directory} aria-label="课程目录">
            <h2>课程目录</h2>
            {[...chapters.entries()].map(([chapter, units]) => <section key={chapter}>
              {chapters.size > 1 && <h3>{chapter}</h3>}
              <ol>{units.map(unit => <li key={unit.id}>
                <button type="button" className={s.unit} disabled={open.isPending} onClick={() => open.mutate(unit.id)}>
                  <span className={s.unitIcon}><Icon name="book" size={16} /></span>
                  <span><strong>{unit.title}</strong><small>{unit.goal}</small></span>
                  <span aria-hidden="true">{open.isPending && open.variables === unit.id ? '…' : '→'}</span>
                </button>
                {lessons.some(lesson => lesson.topicId === unit.id) &&
                  <button type="button" className={s.summary} onClick={() => setSummaryTopic(unit.id)}>学习概括</button>}
              </li>)}</ol>
            </section>)}
          </div> : overview ? <section className={s.directory} aria-label="课程内容">
            <MarkdownView source={overview} />
          </section> : <section className={s.directory}><h2>课程目录</h2><p>这个主题还没有学习单元。</p><Link to="/">和学习助手讨论课程</Link></section>}
      {open.error && <p role="alert">{open.error.message}</p>}
      {summaryTopic && <CourseSummaryModal mapSlug={slug} mapVersionId={projection.data?.mapVersionId ?? ''}
        title={topics.find(topic => topic.id === summaryTopic)?.title ?? '本节'}
        lessons={lessons.filter(lesson => lesson.topicId === summaryTopic)} open
        onOpenChange={visible => { if (!visible) setSummaryTopic(undefined); }} />}
    </div>
  </main>;
}
