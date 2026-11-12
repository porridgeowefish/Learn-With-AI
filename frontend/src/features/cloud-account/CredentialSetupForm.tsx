import { useState, type FormEvent } from 'react';
import { Button } from '@/shared/primitive/Button';
import { cloudAction, type Account } from './api';
import s from './LoginPage.module.css';

export function CredentialSetupForm({ accountId, onSaved, onDismiss, dismissLabel }: {
  accountId: string;
  onSaved: (account: Account) => void;
  onDismiss: () => void;
  dismissLabel: string;
}) {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    const data = new FormData(event.currentTarget);
    const password = data.get('password');
    const confirmPassword = data.get('confirmPassword');
    if (password !== confirmPassword) {
      setMessage('两次输入的密码不一致。');
      return;
    }
    setMessage('');
    setBusy(true);
    try {
      const account = await cloudAction<Account>('set-credentials', {
        accountId, username: data.get('username'), password, confirmPassword,
      });
      onSaved(account);
    } catch (error) {
      setMessage((error as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return <form className={s.form} onSubmit={submit}>
    <label>用户名
      <input name="username" autoComplete="username" autoCapitalize="none" spellCheck={false}
        minLength={3} maxLength={32} pattern="[A-Za-z0-9][A-Za-z0-9_.\-]{2,31}"
        aria-describedby="credential-username-hint" required />
    </label>
    <p className={s.hint} id="credential-username-hint">3 至 32 个字符。用字母或数字开头，可包含点、下划线和连字符。</p>
    <label>密码<input type="password" name="password" autoComplete="new-password" minLength={6} maxLength={256} required /></label>
    <label>确认密码<input type="password" name="confirmPassword" autoComplete="new-password" required /></label>
    <Button type="submit" className={s.submit} variant="outline" loading={busy}>保存用户名和密码</Button>
    <button type="button" className={`${s.textLink} ${s.dismissLink}`} disabled={busy} onClick={onDismiss}>{dismissLabel}</button>
    {message && <p className={s.notice} role="alert">{message}</p>}
  </form>;
}
