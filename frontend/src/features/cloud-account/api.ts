import { API_BASE } from '@/shared/lib/constants';

export type Wallet = { balance: number; held: number; granted?: boolean };
export type Account = { id?: string; email: string; name?: string; balance: number; githubLinked?: boolean };
export type CloudStatus = { signedIn: boolean; account?: Account; wallet?: Wallet; selectedModel?: string; dailyClaimed?: boolean };
export type Model = { id: string; name: string };
export type DailyHistory = { today: string; entries: { day: string; units: number }[] };

export const managedProviderPrefix = 'lwa-cloud:';

export function normalizeManagedProviderId(id: string) {
  const legacyPrefix = 'lll-cloud:';
  return id.startsWith(legacyPrefix) ? managedProviderPrefix + id.slice(legacyPrefix.length) : id;
}

const errors: Record<string, string> = {
  invalid_account_input: '请检查输入后重试。',
  invalid_credentials: '账号、密码或验证码有误，也可能已过期。',
  code_cooldown: '验证码刚刚发送，请稍后再试。',
  authentication_rate_limited: '操作过于频繁，请稍后再试。',
  account_conflict: '此邮箱或用户名已注册，请使用已有账号登录。',
  email_unavailable: '暂时无法发送验证码，请稍后再试。',
  github_link_required: '请先用已有账号登录，再绑定 GitHub。',
  github_verified_email_required: '请先验证 GitHub 账号的主邮箱，或登录 Learn-With-AI 后绑定 GitHub。',
  github_authorization_failed: 'GitHub 授权未完成。',
  github_flow_expired: '本次 GitHub 授权已过期，请重新发起。',
  login_required: '请先登录。',
};

async function read<T>(path: string, body?: object): Promise<T> {
  const res = await fetch(API_BASE + path, body ? {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  } : { cache: 'no-store' });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    if (path === '/api/cloud/unbind-email' || path === '/api/cloud/unbind-github') {
      if (data.error === 'invalid_credentials') throw new Error('当前密码有误，请重试。');
      if (data.error === 'account_conflict') throw new Error('此登录方式已解绑，或当前账号未设置密码。');
    }
    throw new Error(errors[data.error] ?? '暂时无法连接账号服务，请稍后重试。');
  }
  return data as T;
}

export const readCloudStatus = () => read<CloudStatus>('/api/cloud/account');
export const readCloudModels = () => read<{ models: Model[] }>('/api/cloud/models');
export const readCloudGuestModels = () => read<{ models: Model[] }>('/api/cloud/models/guest');
export const readCloudDailyHistory = () => read<DailyHistory>('/api/cloud/daily-history');
export const cloudAction = <T>(name: string, body: object = {}) => read<T>(`/api/cloud/${name}`, body);

export async function githubLogin(link: boolean, onWaiting: () => void): Promise<Account> {
  const desktop = window as Window & { lllOpenExternal?: (url: string) => Promise<void> };
  const popup = desktop.lllOpenExternal ? null : window.open('about:blank', '_blank');
  if (!desktop.lllOpenExternal && !popup) throw new Error('请允许弹出登录窗口后重试。');
  try {
    const flow = await cloudAction<{ authorizationUrl: string; flowId: string }>('github-start', { link });
    const url = new URL(flow.authorizationUrl);
    if (url.origin !== 'https://github.com' || url.pathname !== '/login/oauth/authorize') throw new Error('GitHub 授权地址无效。');
    if (desktop.lllOpenExternal) await desktop.lllOpenExternal(url.href);
    else if (popup) { popup.opener = null; popup.location.replace(url.href); }
    onWaiting();
    const deadline = Date.now() + 10 * 60 * 1000;
    while (Date.now() < deadline) {
      const result = await cloudAction<Account & { pending?: boolean }>('github-finish', { flowId: flow.flowId });
      if (!result.pending) return result;
      await new Promise(resolve => setTimeout(resolve, 2000));
    }
    throw new Error('授权超时，请重新发起。');
  } catch (error) {
    popup?.close();
    throw error;
  }
}
