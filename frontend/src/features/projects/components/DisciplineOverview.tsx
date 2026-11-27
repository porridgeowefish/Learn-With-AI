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

export function extractDisciplineTopics(markdown: string): string[] {
  return extractDisciplineOutline(markdown)
    .filter((item) => item.actionable)
    .map((item) => item.title);
}

export interface DisciplineOutlineItem {
  id: string;
  level: 2 | 3 | 4;
  number: string;
  title: string;
  actionable: boolean;
}

export function extractDisciplineOutline(markdown: string): DisciplineOutlineItem[] {
  const outline: Array<Omit<DisciplineOutlineItem, 'actionable'>> = [];
  let section = 0;
  let subsection = 0;
  let topic = 0;
  for (const rawLine of markdown.split(/\r?\n/)) {
    const line = rawLine.trim();
    const h2 = /^##\s+(.+)$/.exec(line);
    if (h2) {
      section += 1;
      subsection = 0;
      topic = 0;
      const title = cleanHeading(h2[1]);
      outline.push({
        id: `outline-${outline.length + 1}`,
        level: 2,
        number: String(section),
        title,
      });
      continue;
    }
    const h3 = /^###\s+(.+)$/.exec(line);
    if (h3) {
      subsection += 1;
      topic = 0;
      const title = cleanHeading(h3[1]);
      outline.push({
        id: `outline-${outline.length + 1}`,
        level: 3,
        number: `${section}.${subsection}`,
        title,
      });
      continue;
    }
    const h4 = /^####\s+(.+)$/.exec(line);
    if (!h4) continue;
    topic += 1;
    const title = cleanHeading(h4[1]);
    outline.push({
      id: `outline-${outline.length + 1}`,
      level: 4,
      number: `${section}.${subsection}.${topic}`,
      title,
    });
  }
  const hasLevelFourTopics = outline.some((item) => item.level === 4);
  return outline.map((item) => ({
    ...item,
    // New maps reserve H4 for learnable topics. Existing H2/H3 maps remain
    // actionable without a migration or forced regeneration.
    actionable: hasLevelFourTopics ? item.level === 4 : item.level === 3,
  }));
}

function cleanHeading(value: string): string {
  return value.replace(/[*_`]/g, '').trim();
}

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
            <p className={s.guide}>学完一个单元后，在课堂里说「我学完了」，学习助手会自动安排复习。</p>
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
          </div> : <div className={s.directory}><h2>课程目录</h2><p>这个主题还没有学习单元。</p><Link to="/">和学习助手讨论课程</Link></div>}
      {open.error && <p role="alert">{open.error.message}</p>}
      {projection.data?.overviewMarkdown && projection.data.mapVersionId !== 'confirmed' &&
        <details className={s.overview}><summary>查看课程说明</summary><MarkdownView source={projection.data.overviewMarkdown} /></details>}
      {summaryTopic && <CourseSummaryModal mapSlug={slug} mapVersionId={projection.data?.mapVersionId ?? ''}
        title={topics.find(topic => topic.id === summaryTopic)?.title ?? '本节'}
        lessons={lessons.filter(lesson => lesson.topicId === summaryTopic)} open
        onOpenChange={visible => { if (!visible) setSummaryTopic(undefined); }} />}
    </div>
  </main>;
}
