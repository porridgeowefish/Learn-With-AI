import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { ApiError, http } from '@/shared/client';

export interface UpdateAttempt {
  version: string;
  exitCode: number;
  log: string;
}

export interface UpdateInfo {
  current: string;
  latest?: string;
  available: boolean;
  canInstall: boolean;
  notesUrl?: string;
  error?: string;
  lastResult?: UpdateAttempt;
  phase?: 'idle' | 'downloading' | 'ready' | 'installing' | 'failed';
  downloaded?: number;
  total?: number;
  preparedVersion?: string;
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
    refetchInterval: query => query.state.data?.phase === 'downloading' ? 1000 : 6 * 60 * 60 * 1000,
    refetchOnWindowFocus: false,
    retry: false,
  });
}

export function useCheckUpdate() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: () => fetchUpdate(true),
    onSuccess: (info) => client.setQueryData(updateKey, info),
  });
}

// msiexec success codes: 0 and 3010 (success, reboot suggested).
export function failedAttempt(info: UpdateInfo | null | undefined): UpdateAttempt | null {
  const last = info?.lastResult;
  if (!last || last.exitCode === 0 || last.exitCode === 3010) return null;
  return last.version !== info?.current ? last : null;
}
