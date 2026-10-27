import type { ConversationMessage, TeacherStreamFrame } from '../api/learningWorkspace';

interface SearchStatus {
  query: string;
  count?: number;
  failed?: boolean;
}

export interface LiveResponse {
  learner: ConversationMessage;
  teacherId?: string;
  responseId?: string;
  text: string;
  reasoning: string;
  searches: SearchStatus[];
  notice?: string;
  reading?: { operationId: string; operation: string; sourceId?: string; displayRange?: string };
}


export interface TeacherSessionState {
  live: LiveResponse | null;
  phase: 'idle' | 'submitting' | 'streaming' | 'reconciling' | 'failed';
  connection: 'disconnected' | 'connecting' | 'connected';
  cursor?: { responseId: string; frameSeq: number };
  error: string;
}
export const initialSessionState: TeacherSessionState = { live: null, phase: 'idle', connection: 'disconnected', error: '' };
export type SessionAction =
  | { type: 'start'; live?: LiveResponse; reconnect?: boolean }
  | { type: 'frame'; frame: TeacherStreamFrame; learner?: ConversationMessage }
  | { type: 'reconcile' }
  | { type: 'settle'; error?: string; preserve?: boolean }
  | { type: 'error'; error: string };

export function teacherSessionReducer(state: TeacherSessionState, action: SessionAction): TeacherSessionState {
  if (action.type === 'error') return { ...state, error: action.error };
  if (action.type === 'start') return { ...initialSessionState, ...(action.reconnect ? state : {}), live: action.live ?? (action.reconnect ? state.live : null), connection: 'connecting', phase: action.reconnect ? state.phase : 'submitting', error: '' };
  if (action.type === 'reconcile') return { ...state, phase: 'reconciling' };
  if (action.type === 'settle') return { ...state, live: action.preserve ? state.live : null, connection: 'disconnected', phase: action.error ? 'failed' : 'idle', error: action.error ?? '', cursor: action.preserve ? state.cursor : undefined };
  const frame = action.frame;
  if (frame.type === 'turn-queued') return { ...state, live: null, phase: 'reconciling' };
  const data = frame.data as Record<string, unknown>;
  const responseId = frame.responseId ?? (typeof data.responseId === 'string' ? data.responseId : undefined);
  const frameSeq = frame.frameSeq ?? (typeof data.frameSeq === 'number' ? data.frameSeq : undefined);
  // A new response can only take ownership through its acceptance frame.
  if (responseId && state.live?.responseId && state.live.responseId !== responseId && frame.type !== 'turn-accepted') return state;
  if (responseId && frameSeq && state.cursor && state.cursor.responseId === responseId && frameSeq <= state.cursor.frameSeq) return state;
  let live = state.live;
  if (frame.type === 'turn-accepted') {
    if (!live || (live.responseId && live.responseId !== responseId)) live = emptyLive(action.learner ?? placeholderLearner(String(frame.data.learnerMessageId)));
    if (action.learner) live = { ...live, learner: action.learner };
  }
  return { ...state, live: reduceLive(live, frame), phase: frame.type === 'message-completed' || frame.type === 'message-failed' ? 'reconciling' : 'streaming', connection: 'connected', cursor: responseId && frameSeq ? { responseId, frameSeq } : state.cursor };
}

export function emptyLive(learner: ConversationMessage): LiveResponse {
  return { learner, text: '', reasoning: '', searches: [] };
}
export function placeholderLearner(id: string): ConversationMessage {
  return { id, role: 'learner', status: 'completed', blocks: [], createdAt: '' };
}
function reduceLive(current: LiveResponse | null, frame: TeacherStreamFrame): LiveResponse | null {
    if (!current) return current;
    if (frame.type === 'turn-accepted') return { ...current, responseId: String(frame.data.responseId), learner: { ...current.learner, id: String(frame.data.learnerMessageId) } };
    if (frame.type === 'message-started') return { ...current, teacherId: String(frame.data.teacherMessageId) };
    if (frame.type === 'message-reset') return { ...current, text: '', reasoning: '', notice: '', searches: [], reading: undefined };
    if (frame.type === 'message-completed' || frame.type === 'message-failed') return { ...current, reading: undefined };
    if (frame.type === 'source-reading') {
      const operationId = String(frame.data.operationId ?? '');
      if (frame.data.phase === 'started') return { ...current, reading: { operationId, operation: String(frame.data.operation ?? ''), sourceId: typeof frame.data.sourceId === 'string' ? frame.data.sourceId : undefined, displayRange: typeof frame.data.displayRange === 'string' ? frame.data.displayRange : undefined } };
      return current.reading?.operationId === operationId ? { ...current, reading: undefined } : current;
    }
    if (frame.type === 'text-delta') return { ...current, text: current.text + String(frame.data.delta ?? '') };
    if (frame.type === 'reasoning-summary-delta') return { ...current, reasoning: current.reasoning + String(frame.data.delta ?? '') };
    if (frame.type === 'search-started') return { ...current, searches: [...current.searches, { query: String(frame.data.query ?? '') }] };
    if (frame.type === 'search-completed' || frame.type === 'search-failed') {
      return { ...current, searches: current.searches.map((search, index) => index === current.searches.length - 1 && !search.count && !search.failed
        ? (frame.type === 'search-completed' ? { ...search, count: Number(frame.data.count ?? 0) } : { ...search, failed: true })
        : search) };
    }
    return current;
}

// Durable messages win at the same position as soon as they exist.
export function projectTeacherMessages(messages: ConversationMessage[], live: LiveResponse | null) {
  const visible = messages.filter(message => message.operationId !== 'teacher-welcome-v1');
  if (!live) return visible;
  const ids = new Set(visible.map(message => message.id));
  const learnerAccepted = live.learner.operationId && visible.some(message => message.operationId === live.learner.operationId);
  if (live.learner.id !== 'teacher-opening' && !ids.has(live.learner.id) && !learnerAccepted) visible.push(live.learner);
  const id = live.teacherId ?? 'streaming-teacher';
  if (!ids.has(id)) visible.push({ id, role: 'teacher', status: 'completed', createdAt: '', blocks: [
    ...(live.reasoning ? [{ id: 'reasoning', type: 'reasoning-summary' as const, source: live.reasoning }] : []),
    ...(live.text ? [{ id: 'text', type: 'markdown' as const, source: live.text }] : []),
  ] });
  return visible;
}
