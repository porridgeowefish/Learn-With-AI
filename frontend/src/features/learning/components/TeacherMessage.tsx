import { memo, useId, useState } from 'react';
import type { ConversationMessage } from '../api/learningWorkspace';
import { CopyIcon, DownloadIcon, RegenerateIcon } from '@/shared/primitive/Icons';
import { MarkdownView } from '@/shared/primitive/MarkdownView';
import { SourceCitations } from './SourceCitations';
import { ChatMediaBlock } from './ChatMediaInput';
import { teacherMarkdown } from './LearningProgress';
import s from './TeacherView.module.css';

// MessageActions stays primitive-valued (or stable callbacks) so the memo
// comparator can keep history messages frozen while live text streams.
interface MessageActions {
  copied: boolean;
  onCopy: (message: ConversationMessage) => void;
  onDownload: (message: ConversationMessage) => void;
  canRegenerate: boolean;
  onRegenerate: () => void;
}

export const TeacherMessage = memo(function Message({ slug, message, sourceNames, reasoningOpen, setReasoningOpen, notice, streaming = false, actions }: { slug: string; message: ConversationMessage; sourceNames: Map<string, string>; reasoningOpen: boolean; setReasoningOpen: (messageID: string, open: boolean) => void; notice?: string; streaming?: boolean; actions?: MessageActions }) {
  const teacher = message.role === 'teacher';
  const [selectedEvidence, setSelectedEvidence] = useState('');
  const citations = message.sourceCitations ?? [];
  const citationSource = (source: string) => source.replace(/\[\[source:([^\]]+)\]\]/g, (_marker, id: string) => {
    const index = citations.findIndex(item => item.evidenceId === id);
    return index < 0 ? '' : `[资料${index + 1}](#source-${id})`;
  });
  return (
    <article data-tour={teacher ? undefined : "learner-message"} className={teacher ? s.teacherMessage : s.learnerMessage}>
      {teacher && <div className={s.speaker}>教师</div>}
      <div className={s.messageBody} onClick={event => {
        const target = event.target;
        if (!(target instanceof Element)) return;
        const href = target.closest('a')?.getAttribute('href');
        if (!href?.startsWith('#source-')) return;
        const evidenceId = href.slice('#source-'.length);
        if (!citations.some(item => item.evidenceId === evidenceId)) return;
        event.preventDefault();
        setSelectedEvidence(evidenceId);
      }}>
        {streaming && !message.blocks?.some((block) => block.type === 'markdown' && block.source) && <span className={s.thinking} role="status">教师正在生成…</span>}
        {(message.blocks ?? []).map((block) => block.type === 'reasoning-summary'
          ? <ReasoningBlock key={block.id} messageID={message.id} source={block.source ?? ''} streaming={streaming} open={reasoningOpen} onOpenChange={setReasoningOpen} />
          : block.type === 'markdown' ? <MarkdownView key={block.id} source={citationSource(teacher ? teacherMarkdown(block.source ?? '') : block.source ?? '')} className={s.markdown} streaming={streaming} />
            : block.type === 'attachment' ? <span key={block.id} className={s.attachment}>附件 · {sourceNames.get(block.artifactRef ?? '') ?? '已选择附件'}</span>
              : block.type === 'chat-image' || block.type === 'chat-audio' ? <ChatMediaBlock key={block.id} slug={slug} id={block.artifactRef ?? ''} kind={block.type === 'chat-image' ? 'image' : 'audio'} name={block.name} text={block.source} /> : null)}
        {teacher && !streaming && <SourceCitations slug={slug} citations={citations} sourceNames={sourceNames} selected={selectedEvidence} onSelect={setSelectedEvidence} />}
        {notice && <div className={s.notice}>{notice}</div>}
        {teacher && !streaming && message.status === 'failed' && <div className={s.notice} role="alert">这条回复未完成，请重新生成或继续提问。</div>}        {actions && !streaming && (
          <div className={s.messageActions}>
            <button type="button" className={s.iconButton} title={actions.copied ? '已复制' : '复制原文'} aria-label={actions.copied ? '已复制' : '复制原文'} onClick={() => void actions.onCopy(message)}><CopyIcon />{actions.copied && <span className={s.copiedMark}>已复制</span>}</button>
            <button type="button" className={s.iconButton} title="下载本条文本" aria-label="下载本条文本" onClick={() => actions.onDownload(message)}><DownloadIcon /></button>
            {actions.canRegenerate && <button type="button" className={s.iconButton} title="重新生成这条回复" aria-label="重新生成这条回复" onClick={actions.onRegenerate}><RegenerateIcon /></button>}
          </div>
        )}
      </div>
    </article>
  );
}, (previous, next) => previous.slug === next.slug && previous.setReasoningOpen === next.setReasoningOpen && previous.message === next.message
  && previous.reasoningOpen === next.reasoningOpen
  && previous.streaming === next.streaming
  && previous.notice === next.notice
  && previous.sourceNames === next.sourceNames
  && previous.actions?.copied === next.actions?.copied
  && previous.actions?.canRegenerate === next.actions?.canRegenerate
  && previous.actions?.onCopy === next.actions?.onCopy
  && previous.actions?.onDownload === next.actions?.onDownload
  && previous.actions?.onRegenerate === next.actions?.onRegenerate);

export function messageMarkdown(message: ConversationMessage) {
  return (message.blocks ?? [])
    .filter((block) => block.type === 'markdown')
    .map((block) => block.source ?? '')
    .join('\n\n')
    .trim();
}

function ReasoningBlock({ messageID, source, streaming, open, onOpenChange }: { messageID: string; source: string; streaming: boolean; open: boolean; onOpenChange: (messageID: string, open: boolean) => void }) {
  const panelID = useId();
  return <div className={s.reasoning}>
    <button type="button" className={s.reasoningToggle} aria-expanded={open} aria-controls={panelID} onClick={() => onOpenChange(messageID, !open)}>
      <span aria-hidden="true">{open ? '▾' : '▸'}</span>思考过程
    </button>
    <div id={panelID} role="region" aria-label="思考内容" hidden={!open} className={s.reasoningText} aria-busy={streaming}>{source}</div>
  </div>;
}
