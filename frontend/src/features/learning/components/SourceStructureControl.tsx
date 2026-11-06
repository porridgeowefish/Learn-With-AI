import { useEffect, useRef } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ApiError, http } from '@/shared/client';
import { subscribeToSSE } from '@/app/hooks/useSSE';
import { SSE_EVENTS } from '@/shared/lib/constants';
import type { LearningSource } from '../api/learningWorkspace';
import s from './SourceStructureControl.module.css';

interface StructureJob {
	allPages?: boolean;
	pages?: number[];
	completedPages?: number[];
  id: string;
  revisionId: string;
  status: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled' | 'interrupted';
  providerId: string;
  model?: string;
  errorCode?: string;
  emptyPages?: number[];
}
interface JobList { jobs: StructureJob[]; localOCRAvailable: boolean; configuredOCRAvailable?: boolean; localOCR?: {platform:string; providerId:string; name:string; available:boolean; setupHint?:string} }

const messages: Record<string, string> = {
  'structure-active': '已有识别任务，正在同步状态，请等待完成。',
  'vision-model-unsupported': '当前文档模型不支持图片输入，请使用免费识别或选择视觉模型。',
  'vision-binding-required': '未配置文档识别模型，且系统中文 OCR 不可用。',
  'windows-ocr-unavailable': '系统中文 OCR 不可用，请检查 Windows 简体中文 OCR 语言包。',
  'local-ocr-unavailable': '本地识别引擎不可用，请按下方提示检查安装并刷新状态。',
  'local-ocr-no-text': '已处理所有待识别页，但没有识别到文字，原件已保留。',
  'provider-unavailable': '文档模型连接不可用，请检查其配置。',
  'vision-provider-error': '文档模型拒绝了图片请求，请检查图片能力、接口和额度。',
  'vision-request-failed': '文档模型连接失败，请检查网络后重试。',
  'vision-no-text': '文档模型未返回文字，可改用免费识别。',
  'windows-ocr-no-text': '已处理所有待识别页，但没有识别到文字，原件已保留。',
  'vision-output-truncated': '文档模型输出被截断，请减少页数或调整模型。',
  'structure-queue-full': '识别队列已满，请稍后再试。',
  'derived-conflict': '资料版本已更新，正在同步，请重新识别。',
  'structure-interrupted': '识别被中断，可重新开始。',
  'no-candidate-pages': '没有新的待识别页；未读页可对照原件核查。',
  'automatic-budget-limit': '旧任务已达到云端自动识别上限，可改用本机整份识别。',
  'structure-unavailable': '文档结构尚未准备好，请重试整份识别。',
};
function errorMessage(error: unknown) {
  if (error instanceof ApiError) {
    const body = error.body as { error?: { code?: string } } | null;
    const code = body?.error?.code ?? '';
    if (messages[code]) return messages[code];
    if (error.status === 0) return '无法连接本地服务，请重试。';
    return `识别未开始（HTTP ${error.status}），请重试。`;
  }
  return '识别未开始，请重试。';
}

export function SourceStructureControl({ slug, source }: { slug: string; source: LearningSource }) {
  const qc = useQueryClient();
  const operation = useRef('');
  const endpoint = `/api/projects/${encodeURIComponent(slug)}/sources/${encodeURIComponent(source.sourceId)}/structure-jobs`;
  const supported = Boolean(source.derivedVersionId) && /\.pdf$/i.test(source.displayName);
  const parsing = source.status === 'stored' || source.status === 'processing';
  const automaticExpected = source.failureCode === 'no_extractable_text' || Boolean(source.coverage?.unreadPages);
  const needsOCR = automaticExpected || !source.coverage?.totalPages;
  const queryKey = ['source-structure', slug, source.sourceId, source.currentRevisionId];
  const query = useQuery({
    queryKey, enabled: supported,
    queryFn: () => http.get<JobList>(endpoint),
    refetchInterval: q => q.state.data?.jobs.some(job => job.status === 'queued' || job.status === 'running') || (!q.state.data?.jobs.length && (parsing || automaticExpected) && q.state.data?.localOCRAvailable !== false) ? 1500 : false,
  });
  const job = query.data?.jobs.find(item => item.revisionId === source.currentRevisionId);
  const active = job?.status === 'queued' || job?.status === 'running';
  useEffect(() => { operation.current = ''; }, [slug, source.sourceId, source.currentRevisionId, source.derivedVersionId]);
  useEffect(() => subscribeToSSE(SSE_EVENTS.sourceStructureJobUpdated, payload => {
    const event = payload as { projectSlug?: string; sourceId?: string };
    if (event.projectSlug === slug && event.sourceId === source.sourceId) {
      void query.refetch();
      void qc.invalidateQueries({ queryKey: ['learning-workspace', slug, 'sources'] });
    }
  }), [qc, slug, source.sourceId, query.refetch]);
  useEffect(() => {
    if (job?.status === 'succeeded') void qc.invalidateQueries({ queryKey: ['learning-workspace', slug, 'sources'] });
  }, [job?.id, job?.status, qc, slug]);
  const action = useMutation({
    mutationFn: (mode: 'cancel' | 'local' | 'configured') => mode === 'cancel'
      ? http.post<{ job: StructureJob }>(`${endpoint}/${encodeURIComponent(job!.id)}/cancel`, {})
      : http.post<{ job: StructureJob }>(endpoint, {
        allPages: true,
        operationId: operation.current || (operation.current = crypto.randomUUID()),
        revisionId: source.currentRevisionId, baseDerivedVersionId: source.derivedVersionId,
        ...(mode === 'local' ? { providerId: query.data?.localOCR?.providerId ?? 'windows-local-ocr' } : {}),
      }),
    onSuccess: result => {
      operation.current = '';
      qc.setQueryData<JobList>(queryKey, previous => ({
        localOCRAvailable: previous?.localOCRAvailable ?? false,
        configuredOCRAvailable: previous?.configuredOCRAvailable,
        localOCR: previous?.localOCR,
        jobs: [result.job, ...(previous?.jobs ?? []).filter(item => item.id !== result.job.id)],
      }));
      void query.refetch();
    },
    onError: () => { operation.current = ''; void query.refetch(); },
  });
  if (!supported) return null;
  const local = query.data?.localOCRAvailable;
  const failed = job?.status === 'failed' || job?.status === 'interrupted';
  const awaitingAutomatic = !job && automaticExpected && local !== false;
  const localFinished = job?.status === 'succeeded' && job.allPages && job.providerId.endsWith('-local-ocr');
  return <div className={s.control}>
    <div className={s.actions}>
      {active ? <>
        <small role="status">{job.status === 'queued' ? '等待后台识别' : '正在后台识别'}{job.allPages && ` · ${job.completedPages?.length ?? 0}/${job.pages?.length ?? 0} 页已处理`} · {job.providerId.endsWith('-local-ocr') ? query.data?.localOCR?.name ?? '免费系统 OCR' : job.model || '文档模型'}</small>
        <button type="button" disabled={action.isPending} onClick={() => action.mutate('cancel')}>取消识别</button>
      </> : parsing ? <small role="status">正在提取原有文字，扫描页随后自动识别。</small> : awaitingAutomatic ? <small role="status">正在安排整份自动识别，无需再次操作。</small> : <>
        {needsOCR && local && !localFinished && <button type="button" disabled={action.isPending || query.isLoading} onClick={() => action.mutate('local')}>{action.isPending ? '正在准备…' : failed || job?.status === 'cancelled' ? '重试整份免费识别' : '整份免费识别'}</button>}
        {needsOCR && query.data?.configuredOCRAvailable && <button type="button" className={s.secondary} disabled={action.isPending} onClick={() => action.mutate('configured')}>使用模型识别整份（按量计费）</button>}
      </>}
    </div>
    {query.data?.localOCR && <small>{local ? `${query.data.localOCR.name} · 本机免费识别` : query.data.localOCR.setupHint}{!local && <button type="button" onClick={() => void query.refetch()}>刷新状态</button>}</small>}
    {query.isError && <small role="alert">任务状态读取失败。<button type="button" onClick={() => void query.refetch()}>刷新状态</button></small>}
    {action.isError ? <small role="alert" className={s.failure}>{errorMessage(action.error)}</small>
      : failed && <small role="alert" className={s.failure}>{messages[job.errorCode ?? ''] ?? '识别未完成，已提取文字和原件已保留。可重试整份识别或检查文档模型。'}</small>}
    {active && job.allPages && <small>离开此页后仍在后台继续。可以随时取消；本地识别重启后从已保存进度恢复。</small>}
    {job?.status === 'succeeded' && <small role="status">本次识别已结束{source.coverage?.unreadPages ? `，${source.coverage.unreadPages} 页仍未提取到文字，请对照原件核查。` : '，请核对提取文字。'}{job.emptyPages?.length ? ` ${job.emptyPages.length} 页未识别到文字，仍标为未读。` : ''}</small>}
    {needsOCR && !active && query.data?.configuredOCRAvailable && <small>选择模型识别会发送剩余扫描页，按服务商计费；一次启动后连续处理整份，不自动重试已发送的请求。</small>}
  </div>;
}
