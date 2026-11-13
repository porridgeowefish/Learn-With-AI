import { LearnWriting } from '@/shared/primitive/LearnWriting';
import { useLearningProfile } from '@/features/settings';
import { memo, useId, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import type { ConversationMessage } from '../api/learningWorkspace';
import { CopyIcon, DownloadIcon, RegenerateIcon } from '@/shared/primitive/Icons';
import { MarkdownView } from '@/shared/primitive/MarkdownView';
import { SourceCitations } from './SourceCitations';
import { ChatMediaBlock } from './ChatMediaInput';
import { teacherMarkdown } from './LearningProgress';
import { AnswerComparison, AnswerFeedback } from './AnswerFeedback';
import s from './TeacherView.module.css';


const credits = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 3 });
const teacherFailureMessages: Record<string, string> = {
  comparison_unavailable: '对比回答生成失败，可以重新生成或继续提问。',
  login_required: '使用系统模型需要登录 Learn-With-AI。',
  insufficient_credits: '可用积分不足以发送这一轮。可以领取今日积分、缩短本轮内容，或改用自己的模型连接。',
  daily_credit_limit_reached: '已达到今日系统模型积分限额。可以明天继续，或改用自己的模型连接。',
  model_busy: '系统模型当前繁忙。请稍后重试，或换一个模型。',
  cloud_budget_exhausted: '系统模型暂时暂停推理。可以改用自己的模型连接继续。',
  model_unavailable: '这个系统模型当前不可用。请在输入框下方选一个可用模型。',
  model_tools_unsupported: '这个模型无法执行本轮所需的工具。请换一个支持工具调用的模型，或关闭联网搜索后重试。',
  credits_unavailable: '暂时无法检查积分。请稍后重试，或改用自己的模型连接。',
  settlement_pending: '本轮积分尚未完成结算，可以在个人信息中查看余额。',
  inference_interrupted: '模型在生成过程中断开。已收到的正文仍会显示，可以重新生成。',
  context_preparation_failed: '无法读取本轮教学设置，请确认偏好和附加信息已保存后重试。',
  context_budget_exhausted: '这轮内容超过了模型的输入上限。可以缩短附加信息，或在新对话中继续。',
  teacher_request_timeout: '这轮请求超时，请重新生成。',
  teacher_provider_rate_limited: '模型服务当前限流，请稍后重试。',
  teacher_provider_not_configured: '当前没有可用的模型连接。请选择系统模型，或添加自己的 API。',
  teacher_api_key_rejected: '模型服务拒绝了 API Key。请检查密钥是否有效，再测试连接。',
  teacher_provider_forbidden: '当前连接无权使用该模型。请检查服务商的模型权限，或换一个模型。',
  teacher_provider_quota: '模型服务商拒绝计费。请检查自配 API 的余额或套餐额度。',
  teacher_model_not_found: '模型服务找不到所选模型或接口。请检查模型名称和 Base URL。',
  teacher_network_error: '无法连到模型服务。请检查网络、代理和模型地址，或换一个模型。',
  teacher_request_rejected: '模型拒绝了本轮请求。可以减少附件、关闭联网搜索，或换一个模型后重试。',
  teacher_cloud_unavailable: '系统模型暂时无法响应。请换一个模型或稍后重试。',
  teacher_provider_unavailable: '模型服务未能返回完整回复。请重新生成；如果反复失败，请测试模型连接或切换模型。',
  chatgpt_connection_failed: 'ChatGPT 授权连接失败。请到设置的「模型连接」检查 ChatGPT 连接后重试。',
  chatgpt_request_rejected: 'ChatGPT 拒绝了这轮请求。请重新生成，或换一个可用模型继续。',
  chatgpt_model_unavailable: '当前 ChatGPT 模型不可用，请在模型设置里重新选择。',
};

const modelSettingsErrors = new Set([
  'teacher_provider_not_configured', 'teacher_api_key_rejected', 'teacher_provider_forbidden', 'teacher_provider_quota',
  'teacher_model_not_found', 'teacher_network_error', 'teacher_provider_unavailable', 'cloud_budget_exhausted',
  'chatgpt_connection_failed', 'chatgpt_model_unavailable',
]);

function TeacherFailure({ code, retry }: { code?: string; retry?: () => void }) {
  const { pathname } = useLocation();
  const key = code ?? 'teacher_provider_unavailable';
  const account = ['insufficient_credits', 'daily_credit_limit_reached', 'credits_unavailable', 'settlement_pending'].includes(key);
  return <div className={s.notice} role="alert">
    {teacherFailureMessages[key] ?? teacherFailureMessages.teacher_provider_unavailable}
    <div className={s.failureActions}>
      {key === 'login_required' && <Link to="/login" state={{ next: pathname }}>去登录</Link>}
      {(key === 'login_required' || modelSettingsErrors.has(key)) && <Link to="/settings?section=models">打开模型连接</Link>}
      {account && <Link to="/settings?section=account">查看积分</Link>}
      {retry && !account && key !== 'login_required' && key !== 'teacher_provider_not_configured' &&
        <button type="button" onClick={retry}>重新生成</button>}
    </div>
  </div>;
}

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
  const hasAnswer = (message.blocks ?? []).some(block => block.type === 'markdown' && teacherMarkdown(block.source ?? '').trim().length > 0);
  const awaitingAnswer = teacher && streaming && !hasAnswer && !message.comparison && message.status !== 'failed';
  const profile = useLearningProfile();
  const [selectedEvidence, setSelectedEvidence] = useState('');
  const citations = message.sourceCitations ?? [];
  const citationSource = (source: string) => source.replace(/\[\[source:([^\]]+)\]\]/g, (_marker, id: string) => {
    const index = citations.findIndex(item => item.evidenceId === id);
    return index < 0 ? '' : `[资料${index + 1}](#source-${id})`;
  });
  return (
    <article id={`message-${message.id}`} data-tour={teacher ? undefined : "learner-message"} className={teacher ? s.teacherMessage : s.learnerMessage}>
      {teacher && <div className={s.speaker}>教师{awaitingAnswer && <span className={s.writing} role="status" aria-label="教师正在思考或调用工具"><LearnWriting /></span>}</div>}
      {message.comparison && !message.comparison.vote && <AnswerComparison slug={slug} message={{ ...message, comparison: message.comparison }} streaming={streaming} />}
      {awaitingAnswer && <div className={s.thinking}>思考中</div>}
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
        {(message.blocks ?? []).map((block) => block.type === 'reasoning-summary'
          ? (profile.data?.settings.showReasoning !== false && <ReasoningBlock key={block.id} messageID={message.id} source={block.source ?? ''} streaming={streaming} open={reasoningOpen} onOpenChange={setReasoningOpen} />)
          : block.type === 'markdown' ? <MarkdownView key={block.id} source={citationSource(teacher ? teacherMarkdown(block.source ?? '') : block.source ?? '')} className={s.markdown} streaming={streaming} />
            : block.type === 'attachment' ? <span key={block.id} className={s.attachment}>附件 · {sourceNames.get(block.artifactRef ?? '') ?? '已选择附件'}</span>
              : block.type === 'chat-image' || block.type === 'chat-audio' ? <ChatMediaBlock key={block.id} slug={slug} id={block.artifactRef ?? ''} kind={block.type === 'chat-image' ? 'image' : 'audio'} name={block.name} text={block.source} /> : null)}
        {teacher && !streaming && <SourceCitations slug={slug} citations={citations} sourceNames={sourceNames} selected={selectedEvidence} onSelect={setSelectedEvidence} />}
        {teacher && !streaming && message.creditUsage && <p className={s.creditUsage}>
          {message.creditUsage.pending
            ? (message.creditUsage.units ? `已扣 ${credits.format(message.creditUsage.units / 1000)} 积分 · 部分待结算` : '积分待结算')
            : `消耗 ${credits.format(message.creditUsage.units / 1000)} 积分`}
        </p>}
        {notice && <div className={s.notice}>{notice}</div>}
        {teacher && !streaming && message.status === 'failed' && <TeacherFailure code={message.failureCode}
          retry={actions?.canRegenerate ? actions.onRegenerate : undefined} />}
        {teacher && !streaming && hasAnswer && message.status !== 'failed' && <AnswerFeedback slug={slug} message={message} />}
        {actions && !streaming && (!message.comparison || message.comparison.vote) && (
          <div className={s.messageActions}>
            <button type="button" className={s.iconButton} title={actions.copied ? '已复制' : '复制原文'} aria-label={actions.copied ? '已复制' : '复制原文'} onClick={() => void actions.onCopy(message)}><CopyIcon />{actions.copied && <span className={s.copiedMark}>已复制</span>}</button>
            <button type="button" className={s.iconButton} title="下载本条文本" aria-label="下载本条文本" onClick={() => actions.onDownload(message)}><DownloadIcon /></button>
            {actions.canRegenerate && message.status !== 'failed' && <button type="button" className={s.iconButton} title="重新生成这条回复" aria-label="重新生成这条回复" onClick={actions.onRegenerate}><RegenerateIcon /></button>}
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
      <span aria-hidden="true">{open ? '▾' : '▸'}</span>思考摘要
    </button>
    <div id={panelID} role="region" aria-label="思考摘要内容" hidden={!open} className={s.reasoningText} aria-busy={streaming}>{source}</div>
  </div>;
}
