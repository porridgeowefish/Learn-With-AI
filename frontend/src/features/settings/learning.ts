import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { http } from '@/shared/client';

export interface LearningSettings {
  length: 'short' | 'medium' | 'long';
  effort: string;
  showReasoning: boolean;
  memoryEnabled: boolean;
  search: { enabled: boolean; provider: string; providerId: string; apiKey?: string; hasKey: boolean };
}
export interface LearningProfile { settings: LearningSettings; capabilities: Record<string, string[]> }
export interface LearningMemory { id: string; text: string; project: string; sourceProject: string; messageId: string; evidence: string; updatedAt: string; edited: boolean }
export const effortNames: Record<string, string> = { low: '低', medium: '中', high: '高', xhigh: '极高' };
export function useLearningProfile() {
  return useQuery({ queryKey: ['learning-profile'], queryFn: () => http.get<LearningProfile>('/api/settings/learning'), staleTime: 30000 });
}
export function useSaveLearningProfile() {
  const client = useQueryClient();
  return useMutation({ mutationFn: (settings: LearningSettings) => http.put<LearningProfile>('/api/settings/learning', settings),
    onSuccess: data => client.setQueryData(['learning-profile'], data),
    onSettled: () => { void client.invalidateQueries({ queryKey: ['learning-profile'] }); },
  });
}
export function useLearningMemories(enabled: boolean) {
  return useQuery({ queryKey: ['learning-memories'], queryFn: () => http.get<{ memories: LearningMemory[]; lastError?: string }>('/api/learning-memories'), enabled });
}
export function useChangeMemory() {
  const client = useQueryClient();
  return useMutation({ mutationFn: ({ id, text }: { id: string; text?: string }) => text === undefined
    ? http.delete(`/api/learning-memories/${encodeURIComponent(id)}`)
    : http.put(`/api/learning-memories/${encodeURIComponent(id)}`, { text }),
  onSuccess: () => client.invalidateQueries({ queryKey: ['learning-memories'] }) });
}
