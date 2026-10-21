import { useEffect, useState } from 'react';

import { usePreferences, useSavePreferences } from '@/features/preferences/preferences';
import { Button } from '@/shared/primitive/Button';

import s from './PreferencesPage.module.css';

export function PreferencesPage() {
  const preferences = usePreferences();
  const save = useSavePreferences();
  const [draft, setDraft] = useState('');
  const [dirty, setDirty] = useState(false);
  const pageError = (preferences.error ?? save.error) as Error | null;

  useEffect(() => {
    if (!preferences.data || dirty) return;
    setDraft(preferences.data.content);
  }, [dirty, preferences.data]);

  const persist = async () => {
    await save.mutateAsync(draft);
    setDirty(false);
  };

  return (
    <main className={s.page} data-tour="preferences">
      <header className={s.header}>
        <div>
          <span className={s.eyebrow}>LEARNER-OWNED CONTEXT</span>
          <h1>全局学习偏好</h1>
          <p>只保留这一份跨学习单元生效的偏好文件。教师与助教可以读取，但不会编辑或自动推断新偏好。</p>
        </div>
        <Button onClick={() => void persist()} loading={save.isPending} disabled={!dirty || save.isPending}>
          {dirty ? '保存偏好' : '已保存'}
        </Button>
      </header>

      <section className={s.editorShell} aria-busy={preferences.isLoading}>
        <div className={s.fileBar}>
          <span className={s.dot} />
          <strong>{preferences.data?.path ?? 'preferences.md'}</strong>
          <small>{new Blob([draft]).size.toLocaleString()} / {(preferences.data?.maxBytes ?? 262144).toLocaleString()} bytes</small>
        </div>
        <textarea
          value={preferences.isLoading ? '加载中…' : draft}
          disabled={preferences.isLoading}
          onChange={(event) => {
            setDraft(event.target.value);
            setDirty(true);
          }}
          spellCheck={false}
          aria-label="全局学习偏好文件"
        />
      </section>

      <footer className={s.note}>
        <strong>边界</strong>
        <span>适合写表达方式、例子类型、公式与代码密度、提问和练习偏好；不要写 API Key、密码或项目事实。</span>
      </footer>
      {pageError && <p className={s.error} role="alert">{pageError.message}</p>}
    </main>
  );
}
