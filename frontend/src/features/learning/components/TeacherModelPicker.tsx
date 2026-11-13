import { useRef, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { CheckIcon, ChevronDownIcon, LockClosedIcon } from '@radix-ui/react-icons';
import { cloudModelLabel, managedProviderPrefix, useCloudModelCatalog, type CloudStatus } from '@/features/cloud-account';
import s from './TeacherView.module.css';

export function TeacherModelPicker({ status, selected, localModels, onSelect }: {
  status?: CloudStatus;
  selected: string;
  localModels: { id: string; label: string }[];
  onSelect: (id: string) => void;
}) {
  const { pathname } = useLocation();
  const trigger = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const models = useCloudModelCatalog(open, selected.startsWith(managedProviderPrefix));
  const cloudModels = models.data?.models ?? [];
  const selectedCloud = cloudModels.find(model => managedProviderPrefix + model.id === selected);
  const selectedName = localModels.find(model => model.id === selected)?.label ||
    (selectedCloud && cloudModelLabel(selectedCloud)) ||
    (selected.startsWith(managedProviderPrefix) ? selected.slice(managedProviderPrefix.length) : undefined);

  function select(id: string) {
    onSelect(id);
    setOpen(false);
    trigger.current?.focus();
  }

  return <div className={s.modelPicker} onBlur={event => {
    if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false);
  }} onKeyDown={event => {
    if (event.key === 'Escape') { event.stopPropagation(); setOpen(false); trigger.current?.focus(); }
  }}>
    <button ref={trigger} className={s.modelPickerTrigger} type="button" aria-label="选择教师模型" aria-expanded={open}
      onClick={() => setOpen(!open)}>{selectedName || '选择模型'}<ChevronDownIcon /></button>
    {open && <div className={s.modelPickerPanel} aria-label="可选模型">
      <h3>云端模型</h3>
      {models.isPending && <p role="status">正在读取模型…</p>}
      {models.isError && <button type="button" onClick={() => void models.refetch()}>云端模型读取失败，点击重试</button>}
      {models.data && !cloudModels.length && <p>暂无已发布的云端模型。</p>}
      {cloudModels.map(model => status?.signedIn
        ? <button key={model.id} type="button" onClick={() => select(managedProviderPrefix + model.id)}>
          {cloudModelLabel(model)}{selected === managedProviderPrefix + model.id && <CheckIcon />}
        </button>
        : <Link key={model.id} className={s.lockedModel} to="/login" state={{ next: pathname }} title="请先登录">
          {cloudModelLabel(model)}<span><LockClosedIcon aria-hidden="true" />请先登录</span>
        </Link>)}
      {!!localModels.length && <>
        <h3>本机连接</h3>
        {localModels.map(model => <button key={model.id} type="button" onClick={() => select(model.id)}>
          {model.label}{model.id === selected && <CheckIcon />}
        </button>)}
      </>}
      <Link className={s.modelPickerFooter} to="/settings?section=models">配置自己的 API</Link>
    </div>}
  </div>;
}
