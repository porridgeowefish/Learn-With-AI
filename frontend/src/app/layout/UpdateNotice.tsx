import { useUpdateInfo } from '@/features/settings/update';
import { Link } from 'react-router-dom';
import s from './UpdateNotice.module.css';

export function UpdateNotice() {
  const { data: info } = useUpdateInfo();
  if (!info) return null;
  return <div className={s.root}>
    {info.available && <div className={s.card} role="status">
      <strong>发现新版本 {info.latest}</strong>
      <div className={s.actions}><Link className={s.primary} to="/settings?section=about#version-data">{info.phase === 'ready' ? '重试更新' : ['downloading', 'waiting', 'installing'].includes(info.phase ?? '') ? '查看更新进度' : '查看更新'}</Link></div>
    </div>}
    <span className={s.version}>v{info.current}</span>
  </div>;
}
