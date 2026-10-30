import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { API_BASE } from '@/shared/lib/constants';
import { Button } from '@/shared/primitive/Button';
import type { AskConfig, AskProvider } from './askAi';
import s from './ModelsPage.module.css';

const siwcProviderID = 'chatgpt-siwc';
interface Status { connected: boolean; email?: string; error?: string; pending: boolean }

export function SIWCConnection({ config, save }: { config: AskConfig; save: (next: AskConfig) => void }) {
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const status = useQuery<Status>({ queryKey: ['siwc', 'status'], queryFn: async () => {
    const res = await fetch(`${API_BASE}/api/settings/siwc`, { cache: 'no-store' });
    if (!res.ok) throw new Error('连接状态读取失败');
    return res.json() as Promise<Status>;
  }, refetchInterval: 2000, retry: false });
  const models = useQuery<{models:string[]}>({ queryKey: ['siwc', 'models'], enabled: !!status.data?.connected, queryFn: async () => {
    const res = await fetch(`${API_BASE}/api/settings/siwc/models`, { cache: 'no-store' });
    if (!res.ok) throw new Error('模型目录读取失败');
    return res.json() as Promise<{models:string[]}>;
  }, staleTime: 5*60*1000, retry: false });
  const active = config.providers.find(p => p.id === siwcProviderID);
  const connect = async () => {
    const desktop = window as Window & { lllOpenExternal?: (url: string) => Promise<void> };
    const popup = desktop.lllOpenExternal ? null : window.open('about:blank', '_blank');
    if (!desktop.lllOpenExternal && !popup) { setError('浏览器阻止了登录窗口，请允许弹窗后重试。'); return; }
    setBusy(true); setError('');
    try {
      const res = await fetch(`${API_BASE}/api/settings/siwc/start`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
      if (!res.ok) throw new Error('无法开始 ChatGPT 登录');
      const body = await res.json() as { authorizationUrl: string };
      const auth = new URL(body.authorizationUrl);
      if (auth.origin !== 'https://auth.openai.com') throw new Error('登录地址无效');
      if (desktop.lllOpenExternal) await desktop.lllOpenExternal(auth.href);
      else if (popup) popup.location.href = auth.href;
      void qc.invalidateQueries({ queryKey: ['siwc', 'status'] });
    } catch (cause) { popup?.close(); setError((cause as Error).message); }
    finally { setBusy(false); }
  };
  const chooseModel = (model: string) => {
    if (!model) return;
    const provider: AskProvider = { id: siwcProviderID, kind: 'siwc', name: 'ChatGPT 套餐', baseURL: 'https://api.openai.com/v1', apiKey: '', model };
    save({ ...config, default: siwcProviderID, providers: [...config.providers.filter(p => p.id !== siwcProviderID), provider], bindings: { ...config.bindings, teacher: { providerId: siwcProviderID } } });
  };
  const disconnect = async () => {
    setBusy(true); setError('');
    try {
      const res = await fetch(`${API_BASE}/api/settings/siwc/disconnect`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
      if (!res.ok) throw new Error('断开连接失败');
      const remaining = config.providers.filter(p => p.id !== siwcProviderID);
      save({ ...config, default: config.default === siwcProviderID ? remaining[0]?.id ?? '' : config.default, providers: remaining, bindings: Object.fromEntries(Object.entries(config.bindings ?? {}).filter(([, value]) => value.providerId !== siwcProviderID)) });
      void qc.invalidateQueries({ queryKey: ['siwc'] });
    } catch (cause) { setError((cause as Error).message); }
    finally { setBusy(false); }
  };
  return <div className={s.card}>
    <strong>用 ChatGPT 套餐登录</strong>
    <p className={s.saveStatus}>在 OpenAI 页面授权后，LLL 使用你套餐中允许的模型；令牌仅保存在本机。</p>
    {status.data?.connected ? <>
      <p className={s.connected}>已连接 {status.data.email || 'ChatGPT 账户'}</p>
      <label className={s.siwcModel}>当前模型<select value={active?.model ?? ''} onChange={event => chooseModel(event.target.value)} disabled={models.isLoading || busy}>
        <option value="">选择模型</option>{models.data?.models.map(model => <option key={model} value={model}>{model}</option>)}
        {active?.model && !models.data?.models.includes(active.model) && <option value={active.model}>{active.model}</option>}
      </select></label>
      {models.isError && <p role="alert" className={s.error}>模型目录读取失败。请检查套餐权限后重试。</p>}
      <Button size="sm" variant="ghost" disabled={busy} onClick={() => void disconnect()}>断开 ChatGPT</Button>
    </> : <Button size="sm" disabled={busy || status.data?.pending} onClick={() => void connect()}>{status.data?.pending ? '等待浏览器授权…' : '继续使用 ChatGPT'}</Button>}
    {(error || status.data?.error) && <p role="alert" className={s.error}>{error || status.data?.error}</p>}
  </div>;
}
