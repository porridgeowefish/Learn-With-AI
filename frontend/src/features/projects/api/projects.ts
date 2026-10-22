// Projects API — TanStack Query hooks wrapping the backend endpoints.
// Provides the project API shape with typed responses.
// and proper cache invalidation.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { http } from '@/shared/client';
import { qk } from '@/shared/queryKeys';
import type {
  CreateProjectRequest,
  DeleteProjectResponse,  DisciplineLearningPlanResponse,
  ProjectTypeAdviceRequest,
  ProjectTypeAdviceResponse,
  ProjectResponse,
  ProjectsListResponse,} from '@/shared/types/api';

export function useProjects() {
  return useQuery({
    queryKey: qk.projects.all(),
    queryFn: () => http.get<ProjectsListResponse>('/api/projects'),
  });
}

export function useProject(slug: string | undefined) {
  return useQuery({
    queryKey: slug ? qk.projects.detail(slug) : ['projects', '__missing__'],
    enabled: !!slug,
    queryFn: () => http.get<ProjectResponse>(`/api/projects/${encodeURIComponent(slug!)}`),
  });
}

export function useCreateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: CreateProjectRequest) =>
      http.post<ProjectResponse>('/api/projects', payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.projects.all() });
      qc.invalidateQueries({ queryKey: qk.folders.all() });
      qc.invalidateQueries({ queryKey: qk.health() });
    },
  });
}

export function useDeleteProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (slug: string) =>
      http.delete<DeleteProjectResponse>(`/api/projects/${encodeURIComponent(slug)}`),
    onSuccess: (_response, slug) => {
      qc.removeQueries({ predicate: (query) => query.queryKey.includes(slug) });
      qc.invalidateQueries({ queryKey: qk.projects.all() });
      qc.invalidateQueries({ queryKey: qk.folders.all() });
      qc.invalidateQueries({ queryKey: qk.health() });
    },
  });
}

export function useProjectTypeAdvice() {
  return useMutation({
    mutationFn: (payload: ProjectTypeAdviceRequest) =>
      http.post<ProjectTypeAdviceResponse>('/api/project-type-advice', payload),
  });
}

export function useDisciplineLearningPlan(slug: string | undefined) {
  return useQuery({
    queryKey: ['projects', slug ?? '__missing__', 'discipline-learning-plan'],
    enabled: !!slug,
    queryFn: () =>
      http.get<DisciplineLearningPlanResponse>(
        `/api/projects/${encodeURIComponent(slug!)}/learning-plan`,
      ),
  });
}

export function useSaveDisciplineLearningPlan(slug: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (plan: DisciplineLearningPlanResponse) =>
      http.put<DisciplineLearningPlanResponse>(
        `/api/projects/${encodeURIComponent(slug)}/learning-plan`,
        plan,
      ),
    onSuccess: (plan) => {
      qc.setQueryData(['projects', slug, 'discipline-learning-plan'], plan);
    },
  });
}
