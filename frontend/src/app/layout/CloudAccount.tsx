import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { EnvelopeClosedIcon, GitHubLogoIcon, PersonIcon } from '@radix-ui/react-icons';
import { cloudAction as action, githubLogin, readCloudDailyHistory, readCloudStatus, type Account, type CloudStatus as Status, type Wallet } from '@/features/cloud-account/api';
import { Button } from '@/shared/primitive/Button';
import { Modal } from '@/shared/primitive/Modal';
import s from './CloudAccount.module.css';
const points = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 3 });
type Identity = 'email' | 'github';
type IdentityView = 'bind-email' | Identity;

export function CloudAccount() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');
  const [identityView, setIdentityView] = useState<IdentityView>();
  const [showHistory, setShowHistory] = useState(false);
  const status = useQuery({ queryKey: ['cloud-account'], queryFn: readCloudStatus, retry: false });
  const account = status.data?.account;
  const history = useQuery({
    queryKey: ['cloud-daily-history', account?.id || account?.email || account?.name], queryFn: readCloudDailyHistory,
    enabled: open && showHistory && !!account, retry: false,
  });
  const wallet = status.data?.wallet;
  const claimed = !!status.data?.dailyClaimed;
  const showEmailBinding = identityView === 'bind-email' && !!account?.name && !account.email;
  const unlinkProvider = account?.name && (identityView === 'email' || identityView === 'github')
    ? identityView : undefined;
  const unlinkTitle = unlinkProvider === 'email' ? '解绑邮箱' : '解绑 GitHub';
  const unlinkDescription = unlinkProvider === 'email'
    ? '解绑后，此邮箱无法登录当前账号。其他设备需要重新登录。'
    : '解绑后，此 GitHub 账号无法登录当前账号。其他设备需要重新登录。';
  const name = account?.name || account?.email.split('@')[0] || '学习者';
  const balance = ((wallet?.balance ?? account?.balance ?? 0) - (wallet?.held ?? 0)) / 1000;
  function updateAccount(user: Account) {
    qc.setQueryData<Status>(['cloud-account'], current => current?.signedIn && current.account?.id === user.id
      ? { ...current, account: user }
      : current);
    void qc.invalidateQueries({ queryKey: ['cloud-account'] });
  }

  async function perform(operation: string, callback: () => Promise<void>) {
    if (busy) return;
    setBusy(operation);
    setMessage('');
    try { await callback(); }
    catch (error) { setMessage((error as Error).message); }
    finally { setBusy(''); }
  }

  function bindGitHub() {
    void perform('github', async () => {
      updateAccount(await githubLogin(true, () => setMessage('请在浏览器完成 GitHub 授权。')));
      setMessage('GitHub 已绑定。');
    });
  }

  function showIdentity(view?: IdentityView) {
    setIdentityView(view);
    setMessage('');
  }

  return (
    <>
      <button type="button" className={s.trigger} aria-haspopup={account ? 'dialog' : undefined} onClick={() => {
        if (!account) { navigate('/login'); return; }
        setOpen(true); void status.refetch();
      }}>
        <span className={s.avatar} aria-hidden="true">{account ? Array.from(name)[0]?.toUpperCase() : <PersonIcon />}</span>
        <span className={s.accountName}>{account ? name : '登录'}</span>
      </button>
      {account && <Modal open={open} onOpenChange={next => {
        setOpen(next);
        if (!next) { showIdentity(); setShowHistory(false); }
      }} size="sm" title={showEmailBinding ? '绑定邮箱' : unlinkProvider ? unlinkTitle : '我的账号'}
        description={showEmailBinding ? '绑定后可以用邮箱验证码登录同一个账号。' :
          unlinkProvider ? unlinkDescription : name}>
          {showEmailBinding ? <EmailBindingForm busy={!!busy} perform={perform} onCancel={() => showIdentity()}
            onSent={() => setMessage('验证码已发送，请查收邮件。')} onBound={linked => {
            updateAccount(linked);
            setIdentityView(undefined);
            setMessage('邮箱已绑定。');
          }} /> : unlinkProvider ? <UnlinkForm key={unlinkProvider} title={unlinkTitle} busy={!!busy}
            requirePassword={unlinkProvider === 'github'}
            onCancel={() => showIdentity()} onSubmit={password => {
              void perform('unlink', async () => {
                updateAccount(await action<Account>(`unbind-${unlinkProvider}`, unlinkProvider === 'github' ? { password } : {}));
                setIdentityView(undefined);
                setMessage(unlinkProvider === 'email' ? '邮箱已解绑。' : 'GitHub 已解绑。');
              });
            }} /> : <div className={s.account}>
            <div className={s.wallet}>
              <div>
                <p className={s.caption}>可用积分</p>
                <strong>{balance.toLocaleString('zh-CN', { maximumFractionDigits: 3 })}</strong>
              </div>
              <Button variant={claimed ? 'outline' : 'primary'} disabled={claimed || !!busy} onClick={() => void perform('claim', async () => {
                const next = await action<Wallet>('claim');
                qc.setQueryData<Status>(['cloud-account'], current => current && { ...current, wallet: next, dailyClaimed: true });
                void qc.invalidateQueries({ queryKey: ['cloud-daily-history'] });
              })}>
                {claimed ? '今日已领取' : '领取 100 积分'}
              </Button>
            </div>
            {<div className={s.checkIns}>
              <Button variant="ghost" className={s.signOut} aria-expanded={showHistory} onClick={() => setShowHistory(!showHistory)}>签到记录</Button>
              {showHistory && <section aria-label="最近 30 天签到记录">
                <p className={s.caption}>最近 30 天 · 北京时间</p>
                {history.isPending && <p className={s.caption}>正在读取签到记录…</p>}
                {history.isError && <p className={s.caption} role="alert">{history.error.message}</p>}
                {history.data && (history.data.entries.length ? <ul className={s.claimHistory}>
                  {history.data.entries.map(entry => <li key={entry.day}><time dateTime={entry.day}>{entry.day}</time>
                    <span>+{points.format(entry.units / 1000)} 积分</span></li>)}
                </ul> : <p className={s.caption}>最近 30 天没有签到记录。</p>)}
              </section>}
            </div>}
            {<section className={s.identities} aria-labelledby="account-identities-title">
              <h3 id="account-identities-title">登录方式</h3>
              <div className={s.identityRow}>
                <EnvelopeClosedIcon aria-hidden="true" />
                <div><strong>邮箱</strong><p className={s.caption}>{account.email || '未绑定'}</p></div>
                {account.name && (account.email
                  ? <Button size="sm" variant="ghost" disabled={!!busy} onClick={() => showIdentity('email')}>解绑邮箱</Button>
                  : <Button size="sm" variant="outline" disabled={!!busy} onClick={() => showIdentity('bind-email')}>绑定邮箱</Button>)}
              </div>
              <div className={s.identityRow}>
                <GitHubLogoIcon aria-hidden="true" />
                <div><strong>GitHub</strong><p className={s.caption}>{account.githubLinked ? '已绑定' : '未绑定'}</p></div>
                {account.githubLinked ? (account.name && <Button size="sm" variant="ghost" disabled={!!busy}
                  onClick={() => showIdentity('github')}>解绑 GitHub</Button>) :
                  <Button size="sm" variant="outline" disabled={!!busy} onClick={bindGitHub}>绑定 GitHub</Button>}
              </div>
              {!account.name && <p className={s.caption}>解绑需要已设置的账号密码。</p>}
            </section>}
            <Button variant="ghost" className={s.signOut} disabled={!!busy} onClick={() => void perform('logout', async () => {
              await action('logout');
              qc.removeQueries({ queryKey: ['cloud-daily-history'] });
              qc.setQueryData<Status>(['cloud-account'], { signedIn: false });
              setOpen(false);
              setShowHistory(false);
            })}>退出登录</Button>
          </div>}
        {message && <p className={s.caption} role="status">{message}</p>}
      </Modal>}
    </>
  );
}

function UnlinkForm({ title, busy, requirePassword, onCancel, onSubmit }: {
  title: string;
  busy: boolean;
  requirePassword: boolean;
  onCancel: () => void;
  onSubmit: (password: string) => void;
}) {
  const passwordInput = useRef<HTMLInputElement>(null);
  const cancelButton = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (requirePassword) passwordInput.current?.focus();
    else cancelButton.current?.focus();
  }, [requirePassword]);

  return <form className={s.binding} aria-label={title} onSubmit={event => {
    event.preventDefault();
    onSubmit(String(new FormData(event.currentTarget).get('password') ?? ''));
  }}>
    {requirePassword && <label className={s.field}>当前密码<input ref={passwordInput} name="password" type="password"
      autoComplete="current-password" required disabled={busy} /></label>}
    <div className={s.bindingActions}>
      <Button ref={cancelButton} type="button" variant="ghost" disabled={busy} onClick={onCancel}>取消</Button>
      <Button type="submit" variant="danger" loading={busy}>确认解绑</Button>
    </div>
  </form>;
}

function EmailBindingForm({ busy, perform, onCancel, onSent, onBound }: {
  busy: boolean;
  perform: (operation: string, callback: () => Promise<void>) => Promise<void>;
  onCancel: () => void;
  onSent: () => void;
  onBound: (account: Account) => void;
}) {
  const emailInput = useRef<HTMLInputElement>(null);
  const [sentTo, setSentTo] = useState('');
  useEffect(() => { emailInput.current?.focus(); }, []);

  function sendCode() {
    const field = emailInput.current;
    if (!field?.reportValidity()) return;
    const email = field.value;
    void perform('send-binding-email', async () => {
      await action('send-binding-email', { email });
      setSentTo(email);
      onSent();
    });
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const fields = new FormData(event.currentTarget);
    if (fields.get('email') !== sentTo) return;
    void perform('bind-email', async () => {
      onBound(await action<Account>('bind-email', {
        email: fields.get('email'), code: fields.get('code'), password: fields.get('password'),
      }));
    });
  }

  return <form className={s.binding} onSubmit={submit} aria-label="绑定邮箱">
    <label className={s.field}>邮箱<input ref={emailInput} name="email" type="email" autoComplete="email" required
      disabled={busy} onChange={() => setSentTo('')} /></label>
    <div className={s.code}>
      <label className={s.field}>六位验证码<input name="code" inputMode="numeric" autoComplete="one-time-code"
        pattern="[0-9]{6}" maxLength={6} required disabled={busy} /></label>
      <Button type="button" variant="outline" disabled={busy} onClick={sendCode}>发送验证码</Button>
    </div>
    <label className={s.field}>当前密码<input name="password" type="password" autoComplete="current-password" required disabled={busy} /></label>
    <div className={s.bindingActions}>
      <Button type="button" variant="ghost" disabled={busy} onClick={onCancel}>取消</Button>
      <Button type="submit" variant="primary" disabled={busy || !sentTo}>确认绑定</Button>
    </div>
  </form>;
}
