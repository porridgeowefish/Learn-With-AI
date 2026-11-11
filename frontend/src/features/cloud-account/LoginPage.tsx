import { useState, type FormEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { GitHubLogoIcon, EnvelopeClosedIcon, ArrowLeftIcon } from '@radix-ui/react-icons';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { Button } from '@/shared/primitive/Button';
import { cloudAction, githubLogin, type Account, type CloudStatus } from './api';
import s from './LoginPage.module.css';

export function LoginPage() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const requested = location.state?.next;
  const next = typeof requested === 'string' && (requested === '/operations' || /^\/project\/[^/]+(?:\/[^/]+)?$/.test(requested))
    ? requested : '/';
  const [mode, setMode] = useState<'password' | 'email' | 'register' | 'reset'>('password');
  const [email, setEmail] = useState('');
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const register = mode === 'register';
  const reset = mode === 'reset';
  const usesEmail = mode === 'email' || reset;
  const purpose = reset ? 'reset' : 'login';

  function showMode(nextMode: typeof mode) {
    setMode(nextMode);
    setMessage('');
  }

  function accept(account: Account) {
    qc.setQueryData<CloudStatus>(['cloud-account'], { signedIn: true, account });
    void qc.invalidateQueries({ queryKey: ['cloud-account'] });
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
    if ((register || reset) && fields.password !== data.get('confirmPassword')) {
      setMessage('两次输入的密码不一致。');
      return;
    }
    void perform(async () => {
      if (register) {
        accept(await cloudAction<Account>('register', { ...fields, confirmPassword: data.get('confirmPassword') }));
      } else if (usesEmail) {
        accept(await cloudAction<Account>('verify-email', {
          email: email.trim(),
          code: data.get('code'),
          purpose,
          ...(reset && { password: fields.password }),
        }));
      } else {
        accept(await cloudAction<Account>('password', fields));
      }
    });
  }

  function sendCode() {
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

  function oauth() {
    const waiting = () => setMessage('请在浏览器完成授权。');
    void perform(async () => {
      accept(await githubLogin(false, waiting));
    });
  }

  return (
    <main className={s.page}>
      <Link className={s.back} to="/"><ArrowLeftIcon /> 返回学习</Link>
      <section className={s.card}>
        <header className={s.header}>
          <div className={s.brand}><img src="/app-icon.png" alt="" /> Learn-With-AI</div>
          <h1>{register ? '注册' : reset ? '找回密码' : usesEmail ? '邮箱登录' : '登录'}</h1>
          {reset && <p className={s.hint}>请输入已绑定到账号的邮箱。</p>}
        </header>
        <form className={s.form} onSubmit={submit} key={mode}>
          {!usesEmail && <>
            <label>账号<input name="username" autoComplete="username" required /></label>
            <label>密码<input type="password" name="password" autoComplete={register ? 'new-password' : 'current-password'} minLength={register ? 6 : undefined} required /></label>
          </>}
          {mode === 'password' && <button type="button" className={`${s.textLink} ${s.recoveryLink}`} disabled={busy}
            onClick={() => showMode('reset')}>忘记密码？</button>}
          {usesEmail && <>
            <label>邮箱<input type="email" name="email" autoComplete="email" required value={email} onChange={event => setEmail(event.target.value)} placeholder="QQ 邮箱或 Gmail" /></label>
            <div className={s.code}>
              <label>六位验证码<input name="code" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} required /></label>
              <Button type="button" className={s.codeButton} disabled={busy} onClick={sendCode}>发送验证码</Button>
            </div>
          </>}
          {reset && <label>新密码<input type="password" name="password" autoComplete="new-password" minLength={6} required /></label>}
          {(register || reset) && <label>确认密码<input type="password" name="confirmPassword" autoComplete="new-password" required /></label>}
          <Button type="submit" className={s.submit} variant="outline" loading={busy}>{register ? '注册并登录' : reset ? '重设密码并登录' : '登录'}</Button>
        </form>
        <div className={s.registration}>
          {mode === 'password' && <span>没有账号？</span>}
          <button type="button" className={s.textLink} disabled={busy} onClick={() => showMode(mode === 'password' ? 'register' : 'password')}>
            {mode === 'password' ? '注册' : '返回登录'}
          </button>
        </div>
        {mode === 'password' && <>
          <div className={s.divider}><span>其他登录方式</span></div>
          <div className={s.providers}>
            <Button type="button" className={s.provider} iconLeft={<EnvelopeClosedIcon />} aria-label="邮箱验证码登录" disabled={busy}
              onClick={() => showMode('email')}>邮箱</Button>
            <Button type="button" className={s.provider} iconLeft={<GitHubLogoIcon />} aria-label="GitHub 授权登录" disabled={busy} onClick={oauth}>GitHub</Button>
          </div>
        </>}
        {message && <p className={s.notice} role="status">{message}</p>}
      </section>
    </main>
  );
}
