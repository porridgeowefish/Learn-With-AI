import { useUpdateInfo } from '@/features/settings/update';
import s from './UpdateNotice.module.css';

export function UpdateNotice() {
  const { data: info } = useUpdateInfo();
  if (!info) return null;
  return <div className={s.root}>
    {info.available && <div className={s.card} role="status">
      <strong>发现新版本 {info.latest}</strong>
      <div className={s.actions}><a className={s.primary} href="https://github.com/porridgeowefish/Learn-With-AI/releases/latest" target="_blank" rel="noreferrer">去 GitHub 下载</a></div>
    </div>}
    <span className={s.version}>v{info.current}</span>
  </div>;
}
