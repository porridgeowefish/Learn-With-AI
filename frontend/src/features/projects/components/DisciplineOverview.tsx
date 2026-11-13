import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';

import {
  useDisciplineLearningPlan,
  useSaveDisciplineLearningPlan,
} from '@/features/projects/api/projects';
import { useFolders, useSaveFolders } from '@/features/projects/api/folders';
import { Button } from '@/shared/primitive/Button';
import { EmptyState } from '@/shared/primitive/EmptyState';
import { Icon } from '@/shared/primitive/Icon';
import { MarkdownView } from '@/shared/primitive/MarkdownView';
import { assignProject, type FolderLayout } from '@/features/projects/lib/folders';
import { GenerationControl, SourceImportControl, SourcesView } from '@/features/learning';
import { useDisciplineMap } from '../api/disciplineMap';
import { useCourseSummaries } from '../api/courseSummaries';
import { CourseSummaryModal } from './CourseSummaryModal';
import type {
  DisciplineLearningPlanResponse,
  DisciplineLearningTask,
  DisciplineLearningTaskStatus,
  LearningScopeSource,
} from '@/shared/types/api';
import { CreateProjectModal } from './CreateProjectModal';

import s from './DisciplineOverview.module.css';

interface DisciplineOverviewProps {
  providerId?: string;
  slug: string;
  title: string;
}

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

export function DisciplineOverview({ slug, title, providerId }: DisciplineOverviewProps) {
  const navigate = useNavigate();
  const projection = useDisciplineMap(slug);
  const courseSummaries = useCourseSummaries(slug, projection.data?.mapVersionId);
  const [summaryTopic, setSummaryTopic] = useState<string>();
  const lessons = useMemo(() => courseSummaries.data?.lessons ?? [], [courseSummaries.data?.lessons]);
  const overview = { ...projection, data: projection.data ? {content: projection.data.overviewMarkdown} : undefined };
  const disciplineTopics = {data: projection.data?.topics};
  const learningPlan = useDisciplineLearningPlan(slug);
  const saveLearningPlan = useSaveDisciplineLearningPlan(slug);
  const folders = useFolders();
  const saveFolders = useSaveFolders();
  const [createOpen, setCreateOpen] = useState(false);
  const [initialTitle, setInitialTitle] = useState('');
  const [initialScopeSource, setInitialScopeSource] = useState<LearningScopeSource>();
  const [launchNotice, setLaunchNotice] = useState('');
  const [activeView, setActiveView] = useState<'overview' | 'plan' | 'sources'>('overview');
  const articleRef = useRef<HTMLElement>(null);
  const documentBodyRef = useRef<HTMLDivElement>(null);
  const content = overview.data?.content ?? '';
  const outline = useMemo(() => extractDisciplineOutline(content), [content]);
  const overviewPending = overview.data?.content.includes('尚未生成') ?? true;
  const planItems = useMemo(() => learningPlan.data?.items ?? [], [learningPlan.data?.items]);
  const plannedTopics = useMemo(() => new Set(planItems.map((item) => item.topicTitle)), [planItems]);
  const topicCatalogByTitle = useMemo(
    () => new Map((disciplineTopics.data?.topics ?? []).map((topic) => [topic.title, topic])),
    [disciplineTopics.data?.topics],
  );

  const persistPlan = useCallback((items: DisciplineLearningTask[]) => {
    const next: DisciplineLearningPlanResponse = {
      schemaVersion: 1,
      items,
      updatedAt: new Date().toISOString(),
    };
    saveLearningPlan.mutate(next);
  }, [saveLearningPlan]);

  const addTopicToPlan = useCallback((topicTitle: string) => {
    if (plannedTopics.has(topicTitle)) return;
    const topic = topicCatalogByTitle.get(topicTitle);
    persistPlan([
      ...planItems,
      {
        id: crypto.randomUUID(),
        ...(topic ? { topicId: topic.id } : {}),
        topicTitle,
        status: 'planned',
        addedAt: new Date().toISOString(),
      },
    ]);
  }, [persistPlan, planItems, plannedTopics, topicCatalogByTitle]);

  const startDeepDive = (topicTitle = '', topicId?: string) => {
    const topic = topicId
      ? disciplineTopics.data?.topics.find((candidate) => candidate.id === topicId)
      : topicCatalogByTitle.get(topicTitle);
    if (topicTitle && !topic) {
      setLaunchNotice('这个主题还没有可用的范围定义。请重新生成地图中的主题边界后再开始系统学习。');
      return;
    }
    setInitialTitle(topic?.title ?? topicTitle);
    setInitialScopeSource(topic ? {
      type: 'discipline-map',
      mapSlug: slug,
      topicId: topic.id,
      mapVersionId: projection.data?.mapVersionId,
    } : undefined);
    setCreateOpen(true);
  };


  useEffect(() => {
    const host = documentBodyRef.current;
    if (!host || outline.length === 0) return;
    const headings = Array.from(host.querySelectorAll<HTMLHeadingElement>('h2, h3, h4'));
    const cleanups: Array<() => void> = [];
    headings.forEach((heading, index) => {
      const item = outline[index];
      if (!item) return;
      heading.dataset.outlineId = item.id;
      if (!item.actionable) return;
      heading.classList.add(s.actionableHeading);
      const priority = topicCatalogByTitle.get(item.title)?.priority;
      const badge = document.createElement('span');
      badge.className = `${s.priority} ${priority ? s[`priority${priority}`] : s.priorityUnknown}`;
      badge.textContent = priority ? { I: 'Ⅰ', II: 'Ⅱ', III: 'Ⅲ' }[priority] : '待评级';
      badge.title = priority ? { I: '关键必学', II: '中等深度', III: '深入选学' }[priority] : '重新生成地图后可获得优先级';
      badge.setAttribute('aria-label', `学习优先级：${badge.title}`);
      heading.appendChild(badge);
      const planButton = document.createElement('button');
      planButton.type = 'button';
      planButton.className = s.inlinePlan;
      const topicPlanned = plannedTopics.has(item.title);
      planButton.textContent = topicPlanned ? '已加入计划' : '加入计划';
      planButton.disabled = topicPlanned || saveLearningPlan.isPending;
      planButton.setAttribute('aria-label', topicPlanned ? `已加入计划：${item.title}` : `加入计划：${item.title}`);
      const handlePlanClick = () => addTopicToPlan(item.title);
      planButton.addEventListener('click', handlePlanClick);
      heading.appendChild(planButton);
      const button = document.createElement('button');
      button.type = 'button';
      button.className = s.inlineDeepDive;
      button.textContent = '深入学习';
      button.setAttribute('aria-label', `深入学习：${item.title}`);
      const handleClick = () => startDeepDive(item.title, topicCatalogByTitle.get(item.title)?.id);
      button.addEventListener('click', handleClick);
      heading.appendChild(button);
      const topicId = topicCatalogByTitle.get(item.title)?.id;
      const summaryButton = document.createElement('button');
      summaryButton.type = 'button';
      summaryButton.className = s.inlinePlan;
      summaryButton.textContent = '概括';
      summaryButton.setAttribute('aria-label', `查看概括：${item.title}`);
      const handleSummary = () => setSummaryTopic(topicId);
      summaryButton.addEventListener('click', handleSummary);
      if (topicId) heading.appendChild(summaryButton);
      cleanups.push(() => {
        summaryButton.removeEventListener('click', handleSummary);
        summaryButton.remove();
        planButton.removeEventListener('click', handlePlanClick);
        planButton.remove();
        button.removeEventListener('click', handleClick);
        button.remove();
        badge.remove();
        heading.classList.remove(s.actionableHeading);
        delete heading.dataset.outlineId;
      });
    });
    return () => cleanups.forEach((cleanup) => cleanup());
  }, [outline, plannedTopics, addTopicToPlan, saveLearningPlan.isPending, topicCatalogByTitle, activeView, lessons]);

  const scrollToOutline = (id: string) => {
    articleRef.current
      ?.querySelector<HTMLElement>(`[data-outline-id="${id}"]`)
      ?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  };

  return (
    <div className={s.host}>
      <header className={s.header}>
        <div>
          <h1>{title}</h1>
          <p>一个统一入口，帮助你看清学科边界、主要领域及可选择的学习路线。</p>
          <div className={s.viewTabs} role="tablist" aria-label="学科地图视图">
            <button
              type="button"
              role="tab"
              aria-selected={activeView === 'overview'}
              className={activeView === 'overview' ? s.viewTabActive : undefined}
              onClick={() => setActiveView('overview')}
            >
              学科总览
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={activeView === 'plan'}
              className={activeView === 'plan' ? s.viewTabActive : undefined}
              onClick={() => setActiveView('plan')}
            >
              学习计划
            </button>
            <button type="button" role="tab" aria-selected={activeView==='sources'} onClick={()=>setActiveView('sources')}>参考资料</button>
          </div>
        </div>
        <div className={s.headerTools}>
          {activeView === 'overview' && <div className={s.priorityLegend} aria-label="学习优先级图例">
            <span><b className={`${s.priority} ${s.priorityI}`}>Ⅰ</b>关键必学</span>
            <span><b className={`${s.priority} ${s.priorityII}`}>Ⅱ</b>中等深度</span>
            <span><b className={`${s.priority} ${s.priorityIII}`}>Ⅲ</b>深入选学</span>
          </div>}
          <div className={s.actions}><GenerationControl slug={slug} kind="discipline-map" initialProviderId={providerId}/><SourceImportControl slug={slug}/></div>
        </div>
      </header>

      {launchNotice&&<p role="status">{launchNotice}</p>}
      {courseSummaries.error && <p role="alert">暂时无法读取学习概括，请稍后刷新。</p>}
      <main className={s.scroll}>
        {activeView === 'sources' ? <SourcesView slug={slug}/> : activeView === 'plan' ? (
          learningPlan.isLoading ? (
            <div className={s.loading}>正在读取学习计划…</div>
          ) : learningPlan.error ? (
            <EmptyState title="暂时无法读取学习计划" description={(learningPlan.error as Error).message} />
          ) : (
            <LearningTaskList
              items={planItems}
              saving={saveLearningPlan.isPending}
              onChange={persistPlan}
              onStartDeepDive={startDeepDive}
              onBackToOverview={() => setActiveView('overview')}
            />
          )
        ) : overview.isLoading ? (
          <div className={s.loading}>正在读取学科总览…</div>
        ) : overview.error ? (
          <EmptyState title="暂时无法读取学科总览" description={(overview.error as Error).message} />
        ) : overviewPending ? (
          <section className={s.emptyOverview}>
            <Icon name="book" size={28} />
            <h2>还没有学科总览</h2>
            <p>生成地图后，这里会成为该学科统一的总览入口。</p>
            <GenerationControl slug={slug} kind="discipline-map" initialProviderId={providerId}/>
          </section>
        ) : (
          <article ref={articleRef} className={s.paper}>
            {projection.data?.coverage?.gap && <aside className={s.coverageNotice} aria-label="资料覆盖范围"><strong>资料依据</strong><p>{projection.data.coverage.gap}</p>{projection.data.coverage.sources?.map((source, index) => <small key={index}>{source.name}{source.extractionCoverage?.unreadUnits?.length ? ` · ${source.extractionCoverage.unreadUnits.length} 处内容未提取` : ''}</small>)}</aside>}
            {outline.length > 0 && (
              <nav className={s.tableOfContents} aria-label="学科总览目录">
                <h2>目录</h2>
                <ol>
                  {outline.map((item) => (
                    <li
                      key={item.id}
                      className={item.level === 4 ? s.tocTopic : item.level === 3 ? s.tocSubsection : undefined}
                    >
                      <button type="button" onClick={() => scrollToOutline(item.id)}>
                        <span className={s.tocNumber}>{item.number}</span>
                        <span>{item.title}{item.actionable && lessons.some(l => l.topicId === topicCatalogByTitle.get(item.title)?.id && l.summaries?.some(summary => summary.status === 'completed')) && <span className={s.summaryMark} aria-label="已学">✓</span>}</span>
                        <i aria-hidden="true" />
                      </button>
                    </li>
                  ))}
                </ol>
              </nav>
            )}
            <div ref={documentBodyRef} className={s.documentBody}>
              <MarkdownView source={content} className={s.markdown} />
            </div>
          </article>
        )}
      </main>

      {summaryTopic && <CourseSummaryModal mapSlug={slug}
        mapVersionId={projection.data?.mapVersionId ?? ''}
        title={disciplineTopics.data?.topics.find(t => t.id === summaryTopic)?.title ?? '本节'}
        lessons={lessons.filter(l => l.topicId === summaryTopic)} open
        onOpenChange={open => { if (!open) setSummaryTopic(undefined); }} />}
      <CreateProjectModal
        open={createOpen}
        onOpenChange={(nextOpen) => {
          setCreateOpen(nextOpen);
          if (!nextOpen) setInitialScopeSource(undefined);
        }}
        fromDisciplineMap
        fixedProjectType="system-learning"
        initialTitle={initialTitle}
        initialScopeSource={initialScopeSource}
        onCreated={(projectSlug) => {
          const layout: FolderLayout = folders.data ?? { folders: [] };
          const mapFolder = layout.folders.find((folder) => folder.mapProjectSlug === slug);
          if (!mapFolder) {
            navigate(`/project/${projectSlug}/teacher`);
            return;
          }
          void saveFolders
            .mutateAsync(assignProject(layout, projectSlug, mapFolder.id))
            .finally(() => navigate(`/project/${projectSlug}/teacher`));
        }}
      />
    </div>
  );
}

function LearningTaskList({
  items,
  saving,
  onChange,
  onStartDeepDive,
  onBackToOverview,
}: {
  items: DisciplineLearningTask[];
  saving: boolean;
  onChange: (items: DisciplineLearningTask[]) => void;
  onStartDeepDive: (topic: string, topicId?: string) => void;
  onBackToOverview: () => void;
}) {
  const completed = items.filter((item) => item.status === 'completed').length;

  const move = (index: number, delta: number) => {
    const target = index + delta;
    if (target < 0 || target >= items.length) return;
    const next = [...items];
    [next[index], next[target]] = [next[target], next[index]];
    onChange(next);
  };

  const setStatus = (id: string, status: DisciplineLearningTaskStatus) => {
    const now = new Date().toISOString();
    onChange(items.map((item) => {
      if (item.id !== id) return item;
      if (status === 'planned') {
        return { ...item, status, startedAt: undefined, completedAt: undefined };
      }
      if (status === 'in-progress') {
        return { ...item, status, startedAt: item.startedAt ?? now, completedAt: undefined };
      }
      return { ...item, status, startedAt: item.startedAt ?? now, completedAt: now };
    }));
  };

  if (items.length === 0) {
    return (
      <section className={s.emptyOverview}>
        <Icon name="book" size={28} />
        <h2>任务清单还是空的</h2>
        <p>回到学科总览，按照你想学的顺序，把细分主题逐个加入计划。</p>
        <Button variant="primary" onClick={onBackToOverview}>去选择学习任务</Button>
      </section>
    );
  }

  return (
    <section className={s.planPage} aria-label="学习任务清单">
      <header className={s.planHeader}>
        <div>
          <span className={s.planEyebrow}>由你决定学习顺序</span>
          <h2>学习任务清单</h2>
          <p>按加入顺序学习，也可以随时调整。完成一个任务后在这里打卡。</p>
        </div>
        <div className={s.planProgress} aria-label={`已打卡 ${completed} 项，共 ${items.length} 项`}>
          <strong>{completed}/{items.length}</strong>
          <span>已打卡</span>
        </div>
      </header>

      <ol className={s.taskList}>
        {items.map((item, index) => (
          <li key={item.id} className={item.status === 'completed' ? s.taskCompleted : undefined}>
            <span className={s.taskOrder}>{index + 1}</span>
            <div className={s.taskBody}>
              <strong>{item.topicTitle}</strong>
              <span>{taskStatusLabel(item.status)}</span>
            </div>
            <div className={s.taskActions}>
              <button type="button" onClick={() => move(index, -1)} disabled={saving || index === 0}>上移</button>
              <button type="button" onClick={() => move(index, 1)} disabled={saving || index === items.length - 1}>下移</button>
              <button type="button" onClick={() => onStartDeepDive(item.topicTitle, item.topicId)} disabled={saving}>系统学习</button>
              {item.status === 'planned' && (
                <button type="button" className={s.taskPrimary} onClick={() => setStatus(item.id, 'in-progress')} disabled={saving}>开始</button>
              )}
              {item.status === 'in-progress' && (
                <button type="button" className={s.taskPrimary} onClick={() => setStatus(item.id, 'completed')} disabled={saving}>完成打卡</button>
              )}
              {item.status === 'completed' && (
                <button type="button" onClick={() => setStatus(item.id, 'planned')} disabled={saving}>重新打开</button>
              )}
              <button
                type="button"
                className={s.taskDanger}
                onClick={() => onChange(items.filter((candidate) => candidate.id !== item.id))}
                disabled={saving}
              >
                移除
              </button>
            </div>
          </li>
        ))}
      </ol>
    </section>
  );
}

function taskStatusLabel(status: DisciplineLearningTaskStatus) {
  if (status === 'in-progress') return '正在学习';
  if (status === 'completed') return '已完成打卡';
  return '尚未开始';
}
