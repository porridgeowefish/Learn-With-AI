import { useQuery } from '@tanstack/react-query';

import { ApiError, http } from '@/shared/client';

export interface UpdateInfo {
  current: string;
  latest?: string;
  available: boolean;
  notesUrl?: string;
  error?: string;
}

const updateKey = ['system', 'update'] as const;

// Only the desktop app serves this endpoint; elsewhere it 404s and the UI hides.
async function fetchUpdate(force = false): Promise<UpdateInfo | null> {
  try {
    return await http.get<UpdateInfo>(`/api/system/update${force ? '?force=1' : ''}`);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) return null;
    throw error;
  }
}

export function useUpdateInfo() {
  return useQuery({
    queryKey: updateKey,
    queryFn: () => fetchUpdate(false),
    staleTime: 30 * 60 * 1000,
    refetchInterval: 6 * 60 * 60 * 1000,
    refetchOnWindowFocus: false,
    retry: false,
  });
}
