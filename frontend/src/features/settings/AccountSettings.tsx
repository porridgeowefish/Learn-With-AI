import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { readCloudDailyHistory, readCloudStatus } from '@/features/cloud-account';
import s from './SettingsPage.module.css';

const points = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 3 });

export function AccountSettings() {
  const status = useQuery({ queryKey: ['cloud-account'], queryFn: readCloudStatus, retry: false });
  const account = status.data?.account;
  const history = useQuery({
    queryKey: ['cloud-daily-history', account?.id || account?.email || account?.name],
    queryFn: readCloudDailyHistory, enabled: !!account, retry: false,
  });
  if (status.isPending) return <p role="status">正在读取账号…</p>;
  if (status.isError) return <p role="alert">暂时无法读取账号信息。</p>;
  if (!account) return <section className={s.appearance}>
    <h3>登录后查看个人信息</h3>
    <p>登录 Learn-With-AI 可查看积分余额和签到记录。</p>
    <Link to="/login">登录 / 注册</Link>
  </section>;
  const wallet = status.data.wallet;
  const balance = ((wallet?.balance ?? account.balance) - (wallet?.held ?? 0)) / 1000;
  return <div className={s.accountDetails}>
    <section aria-labelledby="profile-title">
      <h3 id="profile-title">账号信息</h3>
      <dl className={s.accountFacts}>
        <div><dt>账号</dt><dd>{account.name || '邮箱或 GitHub 登录'}</dd></div>
        <div><dt>邮箱</dt><dd>{account.email || '未绑定'}</dd></div>
        <div><dt>GitHub</dt><dd>{account.githubLinked ? '已绑定' : '未绑定'}</dd></div>
      </dl>
    </section>
    <section aria-labelledby="account-points-title">
      <h3 id="account-points-title">积分与签到</h3>
      <dl className={s.accountFacts}>
        <div><dt>可用积分</dt><dd><strong>{points.format(balance)} 积分</strong></dd></div>
        <div><dt>今日签到</dt><dd>{status.data.dailyClaimed ? '已领取 100 积分' : '未领取'}</dd></div>
      </dl>
      <p className={s.subtitle}>最近 30 天 · 北京时间</p>
      {history.isPending && <p role="status">正在读取签到记录…</p>}
      {history.isError && <p role="alert">{history.error.message}</p>}
      {history.data && (history.data.entries.length ? <ul className={s.accountFacts} aria-label="签到记录">
        {history.data.entries.map(entry => <li key={entry.day}><time dateTime={entry.day}>{entry.day}</time><span>+{points.format(entry.units / 1000)} 积分</span></li>)}
      </ul> : <p className={s.subtitle}>最近 30 天没有签到记录。</p>)}
    </section>
  </div>;
}
