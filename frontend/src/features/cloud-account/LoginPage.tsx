import { useState, type FormEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { GitHubLogoIcon, EnvelopeClosedIcon, ArrowLeftIcon } from '@radix-ui/react-icons';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { Button } from '@/shared/primitive/Button';
import { CredentialSetupForm } from './CredentialSetupForm';
import { cloudAction, GitHubEmailConfirmation, githubLogin, googleLogin, type Account, type CloudStatus } from './api';
import googleLogo from './google-g.png';
import s from './LoginPage.module.css';

const skipKey = (accountId: string) => `lwa:credential-setup-skipped:${accountId}`;

function didSkipSetup(accountId: string) {
  try { return localStorage.getItem(skipKey(accountId)) === '1'; }
  catch { return false; }
}

export function LoginPage() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const requested = location.state?.next;
  const next = typeof requested === 'string' && (requested === '/operations' || /^\/project\/[^/]+(?:\/[^/]+)?$/.test(requested))
    ? requested : '/';
  const [mode, setMode] = useState<'password' | 'email' | 'reset' | 'github-email'>('password');
  const [setupAccountId, setSetupAccountId] = useState<string>();
  const [githubFlow, setGitHubFlow] = useState('');
  const [email, setEmail] = useState('');
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const reset = mode === 'reset';
  const usesEmail = mode === 'email' || reset;
  const confirmsGitHub = mode === 'github-email';
  const purpose = reset ? 'reset' : 'login';

  function showMode(nextMode: typeof mode) {
    setMode(nextMode);
    setMessage('');
  }

  function accept(account: Account, offerSetup = false) {
    qc.setQueryData<CloudStatus>(['cloud-account'], { signedIn: true, account });
    void qc.invalidateQueries({ queryKey: ['cloud-account'] });
    if (offerSetup && account.id && !account.name && !didSkipSetup(account.id)) {
      setSetupAccountId(account.id);
      setMessage('');
      return;
    }
    navigate(next, { replace: true });
  }

  async function perform(task: () => Promise<void>) {
    if (busy) return;
    setMessage('');
    setBusy(true);
    try { await task(); }
    catch (error) { setMessage((error as Error).message); }
    finally { setBusy(false); }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const fields = { username: data.get('username'), password: data.get('password') };
    if (reset && fields.password !== data.get('confirmPassword')) {
      setMessage('两次输入的密码不一致。');
      return;
    }
    void perform(async () => {
      if (confirmsGitHub) {
        accept(await cloudAction<Account>('github-finish', { flowId: githubFlow, code: data.get('code') }), true);
      } else if (usesEmail) {
        accept(await cloudAction<Account>('verify-email', {
          email: email.trim(),
          code: data.get('code'),
          purpose,
          ...(reset && { password: fields.password }),
        }), !reset);
      } else {
        accept(await cloudAction<Account>('password', fields));
      }
    });
  }

  function sendCode() {
    if (confirmsGitHub) {
      void perform(async () => {
        const result = await cloudAction<{ exampleCode?: string }>('github-send-code', { flowId: githubFlow });
        setMessage(result.exampleCode
          ? `验证码为 ${result.exampleCode}。`
          : '验证码已发送到现有账号的邮箱。收件箱没有收到时，请查看垃圾邮件。');
      });
      return;
    }
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) {
      setMessage('请输入有效的邮箱地址。');
      return;
    }
    void perform(async () => {
      const result = await cloudAction<{ exampleCode?: string }>('send-code', { email: email.trim(), purpose });
      setMessage(result.exampleCode
        ? `验证码为 ${result.exampleCode}。`
        : '验证码已发送。如果收件箱没有收到，请查看垃圾邮件。');
    });
  }

  function oauth(provider: 'github' | 'google') {
    const waiting = () => setMessage('请在浏览器完成授权。');
    void perform(async () => {
      if (provider === 'google') setMessage('正在打开 Google 授权…');
      try { accept(await (provider === 'github' ? githubLogin(false, waiting) : googleLogin(waiting)), true); }
      catch (error) {
        if (!(error instanceof GitHubEmailConfirmation)) throw error;
        setGitHubFlow(error.flowId);
        showMode('github-email');
      }
    });
  }

  return (
    <main className={s.page}>
      <Link className={s.back} to="/"><ArrowLeftIcon /> 返回学习</Link>
      <section className={s.card}>
        <header className={s.header}>
          <div className={s.brand}><img src="/app-icon.png" alt="" /> Learn-With-AI</div>
          <h1>{setupAccountId ? '设置用户名和密码' : reset ? '找回密码' : confirmsGitHub ? '验证邮箱' : usesEmail ? '邮箱验证码登录' : '登录'}</h1>
          {setupAccountId && <p className={s.hint}>设置后可用用户名和密码登录。也可以稍后在「设置 → 个人信息」添加。</p>}
          {!setupAccountId && reset && <p className={s.hint}>请输入已绑定到账号的邮箱。</p>}
          {!setupAccountId && confirmsGitHub && <p className={s.hint}>该邮箱已有 Learn-With-AI 账号。验证后可继续使用原账号和积分。</p>}
        </header>
        {setupAccountId ? <CredentialSetupForm accountId={setupAccountId} dismissLabel="跳过"
          onSaved={accept} onDismiss={() => {
            try { localStorage.setItem(skipKey(setupAccountId), '1'); }
            catch { /* A storage restriction should not prevent continuing. */ }
            navigate(next, { replace: true });
          }} /> : <>
        <form className={s.form} onSubmit={submit} key={mode}>
          {!usesEmail && !confirmsGitHub && <>
            <label>用户名<input name="username" autoComplete="username" required /></label>
            <label>密码<input type="password" name="password" autoComplete="current-password" required /></label>
          </>}
          {usesEmail && <label>邮箱<input type="email" name="email" autoComplete="email" required value={email} onChange={event => setEmail(event.target.value)} placeholder="QQ 邮箱或 Gmail" /></label>}
          {(usesEmail || confirmsGitHub) &&
            <div className={s.code}>
              <label>六位验证码<input name="code" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} required /></label>
              <Button type="button" className={s.codeButton} disabled={busy} onClick={sendCode}>发送验证码</Button>
            </div>
          }
          {reset && <label>新密码<input type="password" name="password" autoComplete="new-password" minLength={6} required /></label>}
          {reset && <label>确认密码<input type="password" name="confirmPassword" autoComplete="new-password" required /></label>}
          <Button type="submit" className={s.submit} variant="outline" loading={busy}>{reset ? '重设密码并登录' : '登录'}</Button>
          {mode === 'password' && <button type="button" className={`${s.textLink} ${s.recoveryLink}`} disabled={busy}
            onClick={() => showMode('reset')}>忘记密码？</button>}
        </form>
        {mode !== 'password' && <button type="button" className={`${s.textLink} ${s.dismissLink}`} disabled={busy} onClick={() => showMode('password')}>返回登录</button>}
        {mode === 'password' && <>
          <div className={s.divider}><span id="other-login-methods">其他登录方式</span></div>
          <div className={s.providers} role="group" aria-labelledby="other-login-methods">
            <Button type="button" className={s.provider} title="Google 授权登录"
              aria-label="Google 授权登录" disabled={busy} onClick={() => oauth('google')}>
              <img src={googleLogo} alt="" />
            </Button>
            <Button type="button" className={s.provider} title="GitHub 授权登录"
              aria-label="GitHub 授权登录" disabled={busy} onClick={() => oauth('github')}>
              <GitHubLogoIcon aria-hidden="true" />
            </Button>
            <Button type="button" className={s.provider} title="邮箱验证码登录"
              aria-label="邮箱验证码登录" disabled={busy} onClick={() => showMode('email')}>
              <EnvelopeClosedIcon aria-hidden="true" />
            </Button>
          </div>
        </>}
        {message && <p className={s.notice} role="status">{message}</p>}
        </>}
      </section>
    </main>
  );
}
