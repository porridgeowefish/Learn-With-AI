import { useEffect, useId, useRef, useState } from 'react';

import { useTeacherSettings, useSaveTeacherSettings, type AutoNotesSettings } from '@/features/learning';
import { Button } from '@/shared/primitive/Button';
import { useAskAiSettings } from './askAi';
import s from './TeacherNotesSettings.module.css';

export function TeacherNotesSettings() {
  const query = useTeacherSettings();
  const save = useSaveTeacherSettings();
  const ai = useAskAiSettings();
  const id = useId();
  const [draft, setDraft] = useState<AutoNotesSettings>();
  const dirty = useRef(false);

  useEffect(() => { if (query.data && !dirty.current) setDraft({ ...query.data.autoNotes }); }, [query.data]);

  const update = (patch: Partial<AutoNotesSettings>) => {
    dirty.current = true;
    save.reset();
    setDraft(current => current ? { ...current, ...patch } : current);
  };
  const toggle = (enabled: boolean) => {
    if (!draft) return;
    const next = { ...draft, enabled };
    setDraft(next);
    save.reset();
    save.mutate(next, { onSuccess: () => { dirty.current = false; } });
  };
  if (query.isError) return <section className={s.card} role="alert">自动笔记配置读取失败。</section>;
  if (!draft) return <section className={s.card} aria-busy="true">加载自动笔记配置…</section>;

  const validInterval = Number.isInteger(draft.everyTurns) && draft.everyTurns >= 1 && draft.everyTurns <= 100;
  return (
    <section className={s.card} aria-labelledby={`${id}-title`}>
      <header className={s.header}>
        <div>
          <h2 id={`${id}-title`} className={s.title}>教师自动笔记</h2>
          <p className={s.description}>把对话中的关键概念、知识点和理解变化，简短记录到资产。</p>
        </div>
        <label className={s.toggle}>
          <input type="checkbox" role="switch" checked={draft.enabled} disabled={save.isPending}
            onChange={event => toggle(event.target.checked)} />
          <span className={s.track} aria-hidden="true" />
          <span>开启自动笔记</span>
        </label>
      </header>

      <details className={s.advanced}>
        <summary>高级设置：记录间隔与专用模型</summary>
      <div className={s.fields}>
        <div className={s.field}>
          <label htmlFor={`${id}-interval`}>记录间隔</label>
          <div className={s.interval}>
            <span>每</span>
            <input id={`${id}-interval`} className={s.input} type="number" min={1} max={100} step={1}
              value={draft.everyTurns || ''} disabled={save.isPending} aria-invalid={!validInterval}
              aria-describedby={`${id}-interval-help`}
              onChange={event => update({ everyTurns: Number(event.target.value) })} />
            <span>轮对话记录一次</span>
          </div>
          <p id={`${id}-interval-help`} className={validInterval ? s.hint : s.error}>
            {validInterval ? '只计入成功完成的教师回复。' : '请输入 1–100 之间的整数。'}
          </p>
        </div>
        <div className={s.field}>
          <label htmlFor={`${id}-provider`}>笔记连接</label>
          <select id={`${id}-provider`} className={s.input} value={draft.providerId} disabled={save.isPending}
            onChange={event => update({ providerId: event.target.value, model: '' })}>
            <option value="">跟随当前教师连接</option>
            {ai.data?.providers.map(provider => <option key={provider.id} value={provider.id}>{provider.name || provider.id}</option>)}
          </select>
          <p className={s.hint}>默认跟随当前学习模型。</p>
        </div>
        {draft.providerId && <div className={s.field}>
          <label htmlFor={`${id}-model`}>笔记模型</label>
          <input id={`${id}-model`} className={s.input} value={draft.model} disabled={save.isPending}
            placeholder="使用连接的默认模型" onChange={event => update({ model: event.target.value })} />
          <p className={s.hint}>留空时使用该连接的默认模型。</p>
        </div>}
      </div>
      <Button variant="outline" size="sm" loading={save.isPending} disabled={!validInterval}
        onClick={() => save.mutate(draft, { onSuccess: () => { dirty.current = false; } })}>保存高级设置</Button>
      </details>

      <footer className={s.footer}>
        <p className={save.isError ? s.error : s.hint} role="status" aria-live="polite">
          {save.isError ? `保存失败：${(save.error as Error).message}` : save.isSuccess ? '自动笔记配置已保存。' : '笔记保存在各项目的「资产 → 笔记」中。'}
        </p>
      </footer>
    </section>
  );
}
