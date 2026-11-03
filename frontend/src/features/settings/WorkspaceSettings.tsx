import { useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useHealth } from '@/app/api/health';
import s from './SettingsPage.module.css';

type Directory = { path: string; parent: string; destination: string; children: string[] };
type Conflict = { kind: string; locations: { source: string; projectTitle: string; projectSlug: string; chapter?: string; title: string; topicId?: string }[] };

function post(action: string, body?: BodyInit, signal?: AbortSignal, operation?: string) {
  return fetch('/api/workspace/' + action, {
    method: 'POST', body, signal,
    headers: { 'X-LLL-Workspace': '1', ...(operation ? { 'X-LLL-Operation': operation } : {}), 'Content-Type': body instanceof Blob ? 'application/zip' : 'application/json' },
  });
}

export function WorkspaceSettings() {
  const health = useHealth();
  const client = useQueryClient();
  const [parent, setParent] = useState('');
  const [preview, setPreview] = useState('');
  const [dir, setDir] = useState<Directory>();
  const [file, setFile] = useState<File>();
  const [busy, setBusy] = useState('');
  const [status, setStatus] = useState({ section: '', text: '' });
  const [conflicts, setConflicts] = useState<Conflict[]>([]);
  const active = useRef<AbortController | null>(null);
  const operationId = useRef<string>();
  const input = useRef<HTMLInputElement>(null);

  async function run(action: 'move' | 'preview' | 'directories' | 'export' | 'import', path?: string) {
    if (active.current) return;
    const controller = new AbortController();
    active.current = controller;
    const section = action === 'move' || action === 'preview' || action === 'directories' ? 'move' : 'transfer';
    const message = (text: string) => setStatus({ section, text });
    setBusy(action);
    message('处理中…');
    try {
      const body = action === 'move' ? JSON.stringify({ parent: parent.trim() }) : (action === 'directories' || action === 'preview') ? JSON.stringify({ path }) : action === 'import' ? file : undefined;
      operationId.current = action === 'import' ? crypto.randomUUID() : undefined;
      const response = await post(action === 'preview' ? 'directories' : action, body, controller.signal, operationId.current);
      if (!response.ok) {
        const problem = await response.json().catch(() => ({}));
        if (action === 'import' && response.status === 409 && problem.cancelled) {
          clearSelection();
          message('已取消导入。');
          return;
        }
        if (action === 'import' && response.status === 409 && problem.conflicts?.length) {
          setConflicts(problem.conflicts);
          message('整包未导入。请处理以下重名记录后重新检查。');
          return;
        }
        throw new Error(problem.error?.message || problem.message || (typeof problem.error === 'string' && problem.error) || '操作失败 (' + response.status + ')');
      }
      if (action === 'export') {
        const url = URL.createObjectURL(await response.blob());
        const link = document.createElement('a');
        link.href = url;
        link.download = '学习记录-' + new Date().toISOString().slice(0, 10) + '.zip';
        link.click();
        window.setTimeout(() => URL.revokeObjectURL(url), 60000);
        message('已生成 ZIP，可在下载中查看。');
      } else if (action === 'preview') {
        setPreview((await response.json()).destination);
        message('');
      } else if (action === 'directories') {
        setDir(await response.json());
        message('');
      } else if (action === 'move') {
        const result = await response.json();
        message('已迁移至 ' + result.path + '。请重新打开应用。');
        setDir(undefined);
      } else {
        clearSelection();
        message('导入完成。项目列表已更新。');
        void client.invalidateQueries();
      }
    } catch (err) {
      message(controller.signal.aborted ? '已请求取消，请刷新项目列表核对结果。' : String(err instanceof Error ? err.message : err));
    } finally {
      operationId.current = undefined;
      active.current = null;
      setBusy('');
    }
  }
  async function cancelImport() {
    const id = operationId.current;
    if (!id) {
      clearSelection();
      setStatus({ section: '', text: '' });
      return;
    }
    setStatus({ section: 'transfer', text: '正在请求取消…' });
    try {
      const response = await post('import/cancel', undefined, undefined, id);
      if (!response.ok) throw new Error('取消请求未确认');
    } catch {
      if (operationId.current === id) setStatus({ section: 'transfer', text: '取消请求未确认，可以重试。正在等待导入结果。' });
    }
  }
  function clearSelection() {
    setFile(undefined);
    setConflicts([]);
    if (input.current) input.current.value = '';
  }
  return <>
    <section className={s.appearance} aria-labelledby="workspace-location">
      <h2 id="workspace-location">更改存储位置</h2>
      <p>当前位置 <code>{health.data?.workspace || '…'}</code></p>
      <label>新位置的父目录 <input value={parent} disabled={!!busy} placeholder="输入或选择绝对路径" onChange={e => { setParent(e.target.value); setPreview(''); }} /></label>
      <div className={s.openSourceActions}><button disabled={!!busy} onClick={() => run('directories', parent.trim())}>浏览目录</button></div>
      {dir && <fieldset disabled={!!busy}><legend>选择父目录</legend><p>{dir.path}</p>
        <select aria-label="子目录" value="" onChange={e => run('directories', e.target.value)}><option value="">进入子目录…</option>{dir.children.map(child => <option key={child} value={child}>{child}</option>)}</select>
        <div className={s.openSourceActions}><button disabled={dir.path === dir.parent} onClick={() => run('directories', dir.parent)}>上一级</button>
          <button onClick={() => { setParent(dir.path); setPreview(dir.destination); setDir(undefined); }}>使用此目录</button><button onClick={() => setDir(undefined)}>关闭</button></div>
      </fieldset>}
      {preview && <p>预计新位置 <code>{preview}</code>。迁移前若目录被占用，将使用下一个可用编号。</p>}
      <div className={s.openSourceActions}><button disabled={!!busy || !parent.trim()} onClick={() => preview ? run('move') : run('preview', parent.trim())}>{preview ? '确认迁移' : '预览新位置'}</button></div>
      {status.section === 'move' && <p role="status">{status.text}</p>}
    </section>
    <section className={s.appearance} aria-labelledby="workspace-transfer">
      <h2 id="workspace-transfer">导入与导出</h2>
      <p>导出的 ZIP 含有学习正文、对话与个人记录，请确认分享对象。导入会把整包记录合并到当前位置。</p>
      <div className={s.openSourceActions}><button disabled={!!busy} onClick={() => run('export')}>导出学习记录</button></div>
      <label>选择学习记录 ZIP <input ref={input} disabled={!!busy} type="file" accept=".zip,application/zip" onChange={e => { setFile(e.target.files?.[0]); setConflicts([]); setStatus({ section: '', text: '' }); }} /></label>
      <div className={s.openSourceActions}><button disabled={!!busy || !file} onClick={() => run('import')}>{conflicts.length ? '重新检查' : '导入学习记录'}</button>
        {file && <button disabled={!!busy && busy !== 'import'} onClick={cancelImport}>取消导入</button>}</div>
      {status.section === 'transfer' && <p role="status">{status.text}</p>}
      {conflicts.map((c,i) => <div key={i}><strong>{c.kind === 'project' ? '项目' : '小项目'}重名 · {c.locations[0]?.title}</strong>
        <ul>{c.locations.map((l,j) => <li key={j}>{l.source === 'incoming' ? '导入包' : '当前工作区'} · {l.projectTitle} ({l.projectSlug}){l.topicId && ' / ' + [l.chapter, l.title, l.topicId].filter(Boolean).join(' / ')}</li>)}</ul></div>)}
    </section>
  </>;
}
