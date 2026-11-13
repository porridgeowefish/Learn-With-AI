import { useQuery, useQueryClient } from '@tanstack/react-query';

import { http } from '@/shared/client';
import { qk } from '@/shared/queryKeys';
import { useEffect, useSyncExternalStore } from 'react';
import type { QueryClient } from '@tanstack/react-query';
import { PreferenceAutosave } from './autosave';

export interface LearnerPreferences {
  path: string;
  content: string;
  maxBytes: number;
}

const editors = new WeakMap<QueryClient, PreferenceAutosave>();
export function usePreferenceEditor() {
  const client = useQueryClient();
  let editor = editors.get(client);
  if (!editor) {
    editor = new PreferenceAutosave(async content => {
      await client.cancelQueries({ queryKey: qk.preferences.file() });
      await http.put('/api/preferences', content, { headers: { 'Content-Type': 'text/markdown; charset=utf-8' } });
      client.setQueryData<LearnerPreferences>(qk.preferences.file(), current => current ? { ...current, content } : current);
    });
    editors.set(client, editor);
  }
  const state = useSyncExternalStore(editor.subscribe, editor.getSnapshot);
  const current = editor;
  useEffect(() => {
    const guard = (event: BeforeUnloadEvent) => {
      const draft = current.getSnapshot();
      if (draft.content !== draft.saved) { void current.flush(); event.preventDefault(); event.returnValue = ''; }
    };
    window.addEventListener('beforeunload', guard);
    return () => {
      window.removeEventListener('beforeunload', guard);
      current.setComposing(false);
      void current.flush();
    };
  }, [current]);
  return { editor: current, state };
}

export function usePreferences() {
  return useQuery({
    queryKey: qk.preferences.file(),
    queryFn: () => http.get<LearnerPreferences>('/api/preferences'),
  });
}
