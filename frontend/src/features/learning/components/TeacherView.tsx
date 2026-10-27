import { useCallback, useMemo, useRef } from 'react';
import { useSources } from '../api/learningWorkspace';
import { useTeacherSession } from '../teacher/useTeacherSession';
import { TeacherComposer, type TeacherComposerHandle } from './TeacherComposer';
import { TeacherTranscript } from './TeacherTranscript';
import { TeacherQueuePanel } from './TeacherQueuePanel';
import s from './TeacherView.module.css';

export function TeacherView(props: { slug: string; title: string }) {
  return <TeacherConversation key={props.slug} {...props} />;
}

function TeacherConversation({ slug, title }: { slug: string; title: string }) {
  const session = useTeacherSession(slug);
  const sources = useSources(slug);
  const composer = useRef<TeacherComposerHandle>(null);
  const sourceNames = useMemo(() => new Map((sources.data ?? []).map(source => [source.sourceId, source.displayName])), [sources.data]);
  const sendText = useCallback((text: string) => composer.current?.sendText(text), []);
  return <section className={s.teacher}><div className={s.conversationPane}>
    <TeacherTranscript slug={slug} session={session} sourceNames={sourceNames} onSend={sendText} />
    <div className={s.composerDock}>
      <TeacherQueuePanel session={session} />
      <TeacherComposer ref={composer} slug={slug} title={title} session={session} sourceCount={(sources.data ?? []).filter(source => source.status === 'ready').length} />
    </div>
  </div></section>;
}
