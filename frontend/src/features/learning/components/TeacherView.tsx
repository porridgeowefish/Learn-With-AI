import { useCallback, useMemo, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import { readCloudStatus } from '@/features/cloud-account';
import { useSources } from '../api/learningWorkspace';
import { useTeacherSession } from '../teacher/useTeacherSession';
import { TeacherComposer, type TeacherComposerHandle } from './TeacherComposer';
import { TeacherTranscript } from './TeacherTranscript';
import { TeacherQueuePanel } from './TeacherQueuePanel';
import s from './TeacherView.module.css';

type TeacherViewProps = { slug: string; title: string; initialProviderId?: string };

export function TeacherView(props: TeacherViewProps) {
  return <TeacherConversation key={props.slug} {...props} />;
}

function TeacherConversation({ slug, title, initialProviderId }: TeacherViewProps) {
  const session = useTeacherSession(slug);
  const sources = useSources(slug);
  const cloud = useQuery({ queryKey: ['cloud-account'], queryFn: readCloudStatus, retry: false });
  const composer = useRef<TeacherComposerHandle>(null);
  const sourceNames = useMemo(() => new Map((sources.data ?? []).map(source => [source.sourceId, source.displayName])), [sources.data]);
  const sendText = useCallback((text: string) => composer.current?.sendText(text), []);
  return <section className={s.teacher}><div className={s.conversationPane}>
    <TeacherTranscript slug={slug} session={session} sourceNames={sourceNames} onSend={sendText} />
    <div className={s.composerDock}>
      <TeacherQueuePanel session={session} />
      <TeacherComposer ref={composer} slug={slug} title={title} initialProviderId={initialProviderId} session={session} cloudStatus={cloud.data} sourceCount={(sources.data ?? []).filter(source => source.status === 'ready').length} />
    </div>
  </div></section>;
}
