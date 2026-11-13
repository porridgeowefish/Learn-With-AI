import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { API_BASE } from '@/shared/lib/constants';
import { ApiError } from '@/shared/client';

export interface AskProvider {
  id: string;
  kind: 'openai' | 'anthropic' | 'siwc';
  name?: string;
  baseURL: string;
  apiKey: string;
  apiKeyEnv?: string;
  model: string;
  contextWindowTokens?: number;
  reasoning?: boolean;
  thinking?: boolean;
}

export interface AskConfig {
  default: string;
  searchEngine: 'google' | 'bing';
  providers: AskProvider[];
  bindings?: Record<string, { providerId: string; model?: string }>;
}

export const qkAskAi = {
  settings: () => ['ask-ai', 'settings'] as const,
};

export function useAskAiSettings() {
  return useQuery<AskConfig>({
    queryKey: qkAskAi.settings(),
    queryFn: async () => {
      const res = await fetch(`${API_BASE}/api/settings/ask-ai`);
      if (!res.ok) throw new ApiError(res.status, 'settings failed', null);
      // A fresh workspace has no config yet; the server then sends null lists.
      const raw = (await res.json()) as Partial<AskConfig> & { providers?: AskProvider[] | null; bindings?: AskConfig['bindings'] | null };
      return { ...raw, default: raw.default ?? '', searchEngine: raw.searchEngine || 'google', providers: raw.providers ?? [], bindings: raw.bindings ?? {} } as AskConfig;
    },
  });
}

export function useSaveAskAiSettings() {
  const qc = useQueryClient();
  return useMutation({
    scope: { id: 'ask-ai-settings' },
    mutationFn: async (cfg: AskConfig) => {
      const res = await fetch(`${API_BASE}/api/settings/ask-ai`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(cfg),
      });
      if (!res.ok) throw new ApiError(res.status, 'save failed', null);
    },
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ['learning-profile'] }); return qc.invalidateQueries({ queryKey: qkAskAi.settings() }); },
  });
}

export function useProbeAskAi() {
  return useMutation({
    mutationFn: async (vars: { providerId?: string; inline?: AskProvider }) => {
      const res = await fetch(`${API_BASE}/api/settings/ask-ai/probe`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(vars),
      });
      if (!res.ok) throw new ApiError(res.status, 'probe failed', null);
      return (await res.json()) as { ok: boolean; error?: string };
    },
  });
}

export interface ModelCatalog { models: Array<{ id: string; contextWindowTokens?: number; vision?: boolean | null }>; source: 'provider' | 'preset'; verifiedAt: string }
export async function discoverModels(body: { providerId?: string; inline?: AskProvider }): Promise<ModelCatalog> {
  const res = await fetch(`${API_BASE}/api/model-connections/models`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  if (!res.ok) throw new ApiError(res.status, 'model catalog unavailable', null);
  return await res.json() as ModelCatalog;
}
