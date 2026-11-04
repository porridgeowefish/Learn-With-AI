import { useEffect } from 'react';

import { usePreferences, usePreferenceEditor } from '@/features/preferences/preferences';
import { Button } from '@/shared/primitive/Button';
import { LEARNING_PRESETS } from './presets';

import s from './PreferencesPage.module.css';

export function PreferencesPage() {
  const preferences = usePreferences();
  const { editor, state } = usePreferenceEditor();
  const draft = state.content;
  const dirty = draft !== state.saved;
  const pageError = (preferences.error ?? state.error) as Error | null;

  useEffect(() => {
    if (preferences.data) editor.load(preferences.data.content);
  }, [editor, preferences.data]);

  return (
    <main className={s.page} data-tour="preferences">
      <header className={s.header}>
        <div>
          <span className={s.eyebrow}>LEARNER-OWNED CONTEXT</span>
          <h1>全局学习偏好</h1>
          <p>只保留这一份跨学习单元生效的偏好文件。教师与助教可以读取，但不会编辑或自动推断新偏好。</p>
        </div>
        <span className={s.status} role="status">{state.error ? '尚未保存' : state.saving ? '正在保存…' : dirty ? '等待自动保存…' : state.ready ? '已自动保存' : '加载中…'}</span>
      </header>

      <section className={s.presets} aria-label="学习方法预置">
        <h2>选择一种学习方法</h2>
        <p>点击后添加到下方，可自由修改，也可以组合使用。</p>
        <div>{LEARNING_PRESETS.map(preset => <Button key={preset.name} disabled={!state.ready || draft.includes(preset.content)} onClick={() => editor.edit(`${draft}${draft.trim() ? '\n\n' : ''}${preset.content}`)}>{preset.name}</Button>)}</div>
      </section>

      <section className={s.editorShell} aria-busy={preferences.isLoading}>
        <div className={s.fileBar}>
          <span className={s.dot} />
          <strong>{preferences.data?.path ?? 'preferences.md'}</strong>
          <small>{new Blob([draft]).size.toLocaleString()} / {(preferences.data?.maxBytes ?? 262144).toLocaleString()} bytes</small>
        </div>
        <textarea
          value={preferences.isLoading ? '加载中…' : draft}
          disabled={!state.ready}
          onChange={(event) => editor.edit(event.target.value)}
          onCompositionStart={() => editor.setComposing(true)}
          onCompositionEnd={() => editor.setComposing(false)}
          onBlur={() => { void editor.flush(); }}
          spellCheck={false}
          aria-label="全局学习偏好文件"
        />
      </section>

      <footer className={s.note}>
        <strong>边界</strong>
        <span>适合写表达方式、例子类型、公式与代码密度、提问和练习偏好；不要写 API Key、密码或项目事实。</span>
      </footer>
      {pageError && <div className={s.error} role="alert">{pageError.message} {state.error && <Button onClick={() => void editor.flush()}>重试保存</Button>}</div>}
    </main>
  );
}
