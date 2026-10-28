import { CollectionShortcut } from '@/features/treasures';
import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { NavLink, useNavigate, useParams } from 'react-router-dom';

import type { ProjectState } from '@/shared/types/domain';
import { TeacherView } from './TeacherView';
import { ConversationInstructions } from './ConversationInstructions';
import { AssetsView } from './AssetsView';
import { SourcesView } from './SourcesView';
import { SourceImportControl } from './SourceImportControl';
import { subscribeToSSE } from '@/app/hooks/useSSE';
import { SSE_EVENTS } from '@/shared/lib/constants';
import { conversationExportURL, learningWorkspaceKeys } from '@/features/learning/api/learningWorkspace';
import { DownloadIcon } from '@/shared/primitive/Icons';

import s from './LearningWorkspace.module.css';

const tabs = [
  { key: 'teacher', label: '教师' },
  { key: 'assets', label: '笔记与复习' },
  { key: 'sources', label: '附件' },
] as const;

export function LearningWorkspace({ project }: { project: ProjectState }) {
  const { zone } = useParams<{ zone?: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const active = tabs.some((tab) => tab.key === zone) ? zone! : 'teacher';

  useEffect(() => {
    if (zone !== active) navigate(`/project/${project.slug}/${active}`, { replace: true });
  }, [active, navigate, project.slug, zone]);

  useEffect(() => {
    const forProject = (refresh: () => void) => (payload: unknown) => {
      if ((payload as { projectSlug?: string })?.projectSlug === project.slug) {
        refresh();
      }
    };
    const unsubscribers = [
      subscribeToSSE(SSE_EVENTS.teacherNoteUpdated, forProject(() => {void queryClient.invalidateQueries({ queryKey: ['learning-workspace',project.slug,'notes'] });void queryClient.invalidateQueries({queryKey:['usage']});})),
      subscribeToSSE(SSE_EVENTS.learningAssetUpdated, forProject(() => { void queryClient.invalidateQueries({ queryKey: learningWorkspaceKeys.assets(project.slug) }); })),
      subscribeToSSE(SSE_EVENTS.sourceUpdated, forProject(() => { void queryClient.invalidateQueries({ queryKey: learningWorkspaceKeys.sources(project.slug) }); })),
      subscribeToSSE(SSE_EVENTS.conversationQueueUpdated, forProject(() => { void queryClient.invalidateQueries({ queryKey: learningWorkspaceKeys.conversation(project.slug) }); })),
      subscribeToSSE(SSE_EVENTS.sourceStructureJobUpdated, forProject(() => { void queryClient.invalidateQueries({ queryKey: learningWorkspaceKeys.sources(project.slug) }); })),
      subscribeToSSE(SSE_EVENTS.annotationUpdated, forProject(() => { void queryClient.invalidateQueries({ queryKey: learningWorkspaceKeys.annotations(project.slug) }); })),
    ];
    return () => unsubscribers.forEach((unsubscribe) => unsubscribe());
  }, [project.slug, queryClient]);

  return (
    <div className={s.workspace}>
      <header className={s.header}>
        <div className={s.identity}>
          <h1>{project.title}</h1>
          <span>学习单元</span>
        </div>
        <nav className={s.tabs} aria-label="学习单元">
          {tabs.map((tab) => (
            <NavLink key={tab.key} to={`/project/${project.slug}/${tab.key}`} className={({ isActive }) => isActive ? s.tabActive : s.tab}>
              {tab.label}
            </NavLink>
          ))}
        </nav>
        <div className={s.headerActions}>
          <CollectionShortcut/>
          {active === 'teacher' && <ConversationInstructions key={`instructions-${project.slug}`} slug={project.slug} />}
          {active === 'sources' && <SourceImportControl key={`sources-${project.slug}`} slug={project.slug} compact />}
          {active === 'teacher' && <a href={conversationExportURL(project.slug)} download aria-label="导出全部对话" title="导出全部对话为 Markdown"><DownloadIcon /><span>导出对话</span></a>}
        </div>
      </header>
      <main className={`${s.content} ${s.withTreasure}`}>
        <div className={s.mainPane}>
        {active === 'teacher' && <TeacherView key={project.slug} slug={project.slug} title={project.title} />}
        {active === 'assets' && <AssetsView slug={project.slug} />}
        {active === 'sources' && <SourcesView slug={project.slug} />}
        </div><div className={s.treasureSlot} data-treasure-slot/>
      </main>
    </div>
  );
}
