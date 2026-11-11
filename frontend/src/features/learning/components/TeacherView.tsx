import { useCallback, useMemo, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import { readCloudStatus } from '@/features/cloud-account';
import { useSources } from '../api/learningWorkspace';
import { useTeacherSession } from '../teacher/useTeacherSession';
import { TeacherComposer, type TeacherComposerHandle } from './TeacherComposer';
import { TeacherTranscript } from './TeacherTranscript';
import { TeacherQueuePanel } from './TeacherQueuePanel';
import { LessonRating } from './LessonRating';
import s from './TeacherView.module.css';

export function TeacherView(props: { slug: string; title: string }) {
  return <TeacherConversation key={props.slug} {...props} />;
}

function TeacherConversation({ slug, title }: { slug: string; title: string }) {
  const session = useTeacherSession(slug);
  const sources = useSources(slug);
  const cloud = useQuery({ queryKey: ['cloud-account'], queryFn: readCloudStatus, retry: false });
  const composer = useRef<TeacherComposerHandle>(null);
  const sourceNames = useMemo(() => new Map((sources.data ?? []).map(source => [source.sourceId, source.displayName])), [sources.data]);
  const sendText = useCallback((text: string) => composer.current?.sendText(text), []);
  const latest = session.conversation.data?.messages.at(-1);
  return <section className={s.teacher}><div className={s.conversationPane}>
    <TeacherTranscript slug={slug} session={session} sourceNames={sourceNames} onSend={sendText} />
    <div className={s.composerDock}>
      <TeacherQueuePanel session={session} />
      <TeacherComposer ref={composer} slug={slug} title={title} session={session} cloudStatus={cloud.data} sourceCount={(sources.data ?? []).filter(source => source.status === 'ready').length} />
    </div>
    {!session.live && latest?.lessonEnded && latest.lessonRating === undefined &&
      <LessonRating key={latest.id} slug={slug} messageId={latest.id} />}
  </div></section>;
}
