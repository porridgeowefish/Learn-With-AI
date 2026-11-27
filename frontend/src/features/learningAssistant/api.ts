import { useQuery } from '@tanstack/react-query';
import { useHealth } from '@/app/api/health';
import { http } from '@/shared/client';

export interface StudyUnit {
  title: string;
  goal: string;
  reviewTask: string;
  date: string;
}

export interface CourseProposal {
  title: string;
  startDate: string;
  weekdays: number[];
  units: StudyUnit[];
}

export interface StudyLink { title: string; url: string; quote?: string }
export interface StudyAttachment { name: string; text: string }
export interface CreatedCourse { slug: string; title: string; planned: number }
export interface StudyEvent {
  id: string;
  kind: 'study' | 'review';
  date: string;
  title: string;
  detail?: string;
  course?: string;
  slug?: string;
  topicId?: string;
  completed: boolean;
}
export interface AssistantCreditUsage {
  units: number;
  pending: boolean;
  pendingRequests?: string[];
}

export interface AssistantReply {
  reply: string;
  course?: CourseProposal;
  links?: StudyLink[];
  completedReview?: string;
  creditUsage?: AssistantCreditUsage;
}

const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
export const calendarKey = ['learning-calendar', timeZone] as const;
export function useStudyCalendar() {
  const workspace = useHealth();
  return useQuery({
    queryKey: [...calendarKey, workspace.data?.workspace],
    enabled: !workspace.isPending,
    queryFn: () => http.get<{ today: string; items: StudyEvent[] }>(
      '/api/learning-calendar?' + new URLSearchParams({ timeZone }),
    ),
    refetchInterval: 30_000,
    refetchOnWindowFocus: 'always',
    refetchOnMount: 'always',
  });
}

export function assistantTurn(
  messages: { role: 'user' | 'assistant'; content: string }[],
  attachments: StudyAttachment[], reviewId: string | undefined, providerId: string, signal: AbortSignal,
) {
  return http.post<AssistantReply>('/api/learning-assistant', {
    messages: messages.slice(-28).map(({ role, content }) => ({ role, content })),
    attachments, reviewId, timeZone, providerId,
  }, { signal });
}

export const readAssistantSettlements = (requestIds: string[]) =>
  http.post<Record<string, number>>('/api/learning-assistant/settlements', { requestIds });

export const confirmCourse = (course: CourseProposal, operationId: string, signal: AbortSignal) =>
  http.post<CreatedCourse>('/api/learning-assistant/courses', { course, operationId, timeZone }, { signal });

export const openStudyUnit = (slug: string, topicId: string) => http.post<{ slug: string }>(
  `/api/projects/${encodeURIComponent(slug)}/units/${encodeURIComponent(topicId)}/open`,
);

export async function uploadStudyAttachment(file: File): Promise<StudyAttachment> {
  const form = new FormData();
  form.set('file', file);
  const response = await fetch('/api/learning-assistant/attachments', { method: 'POST', body: form });
  const result = await response.json();
  if (!response.ok) throw new Error(result.error ?? '附件未能上传。');
  return result as StudyAttachment;
}
