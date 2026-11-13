import { useEffect, useRef, useState } from 'react';

import { useProjectTypeAdvice } from '@/features/projects/api/projects';
import { Button } from '@/shared/primitive/Button';
import { Icon } from '@/shared/primitive/Icon';
import { Modal } from '@/shared/primitive/Modal';
import type { ProjectType } from '@/shared/types/domain';

import s from './ProjectTypeAdvisorDialog.module.css';

interface AdvisorMessage {
  id: number;
  role: 'user' | 'assistant';
  content: string;
}

interface ProjectTypeAdvisorDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  draft: {
    title?: string;
    why?: string;
  };
  onAdopt: (projectType: ProjectType) => void;
}

const WELCOME: AdvisorMessage = {
  id: 1,
  role: 'assistant',
  content: '告诉我你想学什么，以及你现在更需要“先看清全貌”，还是“直接深入掌握一个具体主题”。我会帮你判断，但最终由你选择。',
};

export function ProjectTypeAdvisorDialog({
  open,
  onOpenChange,
  draft,
  onAdopt,
}: ProjectTypeAdvisorDialogProps) {
  const advice = useProjectTypeAdvice();
  const [messages, setMessages] = useState<AdvisorMessage[]>([WELCOME]);
  const [input, setInput] = useState('');
  const [recommendation, setRecommendation] = useState<ProjectType | undefined>();
  const [recommendationReason, setRecommendationReason] = useState('');
  const [recommendationTradeoff, setRecommendationTradeoff] = useState('');
  const composing = useRef(false);
  const conversationId = useRef(0);
  const messageId = useRef(1);
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    composing.current = false;
    messageId.current = 1;
    setMessages([WELCOME]);
    setInput('');
    setRecommendation(undefined);
    setRecommendationReason('');
    setRecommendationTradeoff('');
    advice.reset();
    return () => { conversationId.current++; };
    // Reset-on-open is intentional.
  }, [open]);

  useEffect(() => {
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages, advice.isPending]);

  const send = async () => {
    const content = input.trim();
    if (!content || advice.isPending) return;
    const sendingConversation = conversationId.current;

    const userMessage: AdvisorMessage = {
      id: ++messageId.current,
      role: 'user',
      content,
    };
    const conversation = [...messages, userMessage];
    setMessages(conversation);
    setInput('');
    setRecommendation(undefined);
    setRecommendationReason('');
    setRecommendationTradeoff('');

    try {
      const response = await advice.mutateAsync({
        ...draft,
        messages: conversation.map(({ role, content: messageContent }) => ({
          role,
          content: messageContent,
        })),
      });
      if (sendingConversation !== conversationId.current) return;
      setMessages((current) => [
        ...current,
        {
          id: ++messageId.current,
          role: 'assistant',
          content: response.reply,
        },
      ]);
      setRecommendation(response.recommendation);
      setRecommendationReason(response.reason ?? '');
      setRecommendationTradeoff(response.tradeoff ?? '');
    } catch {
      // The mutation error is rendered beside the composer so failure is never silent.
    }
  };

  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title="问问 AI：我该选哪一种？"
      description="临时对话仅用于本次选择，关闭后即清空，不会写入项目或学习记录。"
      size="md"
      className={s.modal}
    >
      <div className={s.dialog}>
        <div ref={listRef} className={s.messages} aria-live="polite">
          {messages.map((message) => (
            <div
              key={message.id}
              className={message.role === 'user' ? s.userMessage : s.assistantMessage}
            >
              {message.content}
            </div>
          ))}
          {advice.isPending && (
            <div className={s.thinking} role="status" aria-live="polite">
              <div className={s.thinkingCopy}>
                <Icon name="bot" size={15} />
                <span><strong>正在梳理你的学习意图</strong><small>先辨别你需要一张全景地图，还是一条深入路线，请稍等片刻。</small></span>
              </div>
              <div className={s.thinkingTrack} role="progressbar" aria-label="AI 正在生成建议" aria-valuetext="正在分析">
                <i />
              </div>
            </div>
          )}
        </div>

        {recommendation && (
          <div className={s.recommendation}>
            <div>
              <strong>
                建议选择：{recommendation === 'discipline-map' ? '学科地图' : '系统学习'}
              </strong>
              {recommendationReason && <span>{recommendationReason}</span>}
              {recommendationTradeoff && <span>取舍：{recommendationTradeoff}</span>}
            </div>
            <Button
              type="button"
              size="sm"
              variant="primary"
              onClick={() => {
                onAdopt(recommendation);
                onOpenChange(false);
              }}
            >
              采用建议
            </Button>
          </div>
        )}

        <div className={s.composer}>
          <textarea
            value={input}
            rows={2}
            autoFocus
            placeholder="例如：我想学博弈论，但还不知道它有哪些研究方向"
            onChange={(event) => setInput(event.target.value)}
            onCompositionStart={() => { composing.current = true; }}
            onCompositionEnd={() => { composing.current = false; }}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && !event.shiftKey && !composing.current && !event.nativeEvent.isComposing && event.nativeEvent.keyCode !== 229) {
                event.preventDefault();
                void send();
              }
            }}
          />
          <Button
            type="button"
            variant="primary"
            loading={advice.isPending}
            disabled={!input.trim()}
            onClick={() => void send()}
          >
            发送
          </Button>
        </div>
        {advice.error && (
          <div className={s.error} role="alert">
            暂时无法获得建议：{(advice.error as Error).message}
          </div>
        )}
      </div>
    </Modal>
  );
}
