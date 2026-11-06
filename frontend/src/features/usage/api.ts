import { useQuery } from '@tanstack/react-query';

import { http } from '@/shared/client';

export interface TeacherUsageTurn {
  responseId: string;
  providerId?: string;
  inputTokens: number;
  outputTokens: number;
  usageReported?: boolean;
  cacheReadTokens?: number;
  cacheWriteTokens?: number;
  cacheMissTokens?: number;
  occurredAt: string;
}

export interface TeacherUsageConversation {
  projectSlug: string;
  title: string;
  conversationId: string;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens?: number;
  cacheWriteTokens?: number;
  updatedAt: string;
  turns: TeacherUsageTurn[];
}

export interface TeacherUsagePage {
  page: number;
  pageSize: number;
  total: number;
  conversations: TeacherUsageConversation[];
}

export function useTeacherUsage(page = 1) {
  return useQuery({
    queryKey: ['teacher-usage', page],
    queryFn: () => http.get<TeacherUsagePage>(`/api/usage/teacher?page=${page}&pageSize=20`),
  });
}

export interface ProjectUsage { projectSlug: string; title: string; inputTokens: number; outputTokens: number; noteInputTokens: number; noteOutputTokens: number; cacheReadTokens?: number; cacheWriteTokens?: number }
export function useProjectUsage() {return useQuery({queryKey:['usage','projects'],queryFn:()=>http.get<{projects:ProjectUsage[]}>('/api/usage/projects')});}
