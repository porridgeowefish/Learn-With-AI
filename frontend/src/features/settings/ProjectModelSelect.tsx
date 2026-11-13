import { useQuery } from '@tanstack/react-query';
import { Link, useLocation } from 'react-router-dom';
import { cloudModelLabel, managedProviderPrefix, readCloudStatus, useCloudModelCatalog } from '@/features/cloud-account';
import { useAskAiSettings } from './askAi';

export const defaultProjectModel = `${managedProviderPrefix}glm-5.3`;

export function useProjectModelOptions(open: boolean, value: string) {
  const status = useQuery({ queryKey: ['cloud-account'], queryFn: readCloudStatus, enabled: open, retry: false });
  const signedIn = !!status.data?.signedIn;
  const cloud = useCloudModelCatalog(open);
  const settings = useAskAiSettings();
  const local = (settings.data?.providers ?? []).filter(p => p.id && p.model && (p.kind === 'siwc' || p.baseURL && (p.apiKey || p.apiKeyEnv)));
  const models = cloud.data?.models ?? [];
  const selectedCloud = value.startsWith(managedProviderPrefix);
  const needsLogin = selectedCloud && !status.isLoading && !signedIn;
  const canUse = selectedCloud ? signedIn && models.some(model => managedProviderPrefix + model.id === value)
    : local.some(provider => provider.id === value);
  return { models, local, needsLogin, canUse, loading: status.isLoading || cloud.isLoading || settings.isLoading,
    catalogError: cloud.isError, refreshCatalog: cloud.refetch };
}

export function ProjectModelSelect({ id, value, onChange, options }: {
  id: string; value: string; onChange: (id: string) => void; options: ReturnType<typeof useProjectModelOptions>;
}) {
  const { pathname } = useLocation();
  const selectedListed = options.models.some(model => managedProviderPrefix + model.id === value) || options.local.some(provider => provider.id === value);
  return <>
    <label htmlFor={id}>模型</label>
    <select id={id} value={value} onChange={event => onChange(event.target.value)} aria-describedby={options.canUse ? undefined : `${id}-hint`}>
      {!selectedListed && <option value={value}>{value === defaultProjectModel ? 'GLM-5.3' : '已选模型（不可用）'}</option>}
      {!!options.models.length && <optgroup label="系统模型">
        {options.models.map(model => <option value={managedProviderPrefix + model.id} key={model.id}>{cloudModelLabel(model)}</option>)}
      </optgroup>}
      {!!options.local.length && <optgroup label="本机连接">
        {options.local.map(provider => <option value={provider.id} key={provider.id}>{provider.name || provider.model}</option>)}
      </optgroup>}
    </select>
    {options.catalogError && <button type="button" onClick={() => void options.refreshCatalog()}>云端模型读取失败，点击重试</button>}
    {!options.canUse && <div id={`${id}-hint`}>
      {options.needsLogin ? <>使用系统模型请先<Link to="/login" state={{ next: pathname }}>登录</Link>，或<Link to="/settings?section=models">配置自己的 API</Link>。</>
        : options.loading ? '正在读取可用模型…' : <>当前模型不可用，请选择其他模型或<Link to="/settings?section=models">检查模型连接</Link>。</>}
    </div>}
  </>;
}
