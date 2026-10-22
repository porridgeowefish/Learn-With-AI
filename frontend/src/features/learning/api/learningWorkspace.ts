import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useMemo } from 'react';

import { ApiError, http } from '@/shared/client';

export interface MessageBlock {
  id: string;
  type: 'markdown' | 'reasoning-summary' | 'attachment' | 'chat-image' | 'chat-audio';
  name?: string;
  source?: string;
  artifactRef?: string;
}

export interface ConversationMessage {
  operationId?: string;
  id: string;
  role: 'learner' | 'teacher' | 'system';
  status: 'completed' | 'failed' | 'interrupted';
  blocks: MessageBlock[];
  createdAt: string;
  sourceCitations?: SourceCitation[];
  readingCoverage?: Array<{ sourceId: string; revisionId: string; contentHash: string; scope: string; completeWithinScope: boolean }>;
}

export interface SourceCitation {
  evidenceId: string;
  sourceId: string;
  revisionId: string;
  contentHash: string;
  startByte: number;
  endByte: number;
  startLine: number;
  endLine: number;
  displayRange: string;
  quoteHash: string;
}

export interface ConversationProjection {
  conversationId: string;
  unitId: string;
  latestSeq: number;
  pageFromSeq: number;
  pageThroughSeq: number;
  hasMore: boolean;
  hasPrevious: boolean;
  totalMessages: number;
  messages: ConversationMessage[];
  queue?: QueuedTurn[];
  latestResponseId?: string;
}

export interface QueuedTurn {
  queueId: string;
  content: string;
  attachmentRefs?: string[];
  mediaBlocks?: MessageBlock[];
  queuedAt: string;
}

export interface AssetMeta {
  assetId: string;
  key: 'review' | 'practice';
  title: string;
  currentVersionId: string;
  editRevision: number;
  conversationCursor: number;
  updatedAt: string;
}

export interface LearningAsset {
  meta: AssetMeta;
  content: string;
  contentKind: 'markdown';
}

export interface PracticeRound { versionId:string; content:string; createdAt:string }
export function usePracticeRounds(slug:string,enabled=true){
 return useQuery({queryKey:['learning-workspace',slug,'assets','practice','rounds'],queryFn:async()=>(await http.get<{rounds:PracticeRound[]}>(`/api/projects/${encodeURIComponent(slug)}/assets/practice/rounds`)).rounds,enabled:enabled&&Boolean(slug)});
}

export interface BodyAnnotationMessage {
  id: string;
  role: 'learner' | 'assistant';
  content: string;
  status: 'completed' | 'interrupted' | 'failed';
  createdAt: string;
}

export interface BodyAnnotation {
  schemaVersion: number;
  annotationId: string;
  assetId: string;
  assetVersionId: string;
  quoteSnapshot: string;
  anchors: { start: number; end: number; prefix?: string; suffix?: string };
  note?: string;
  status: 'open' | 'asked' | 'resolved';
  detached?: boolean;
  createdAt: string;
  updatedAt: string;
  ask?: { messages: BodyAnnotationMessage[]; summary?: string; summaryState?: string };
}

export interface LearningSource {
  sourceId: string;
  displayName: string;
  status: 'stored' | 'processing' | 'ready' | 'opaque' | 'failed' | 'tombstoned' | 'deleted';
  currentRevisionId: string;
  derivedVersionId?: string;
  coverage?: { complete?: boolean; gaps?: string[]; extractedPages?: number; totalPages?: number; unreadPages?: number };
  failureCode?: string;
  parser?: string;
  extractedBytes?: number;
  extractionTruncated?: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface SourceFileRef {
  key?: string;
  path: string;
  filename?: string;
  mediaType: string;
  bytes: number;
  sha256: string;
}

export interface SourceRevision {
  revisionId: string;
  sourceId: string;
  original: SourceFileRef;
  derivedFiles: SourceFileRef[];
  parseTaskId?: string;
  createdAt: string;
}

export type TeacherStreamFrame =
  | { type: 'turn-accepted'; data: { learnerMessageId: string; responseId: string } }
  | { type: 'message-started'; data: { teacherMessageId: string } }
  | { type: 'message-reset'; data: { messageId: string; code: string } }
  | { type: 'text-delta' | 'reasoning-summary-delta'; data: { blockId: string; delta: string } }
  | { type: 'search-started'; data: { query: string } }
  | { type: 'search-completed'; data: { query: string; count: number } }
  | { type: 'search-failed'; data: { query: string; code: string } }
  | { type: 'message-completed'; data: { messageId: string; latestSeq: number } }
  | { type: 'message-failed'; data: { messageId?: string; code: string; partialPreserved: boolean } }
  | { type: string; data: Record<string, unknown> };

export const learningWorkspaceKeys = {
  conversation: (slug: string) => ['learning-workspace', slug, 'conversation'] as const,
  assets: (slug: string) => ['learning-workspace', slug, 'assets'] as const,
  asset: (slug: string, key: string) => ['learning-workspace', slug, 'assets', key] as const,
  annotations: (slug: string) => ['learning-workspace', slug, 'annotations'] as const,
  sources: (slug: string) => ['learning-workspace', slug, 'sources'] as const,
  source: (slug: string, sourceId: string) => ['learning-workspace', slug, 'sources', sourceId] as const,
};

const keys = learningWorkspaceKeys;

export function useConversation(slug: string) {
  const query = useInfiniteQuery({
    queryKey: keys.conversation(slug),
    queryFn: ({ pageParam }) => http.get<ConversationProjection>(`/api/projects/${encodeURIComponent(slug)}/conversation?limit=40&beforeSeq=${pageParam}`),
    initialPageParam: 0,
    getNextPageParam: (page) => page.hasPrevious && page.pageFromSeq > 0 ? page.pageFromSeq : undefined,
    enabled: Boolean(slug),
    refetchInterval: (query) => query.state.data?.pages[0]?.queue?.length ? 1500 : false,
  });
  const data = useMemo(() => {
    const pages = query.data?.pages;
    if (!pages?.length) return undefined;
    const newest = pages[0];
    const messages = new Map<string, ConversationMessage>();
    for (const page of [...pages].reverse()) {
      for (const message of page.messages ?? []) messages.set(message.id, { ...message, blocks: message.blocks ?? [] });
    }
    return {
      ...newest,
      pageFromSeq: pages[pages.length - 1].pageFromSeq,
      hasMore: Boolean(query.hasNextPage),
      hasPrevious: Boolean(query.hasNextPage),
      messages: [...messages.values()],
    } satisfies ConversationProjection;
  }, [query.data?.pages, query.hasNextPage]);
  return {
    ...query,
    data,
    hasPrevious: Boolean(query.hasNextPage),
    loadPrevious: query.fetchNextPage,
    isLoadingPrevious: query.isFetchingNextPage,
  };
}

export function useAssets(slug: string) {
  return useQuery({ queryKey: keys.assets(slug), queryFn: async () => (await http.get<{ assets: AssetMeta[] }>(`/api/projects/${encodeURIComponent(slug)}/assets`)).assets, enabled: Boolean(slug) });
}

export function useAsset(slug: string, key: string) {
  return useQuery({ queryKey: keys.asset(slug, key), queryFn: () => http.get<LearningAsset>(`/api/projects/${encodeURIComponent(slug)}/assets/${key}`), enabled: Boolean(slug && key) });
}

export function useSaveAsset(slug: string, key: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (input: { baseEditRevision: number; content: string }) => http.put<LearningAsset>(`/api/projects/${encodeURIComponent(slug)}/assets/${key}`, input),
    onSuccess: (asset) => {
      client.setQueryData(keys.asset(slug, key), asset);
      void client.invalidateQueries({ queryKey: keys.assets(slug) });
    },
  });
}

export function useBodyAnnotations(slug: string) {
  return useQuery({
    queryKey: keys.annotations(slug),
    queryFn: async () => (await http.get<{ annotations: BodyAnnotation[] }>(`/api/projects/${encodeURIComponent(slug)}/assets/review/annotations`)).annotations,
    enabled: Boolean(slug),
  });
}

export function useCreateBodyAnnotation(slug: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (input: { assetVersionId: string; quoteSnapshot: string; anchors: BodyAnnotation['anchors']; note?: string }) =>
      http.post<{ annotation: BodyAnnotation }>(`/api/projects/${encodeURIComponent(slug)}/assets/review/annotations`, input),
    onSuccess: () => void client.invalidateQueries({ queryKey: keys.annotations(slug) }),
  });
}

export function useDeleteBodyAnnotation(slug: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (annotationId: string) => http.delete(`/api/projects/${encodeURIComponent(slug)}/assets/review/annotations/${encodeURIComponent(annotationId)}`),
    onSuccess: () => void client.invalidateQueries({ queryKey: keys.annotations(slug) }),
  });
}

export function useSources(slug: string) {
  return useQuery({ queryKey: keys.sources(slug), queryFn: async () => (await http.get<{ sources: LearningSource[] }>(`/api/projects/${encodeURIComponent(slug)}/sources`)).sources, enabled: Boolean(slug), refetchInterval: (query) => query.state.data?.some((source) => source.status === 'processing'||source.status==='stored') ? 2500 : false });
}

export function useSource(slug: string, sourceId: string) {
  return useQuery({
    queryKey: keys.source(slug, sourceId),
    queryFn: () => http.get<{ source: LearningSource; revision: SourceRevision }>(`/api/projects/${encodeURIComponent(slug)}/sources/${encodeURIComponent(sourceId)}`),
    enabled: Boolean(slug && sourceId),
  });
}

export function sourceFileURL(slug: string, sourceId: string, revisionId: string, fileKey: string) {
  return `/api/projects/${encodeURIComponent(slug)}/sources/${encodeURIComponent(sourceId)}/revisions/${encodeURIComponent(revisionId)}/files/${encodeURIComponent(fileKey)}`;
}

export const MAX_SOURCE_FILE_BYTES = 100 * 1024 * 1024;

export function useUploadSource(slug: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: async ({ file }: { file: File }) => {
      if (file.size > MAX_SOURCE_FILE_BYTES) throw new Error('单文件不能超过 100MiB');
      const form = new FormData();
      form.set('operationId', crypto.randomUUID());
      form.set('displayName', file.name);
      form.set('file', file);
      const response = await fetch(`/api/projects/${encodeURIComponent(slug)}/sources`, { method: 'POST', body: form });
      const payload = await response.json().catch(() => null);
      if (!response.ok) throw new ApiError(response.status, response.status === 413 ? '单文件不能超过 50MB' : '资料上传失败', payload);
      return payload as { source: LearningSource; revision: SourceRevision; parseDisposition: string; parseReason: string };
    },
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.sources(slug) });
    },
  });
}

export function useTombstoneSource(slug: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (sourceId: string) => http.delete(`/api/projects/${encodeURIComponent(slug)}/sources/${encodeURIComponent(sourceId)}`),
    onSuccess: () => void client.invalidateQueries({ queryKey: keys.sources(slug) }),
  });
}

export function useRetrySource(slug: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (sourceId: string) => http.post<LearningSource>(`/api/projects/${encodeURIComponent(slug)}/sources/${encodeURIComponent(sourceId)}/retry`, {}),
    onSuccess: () => void client.invalidateQueries({ queryKey: keys.sources(slug) }),
  });
}

export function usePermanentlyDeleteSource(slug: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (sourceId: string) => http.post(`/api/projects/${encodeURIComponent(slug)}/sources/${encodeURIComponent(sourceId)}/permanent-delete`, { operationId: crypto.randomUUID(), confirmation: 'PERMANENT_DELETE' }),
    onSuccess: () => void client.invalidateQueries({ queryKey: keys.sources(slug) }),
  });
}

export async function streamTeacherTurn(slug: string, input: { operationId: string; content: string; attachmentRefs?: string[]; conversationAttachmentIds?: string[]; providerId?: string }, signal: AbortSignal, onFrame: (frame: TeacherStreamFrame) => void) {
  const response = await fetch(`/api/projects/${encodeURIComponent(slug)}/conversation/turns`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input), signal });
  // A server-owned queued run may begin before the client learns about it.
  // The rejected direct request was not recorded. Preserve its operation ID
  // in the durable queue once; never retry the provider request blindly.
  if (response.status === 409) {
    const queued = await http.post<{ queueId: string }>(`/api/projects/${encodeURIComponent(slug)}/conversation/turns/queue`, input, { signal });
    onFrame({ type: 'turn-queued', data: { queueId: queued.queueId } });
    await resumeTeacherTurn(slug, signal, onFrame);
    return;
  }
  if (!response.ok || !response.body) throw new ApiError(response.status, `教师响应无法开始（HTTP ${response.status}），请重试`, await response.text());
  await consumeSSE(response, onFrame);
}

// Reconnects to a teacher response that is still running in the backend. The
// backend owns the generation; this request is only a disposable subscriber.
export async function resumeTeacherTurn(slug: string, signal: AbortSignal, onFrame: (frame: TeacherStreamFrame) => void) {
  const response = await fetch(`/api/projects/${encodeURIComponent(slug)}/conversation/responses/active`, { signal });
  if (response.status === 204) return false;
  if (!response.ok || !response.body) throw new ApiError(response.status, '教师响应无法恢复', await response.text());
  await consumeSSE(response, onFrame);
  return true;
}

export async function startTeacherOpening(slug: string, signal: AbortSignal, onFrame: (frame: TeacherStreamFrame) => void) {
  const response = await fetch(`/api/projects/${encodeURIComponent(slug)}/conversation/opening`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}', signal });
  if (response.status === 204) return false;
  if (!response.ok || !response.body) throw new ApiError(response.status, '教师开场无法开始', await response.text());
  await consumeSSE(response, onFrame);
  return true;
}

export async function stopTeacherResponse(slug: string, responseId: string) {
  return http.post(`/api/projects/${encodeURIComponent(slug)}/conversation/responses/${encodeURIComponent(responseId)}/stop`, { operationId: crypto.randomUUID() });
}

// Queued turns: durable server-side waiting line for messages sent while the
// teacher is streaming. promoted=true means no response was active and the
// item already became a normal turn (reattach via resumeTeacherTurn).
export async function queueTeacherTurn(slug: string, input: { operationId: string; content: string; attachmentRefs?: string[]; conversationAttachmentIds?: string[]; providerId?: string }) {
  return http.post<{ queueId: string; promoted: boolean }>(`/api/projects/${encodeURIComponent(slug)}/conversation/turns/queue`, input);
}

export async function editQueuedTurn(slug: string, queueId: string, input: { content: string; attachmentRefs?: string[] }) {
  return http.post<{ queueId: string }>(`/api/projects/${encodeURIComponent(slug)}/conversation/queue/${encodeURIComponent(queueId)}/edit`, input);
}

export async function discardQueuedTurn(slug: string, queueId: string) {
  return http.post<{ queueId: string; discarded: boolean }>(`/api/projects/${encodeURIComponent(slug)}/conversation/queue/${encodeURIComponent(queueId)}/discard`, {});
}

// Steaming an immediate steer: interrupts the active response server-side and
// streams the follow-up turn over this connection.
export async function steerQueuedTurn(slug: string, queueId: string, signal: AbortSignal, onFrame: (frame: TeacherStreamFrame) => void) {
  const response = await fetch(`/api/projects/${encodeURIComponent(slug)}/conversation/queue/${encodeURIComponent(queueId)}/steer`, { method: 'POST', signal });
  if (response.status === 204) return false;
  if (!response.ok || !response.body) throw new ApiError(response.status, '引导未能开始', await response.text());
  await consumeSSE(response, onFrame);
  return true;
}

export async function regenerateTeacherResponse(slug: string, responseId: string, signal: AbortSignal, onFrame: (frame: TeacherStreamFrame) => void) {
  const response = await fetch(`/api/projects/${encodeURIComponent(slug)}/conversation/responses/${encodeURIComponent(responseId)}/regenerate`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' , signal });
  if (!response.ok || !response.body) throw new ApiError(response.status, '重新生成未能开始', await response.text());
  await consumeSSE(response, onFrame);
}

async function consumeSSE(response: Response, onFrame: (frame: TeacherStreamFrame) => void) {
  const reader = response.body!.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  while (true) {
    const { value, done } = await reader.read();
    buffer += decoder.decode(value, { stream: !done });
    const chunks = buffer.split(/\r?\n\r?\n/);
    buffer = chunks.pop() ?? '';
    for (const chunk of chunks) {
      let type = '';
      let data = '';
      for (const line of chunk.split(/\r?\n/)) {
        if (line.startsWith('event:')) type = line.slice(6).trim();
        if (line.startsWith('data:')) data += line.slice(5).trim();
      }
      if (type && data) onFrame({ type, data: JSON.parse(data) } as TeacherStreamFrame);
    }
    if (done) break;
  }
}

export function conversationExportURL(slug: string) {
  return `/api/projects/${encodeURIComponent(slug)}/conversation/export.md`;
}

export function invalidateLearningWorkspace(client: ReturnType<typeof useQueryClient>, slug: string) {
  void client.invalidateQueries({ queryKey: ['learning-workspace', slug] });
}
