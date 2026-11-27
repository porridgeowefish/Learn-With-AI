import { useEffect } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { http } from '@/shared/client';
import { subscribeToSSE } from '@/app/hooks/useSSE';
import { SSE_EVENTS } from '@/shared/lib/constants';
import { useConnectionStore } from '@/app/store/connection';

export type GenerationKind = 'review' | 'practice' | 'discipline-map';
export interface GenerationJob {
  id: string; rootJobId: string; retryOf?: string; attempt: number; kind: GenerationKind;
  status: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled' | 'interrupted';
  phase: 'waiting' | 'preparing' | 'generating' | 'validating' | 'publishing' | 'done';
  providerId: string; model: string; questionCount?: number;
  progress: { indeterminate?: boolean; completedUnits?: number; totalUnits?: number; unitLabel?: string; outputTokens?: number; tokensEstimated?: boolean };
  errorCode?: string; updatedAt: string;
  candidateAvailable?: boolean;
  validation?: {code:string; message:string; line?:number; column?:number};
  checkpoint?: { afterSeq:number; throughSeq:number; hasMoreConversation:boolean };
}
export interface GenerationRequest { operationId: string; kind: GenerationKind; providerId?: string; model?: string; sourceIds?: string[]; questionCount?: number; mapMode?: 'general' | 'sources'; mapBrief?: string; mapSchedule?: { startDate: string; weekdays: number[] } }
export const generationKey = (slug: string) => ['learning-workspace', slug, 'generation-jobs'] as const;
const endpoint = (slug: string) => `/api/projects/${encodeURIComponent(slug)}/generation-jobs`;
export const isGenerationActive = (job: GenerationJob) => job.status === 'queued' || job.status === 'running';
export function useGenerationJobs(slug: string) {
  const qc = useQueryClient();
  const connectionStatus=useConnectionStore(state=>state.status);
  useEffect(()=>{if(connectionStatus==='open')void qc.invalidateQueries({queryKey:generationKey(slug)});},[connectionStatus,qc,slug]);
  useEffect(() => subscribeToSSE(SSE_EVENTS.generationJobUpdated, payload => {
    const event=payload as {projectSlug?:string;jobId:string;status:GenerationJob['status'];phase:GenerationJob['phase'];progress:GenerationJob['progress']};
    if (event.projectSlug !== slug) return;
    const snapshot=qc.getQueryData<{jobs:GenerationJob[]}>(generationKey(slug));
    if(snapshot?.jobs.some(job=>job.id===event.jobId))qc.setQueryData(generationKey(slug),{...snapshot,jobs:snapshot.jobs.map(job=>job.id===event.jobId?{...job,status:event.status,phase:event.phase,progress:event.progress}:job)});
    else void qc.invalidateQueries({queryKey: generationKey(slug)});
    if(event.status==='running'||event.status==='queued')return;
    void qc.invalidateQueries({queryKey:generationKey(slug)});
    void qc.invalidateQueries({queryKey: ['learning-workspace', slug, 'assets']});
    void qc.invalidateQueries({queryKey: ['projects', slug, 'discipline-map']});
    void qc.invalidateQueries({queryKey: ['usage']});
  }), [qc, slug]);
  const query=useQuery({queryKey: generationKey(slug), queryFn: () => http.get<{jobs: GenerationJob[]}>(`${endpoint(slug)}?limit=100`), refetchInterval: query => query.state.data?.jobs.some(isGenerationActive) ? 1500 : false, refetchOnWindowFocus: 'always'});
  const successfulVersions=query.data?.jobs.filter(job=>job.status==='succeeded').map(job=>`${job.id}:${job.updatedAt}`).join('|');
  useEffect(()=>{if(!successfulVersions)return;void qc.invalidateQueries({queryKey:['learning-workspace',slug,'assets']});void qc.invalidateQueries({queryKey:['projects',slug,'discipline-map']});},[successfulVersions,qc,slug]);
  return query;
}
export function useGenerationAction(slug: string) {
  const qc = useQueryClient();
  return useMutation({mutationFn: (action: {request: GenerationRequest} | {id: string; action: 'cancel' | 'retry'; operationId?: string}) => 'request' in action
    ? http.post<{job: GenerationJob}>(endpoint(slug), action.request)
    : http.post<{job: GenerationJob}>(`${endpoint(slug)}/${encodeURIComponent(action.id)}/${action.action}`, action.operationId ? {operationId: action.operationId} : {}),
    onSettled: () => { void qc.invalidateQueries({queryKey: generationKey(slug)}); }});
}
export function useGenerationCandidate(slug:string,jobId:string|undefined,enabled:boolean) {
 return useQuery({queryKey:['learning-workspace',slug,'generation-candidate',jobId],enabled:enabled&&Boolean(jobId),retry:false,
 queryFn:()=>http.get<{content:string;overviewMarkdown?:string}>(`${endpoint(slug)}/${encodeURIComponent(jobId!)}/candidate`)});
}
