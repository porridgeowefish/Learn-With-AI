import { useCallback, useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import {
  learningWorkspaceKeys, useConversation, streamTeacherTurn, resumeTeacherTurn, startTeacherOpening,
  queueTeacherTurn, stopTeacherResponse, steerQueuedTurn, regenerateTeacherResponse,
  editQueuedTurn, discardQueuedTurn, resumeTeacherQueue,
  type ConversationMessage, type QueuedTurn, type TeacherStreamFrame,
} from '../api/learningWorkspace';
import type { ChatAttachment } from '../components/ChatMediaInput';
import { emptyLive, initialSessionState, placeholderLearner, teacherSessionReducer, type LiveResponse, type SessionAction } from './sessionState';

export interface TeacherSubmission {
  operationId: string;
  content: string;
  attachments: ChatAttachment[];
  providerId?: string;
}

export function useTeacherSession(slug: string) {
  const client = useQueryClient();
  const conversation = useConversation(slug);
  const projection = useRef(conversation.data);
  projection.current = conversation.data;
  const [state, setState] = useState(initialSessionState);
  const current = useRef(initialSessionState);
  const controller = useRef<AbortController | null>(null);
  const frames = useRef<TeacherStreamFrame[]>([]);
  const timer = useRef<ReturnType<typeof setTimeout>>();
  const checkedProjection = useRef(conversation.data);
  const [probe, setProbe] = useState(0);
  const generation = useRef(0);

  const commit = useCallback((action: SessionAction) => {
    current.current = teacherSessionReducer(current.current, action);
    setState(current.current);
  }, []);
  const flush = useCallback(() => {
    clearTimeout(timer.current); timer.current = undefined;
    const pending = frames.current; frames.current = [];
    for (const frame of pending) {
      const learner = frame.type === 'turn-accepted'
        ? projection.current?.messages.find(message => message.id === frame.data.learnerMessageId) : undefined;
      commit({ type: 'frame', frame, learner });
    }
  }, [commit]);
  const detach = useCallback(() => {
    controller.current?.abort(); controller.current = null;
    clearTimeout(timer.current); timer.current = undefined; frames.current = [];
  }, []);
  const reportError = useCallback((error: string) => commit({ type: 'error', error }), [commit]);
  const refresh = useCallback(() => client.resetQueries({ queryKey: learningWorkspaceKeys.conversation(slug) }), [client, slug]);

  // Every connection origin has the same ownership, buffering and reconciliation.
  const run = useCallback(async (
    request: (signal: AbortSignal, emit: (frame: TeacherStreamFrame) => void) => Promise<unknown>,
    live?: LiveResponse, reconnect = false, onAccepted?: () => void,
  ) => {
    detach();
    const next = new AbortController(); controller.current = next;
    const token = generation.current;
    const owns = () => controller.current === next && !next.signal.aborted && generation.current === token;
    const hadLive = Boolean(current.current.live);
    if (live || !reconnect || current.current.live) commit({ type: 'start', live, reconnect });
    let received = false, accepted = false;
    try {
      await request(next.signal, frame => {
        if (!owns()) return;
        received = true;
        if (frame.type === 'text-delta' || frame.type === 'reasoning-summary-delta') {
          frames.current.push(frame);
          timer.current ??= setTimeout(() => { if (owns()) flush(); }, 64);
        } else {
          flush();
          frames.current.push(frame); flush();
        }
        if (!accepted && (frame.type === 'turn-accepted' || frame.type === 'turn-queued')) {
          accepted = true; onAccepted?.();
        }
      });
      if (!owns()) return accepted;
      flush();
      if (received || hadLive) { commit({ type: 'reconcile' }); await refresh(); }
      if (owns() && (received || !reconnect || current.current.live)) commit({ type: 'settle' });
      return true;
    } catch (cause) {
      if (!owns()) return accepted;
      flush();
      // Keep the partial response and its cursor for a read-only retry.
      commit({ type: 'settle', error: (cause as Error).message || '教师暂时无法响应', preserve: accepted || reconnect });
      await refresh().catch(() => undefined);
      return accepted;
    } finally {
      if (owns()) {
        controller.current = null;
        if ((received || hadLive) && current.current.phase !== 'failed') {
          checkedProjection.current = undefined;
          setProbe(value => value + 1);
        }
      }
    }
  }, [commit, detach, flush, refresh]);

  useEffect(() => {
    checkedProjection.current = undefined;
    current.current = initialSessionState; setState(initialSessionState);
    return () => { generation.current++; detach(); };
  }, [slug, detach]);

  useEffect(() => {
    if (!conversation.data || controller.current || current.current.live || current.current.phase === 'failed' || checkedProjection.current === conversation.data) return;
    checkedProjection.current = conversation.data;
    const opening = conversation.data.messages.every(message => message.operationId === 'teacher-welcome-v1');
    void run((signal, emit) => (opening ? startTeacherOpening : resumeTeacherTurn)(slug, signal, emit), undefined, true);
  }, [conversation.data, probe, run, slug]);

  const send = async (input: TeacherSubmission, onAccepted: () => void) => {
    let accepted = false;
    const token = generation.current;
    const accept = () => { if (!accepted) { accepted = true; onAccepted(); } };
    const attachments = input.attachments;
    const data = {
      operationId: input.operationId, content: input.content, providerId: input.providerId,
      attachmentRefs: attachments.filter(a => a.kind === 'source').map(a => a.attachmentId),
      conversationAttachmentIds: attachments.filter(a => a.kind !== 'source').map(a => a.attachmentId),
    };
    try {
      reportError('');
      if (current.current.live || projection.current?.queuePaused) {
        await queueTeacherTurn(slug, data);
        if (token !== generation.current) return false;
        accept();
        await client.invalidateQueries({ queryKey: learningWorkspaceKeys.conversation(slug) });
        return true;
      }
      const learner: ConversationMessage = {
        ...placeholderLearner(`local-${input.operationId}`), operationId: input.operationId,
        blocks: [{ id: 'local', type: 'markdown', source: input.content }, ...attachments.map(a => ({
          id: a.attachmentId, type: a.kind === 'source' ? 'attachment' as const : a.kind === 'image' ? 'chat-image' as const : 'chat-audio' as const,
          artifactRef: a.attachmentId, source: a.text, name: a.name,
        }))],
      };
      return await run((signal, emit) => streamTeacherTurn(slug, data, signal, emit), emptyLive(learner), false, accept);
    } catch (cause) {
      if (token === generation.current) reportError((cause as Error).message || '排队失败，请稍后重试');
      return accepted;
    }
  };
  const stop = async () => {
    const id = current.current.live?.responseId;
    if (!id) return;
    try {
      await stopTeacherResponse(slug, id);
      // The stream owns terminal persistence and refresh, including cancellation.
      await refresh();
    } catch (cause) { reportError((cause as Error).message || '停止失败'); }
  };
  const steer = (item: QueuedTurn) => run((signal, emit) => steerQueuedTurn(slug, item.queueId, signal, emit), emptyLive({
    ...placeholderLearner(`local-steer-${item.queueId}`), blocks: [{ id: 'local', type: 'markdown', source: item.content }, ...(item.mediaBlocks ?? [])],
  }));
  const regenerate = () => {
    const id = projection.current?.latestResponseId;
    if (!id) return;
    const learner = [...(projection.current?.messages ?? [])].reverse().find(message => message.role === 'learner') ?? placeholderLearner('teacher-opening');
    return run((signal, emit) => regenerateTeacherResponse(slug, id, signal, emit), emptyLive(learner));
  };
  const queueCommand = async (command: () => Promise<unknown>) => {
    const token = generation.current;
    try { await command(); if (token !== generation.current) return false; await refresh(); return true; }
    catch (cause) { if (token === generation.current) reportError((cause as Error).message || '队列操作失败'); return false; }
  };
  const reconnect = () => run((signal, emit) => resumeTeacherTurn(slug, signal, emit, current.current.cursor), undefined, true);
  return {
    conversation, ...state, send, stop, steer, regenerate, reportError, reconnect,
    edit: (id: string, content: string) => queueCommand(() => editQueuedTurn(slug, id, { content })),
    discard: (id: string) => queueCommand(() => discardQueuedTurn(slug, id)),
    resume: () => queueCommand(() => resumeTeacherQueue(slug)),
  };
}
export type TeacherSession = ReturnType<typeof useTeacherSession>;
