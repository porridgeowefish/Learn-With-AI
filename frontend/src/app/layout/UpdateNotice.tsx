import { useUpdateInfo } from '@/features/settings/update';
import { Link } from 'react-router-dom';
import s from './UpdateNotice.module.css';

export function UpdateNotice() {
  const { data: info } = useUpdateInfo();
  if (!info) return null;
  return <div className={s.root}>
    {info.available && <div className={s.card} role="status">
      <strong>发现新版本 {info.latest}</strong>
      <div className={s.actions}><Link className={s.primary} to="/settings#version-data">{info.phase === 'ready' ? '安装并重启' : info.phase === 'downloading' ? '查看下载进度' : '查看更新'}</Link></div>
    </div>}
    <span className={s.version}>v{info.current}</span>
  </div>;
}
