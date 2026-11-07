import { useUpdateAction, useUpdateInfo } from './update';
import s from './SettingsPage.module.css';

const megabytes = (n: number) => (n / 1048576).toFixed(1) + ' MB';

export function UpdatePanel() {
  const query = useUpdateInfo();
  const action = useUpdateAction();
  const info = query.data;
  const phase = info?.phase ?? 'idle';
  const downloading = phase === 'downloading';
  const waiting = phase === 'waiting';
  const installing = phase === 'installing';
  const busy = action.isPending || downloading || waiting || installing;
  const downloaded = info?.downloadedBytes ?? 0;
  const total = info?.totalBytes ?? 0;
  return <section className={`${s.openSource} ${s.updatePanel}`} aria-labelledby="version-data-title" id="version-data">
    <div>
      <h2 id="version-data-title">版本与更新</h2>
      <p>{info ? <>当前版本 <strong>{info.current}</strong>{info.available && <> · 发现新版本 <strong>{info.latest}</strong></>}</> : query.isPending ? '正在读取版本…' : '此运行方式暂不提供页内更新，请从 Gitee 下载。'}</p>
      <p>更新源：Gitee · 点击更新后自动下载、安装并重新打开，保留本机学习记录。</p>
      {info?.latest && !info.available && !info.error && <p role="status">已是最新正式版本。</p>}
      {info && !info.canInstall && <p>当前运行方式请下载对应系统的安装包后更新。Windows MSI 安装版支持在此安装并重启。</p>}
      {downloading && <div role="status">
        <p>正在下载 {megabytes(downloaded)}{total > 0 ? ` / ${megabytes(total)}` : ''}</p>
        <progress aria-label="更新下载进度" value={total > 0 ? downloaded : undefined} max={total || 1} />
      </div>}
      {phase === 'ready' && <p role="status">安装包已就绪，可重试更新。</p>}
      {waiting && <p role="status">下载完成，正在等待学习和资料任务结束，随后自动安装。</p>}
      {installing && <p role="status">正在退出并安装更新，完成后将重新打开应用。</p>}
      {info?.installResult && <p role="status">{info.installResult}</p>}
      {(info?.error || query.isError || action.isError) && <p role="alert">{action.error?.message || info?.error || query.error?.message}</p>}
      {info?.releaseNotes && <details><summary>查看更新说明</summary><div className={s.releaseNotes}>{info.releaseNotes}</div></details>}
    </div>
    <div className={s.settingsActions}>
      <button disabled={busy} onClick={() => action.mutate('check')}>{action.isPending && action.variables === 'check' ? '正在检查…' : '检查更新'}</button>
      {info?.available && info.canInstall && !installing && !downloading && !waiting && <button disabled={action.isPending || !info.downloadUrl} onClick={() => action.mutate('start')}>{phase === 'ready' ? '重试更新' : '立即更新'}</button>}
      {(downloading || waiting) && <button disabled={action.isPending} onClick={() => action.mutate('cancel')}>{waiting ? '取消更新' : '取消下载'}</button>}
      {info?.available && !info.downloadUrl && !info.error && <span>当前系统安装包尚未发布</span>}
      <a href={info?.downloadUrl || 'https://gitee.com/szu-andrew/learn-with-ai/releases'} target="_blank" rel="noreferrer">{info?.multipart ? 'Gitee 安装说明' : info?.downloadUrl ? '下载安装包' : 'Gitee 发布页'}</a>
    </div>
  </section>;
}
