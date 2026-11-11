import { useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useHealth } from '@/app/api/health';
import { Button } from '@/shared/primitive/Button';
import s from './SettingsPage.module.css';

type Directory = { path: string; destination: string };
type Conflict = { kind: string; locations: { source: string; projectTitle: string; projectSlug: string; chapter?: string; title: string; topicId?: string }[] };

const initialConflictLimit = 5;

function post(action: string, body?: BodyInit, signal?: AbortSignal, operation?: string) {
  return fetch('/api/workspace/' + action, {
    method: 'POST', body, signal,
    headers: { 'X-LWA-Workspace': '1', ...(operation ? { 'X-LWA-Operation': operation } : {}), 'Content-Type': body instanceof Blob ? 'application/zip' : 'application/json' },
  });
}

export function WorkspaceSettings() {
  const health = useHealth();
  const client = useQueryClient();
  const [opened, setOpened] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [retry, setRetry] = useState(false);

  const [dir, setDir] = useState<Directory>();
  const [file, setFile] = useState<File>();
  const [busy, setBusy] = useState('');
  const [status, setStatus] = useState({ section: '', text: '' });
  const [conflicts, setConflicts] = useState<Conflict[]>([]);
  const [showAllConflicts, setShowAllConflicts] = useState(false);
  const remainingConflicts = conflicts.length - initialConflictLimit;
  const visibleConflicts = showAllConflicts ? conflicts : conflicts.slice(0, initialConflictLimit);
  const active = useRef<AbortController | null>(null);
  const operationId = useRef<string>();
  const input = useRef<HTMLInputElement>(null);

  async function run(action: 'move' | 'directories' | 'export' | 'import', selectedFile?: File) {
    if (active.current) return;
    const controller = new AbortController();
    active.current = controller;
    const section = action === 'move' || action === 'directories' ? 'move' : 'transfer';
    const message = (text: string) => setStatus({ section, text });
    setBusy(action);
    message(action === 'import' ? '正在检查并导入学习记录…' : action === 'export' ? '正在打包学习记录…' : action === 'move' ? '正在迁移学习记录…' : '');
    try {
      const body = action === 'move' ? JSON.stringify({ parent: dir?.path }) : (action === 'directories') ? JSON.stringify({ pick: true }) : action === 'import' ? selectedFile : undefined;
      operationId.current = action === 'import' ? crypto.randomUUID() : undefined;
      const response = await post(action, body, controller.signal, operationId.current);
      if (!response.ok) {
        const problem = await response.json().catch(() => ({}));
        if (action === 'import' && response.status === 409 && problem.cancelled) {
          clearSelection();
          message('已取消导入。');
          return;
        }
        if (action === 'import' && response.status === 409 && problem.conflicts?.length) {
          setRetry(true);
          setShowAllConflicts(false);
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
      } else if (action === 'directories') {
        const result = await response.json();
        setDir(result.cancelled ? undefined : result);
      } else if (action === 'move') {
        const result = await response.json();
        message('已迁移至 ' + result.path + '。请重新打开应用。');
        setDir(undefined);
        void health.refetch?.();
      } else {
        setRetry(false);
        message('导入完成。项目列表已更新。');
        void client.invalidateQueries();
      }
    } catch (err) {
      if (action === 'import') setRetry(true);
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

  }
  function select(files: FileList | null) {
    if (active.current || !files?.length) return;
    clearSelection();
    setRetry(false);
    if (files.length !== 1 || !/\.zip$/i.test(files[0].name)) {
      setStatus({section: 'transfer', text: '请一次选择一个 ZIP 文件。'});
      return;
    }
    const archive = files[0];
    setFile(archive);
    void run('import', archive);
  }
  return <>
    <section className={s.workspaceSection} aria-labelledby="workspace-location">
      <header className={s.workspaceHeader}><div><h2 id="workspace-location">更改存储位置</h2><p>当前位置 · {health.data?.workspace || '…'}</p></div>
      <div className={s.settingsActions}><button type="button" disabled={!!busy} onClick={() => run('directories')}>选择文件夹</button></div></header>
      {dir && <><p>迁移至 <strong>{dir.destination}</strong>。若目录已被占用，将使用下一个可用编号。</p>
        <div className={s.workspaceActions}><Button size="sm" variant="primary" disabled={!!busy} onClick={() => run('move')}>确认迁移</Button><Button size="sm" variant="ghost" disabled={!!busy} onClick={() => setDir(undefined)}>取消</Button></div></>}
      {status.section === 'move' && status.text && <p role="status">{status.text}</p>}
    </section>
    <section className={s.workspaceSection} aria-labelledby="workspace-transfer">
      <header className={s.workspaceHeader}><div><h2 id="workspace-transfer">导入与导出</h2>
        <p>用 ZIP 转移学习记录。个人偏好和 API 配置仅保存在本机。</p></div>
      <div className={s.settingsActions}><button type="button" disabled={!!busy} onClick={() => { if (!opened) setStatus({section:'',text:''}); setOpened(true); }}>导入学习记录</button>
        <button type="button" disabled={!!busy} onClick={() => {setOpened(false); void run('export');}}>导出学习记录</button></div></header>
      {opened && <div className={s.importPanel} data-dragging={dragging}
        onDragOver={e => {e.preventDefault(); if (!busy) setDragging(true);}}
        onDragLeave={e => {if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDragging(false);}}
        onDrop={e => {e.preventDefault(); setDragging(false); select(e.dataTransfer.files);}}>
        <input hidden aria-label="选择学习记录 ZIP" ref={input} type="file" accept=".zip,application/zip" onChange={e => { select(e.target.files); e.target.value = ''; }} />
        <button className={s.dropButton} disabled={!!busy} onClick={() => input.current?.click()}>
          <strong>{dragging ? '松开以导入 ZIP' : file?.name || '将学习记录 ZIP 拖到这里'}</strong>
          <span>{file ? '选择其他文件' : '或点击选择文件，选好后自动导入'}</span>
        </button>
        {status.section === 'transfer' && <p role="status">{status.text}</p>}
        {visibleConflicts.map((c,i) => <div key={i}><strong>{c.kind === 'project' ? '项目' : '小项目'}重名 · {c.locations[0]?.title}</strong>
          <ul>{c.locations.map((l,j) => <li key={j}>{l.source === 'incoming' ? '导入包' : '当前工作区'} · {l.projectTitle} ({l.projectSlug}){l.topicId && ' / ' + [l.chapter, l.title, l.topicId].filter(Boolean).join(' / ')}</li>)}</ul></div>)}
        {remainingConflicts > 0 && <div className={s.settingsActions}>
          <button type="button" aria-expanded={showAllConflicts} onClick={() => setShowAllConflicts(!showAllConflicts)}>
            {showAllConflicts ? '收起' : `还有 ${remainingConflicts} 条冲突，展开全部`}
          </button>
        </div>}
        <div className={s.workspaceActions}>{retry && file && <Button size="sm" disabled={!!busy} onClick={() => { setConflicts([]); void run('import',file); }}>重新检查</Button>}
          <Button size="sm" variant="ghost" disabled={!!busy && busy !== 'import'} onClick={() => { if (busy === 'import') void cancelImport(); else { clearSelection(); setOpened(false); setStatus({section:'',text:''}); } }}>{busy === 'import' ? '取消导入' : '关闭'}</Button></div>
      </div>}
      {!opened && status.section === 'transfer' && <p role="status">{status.text}</p>}
    </section>
  </>;
}
