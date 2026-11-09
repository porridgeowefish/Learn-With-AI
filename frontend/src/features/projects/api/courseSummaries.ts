import { useQuery } from '@tanstack/react-query';
import { http } from '@/shared/client';

export interface TeachingSummary {
  id: string;
  status: string;
  concepts?: string[];
  scenario?: string;
}
export interface CourseLesson {
  projectSlug: string;
  title: string;
  topicId: string;
  linked: boolean;
  needsConfirmation?: boolean;
  summaries?: TeachingSummary[];
  error?: string;
}
export function useCourseSummaries(slug: string, version?: string) {
  return useQuery({
    queryKey: ['projects', slug, 'course-summaries', version],
    queryFn: () => http.get<{ lessons: CourseLesson[] }>(`/api/projects/${encodeURIComponent(slug)}/course-summaries?versionId=${encodeURIComponent(version ?? '')}`),
    enabled: Boolean(version),
    staleTime: 0,
    refetchOnMount: 'always',
    refetchInterval: query => query.state.data?.lessons.some(l => l.summaries?.some(s => ['pending', 'running'].includes(s.status))) ? 1500 : false,
  });
}
