import { useEffect, useRef, useState } from 'react';
import { discoverModels, useAskAiSettings, useProbeAskAi, useSaveAskAiSettings, type AskConfig, type AskProvider } from './askAi';
import { applyProviderPreset, CODING_PLAN_LINKS, presetForProvider, PROVIDER_PRESETS, type ProviderPreset } from './providerPresets';
import { Button } from '@/shared/primitive/Button';
import { Icon } from '@/shared/primitive/Icon';
import { Modal } from '@/shared/primitive/Modal';
import { SecretInput } from '@/shared/primitive/SecretInput';
import { SIWCConnection } from './SIWCConnection';
import s from './ModelsPage.module.css';

const copyConfig = (config: AskConfig): AskConfig => ({ ...config, providers: config.providers.map(p => ({ ...p })),
  bindings: Object.fromEntries(Object.entries(config.bindings ?? {}).map(([key, value]) => [key, { ...value }])),
});
const PURPOSES = [ ['teacher', '教师'], ['annotationAskAI', 'Ask AI'], ['review', '复习稿'], ['practice', '练习'],
  ['disciplineMap', '学科地图'], ['documentStructure', '文档公式识别'] ] as const;
interface Editor { token: number; provider: AskProvider; original: string; isNew: boolean; presetId: string }
function ModelSelect({ value, models, label, disabled, onChange }: { value: string; models: string[]; label: string; disabled?: boolean; onChange: (model: string) => void }) {
  const [custom, setCustom] = useState(false);
  const ids = [...new Set([...models, ...(value ? [value] : [])])];
  return <><select aria-label={label} value={custom ? '__custom__' : value} disabled={disabled} onChange={event => {
    if (event.target.value === '__custom__') setCustom(true);
    else { setCustom(false); onChange(event.target.value); }
  }}><option value="">使用连接模型</option>{ids.map(id => <option key={id} value={id}>{id}</option>)}<option value="__custom__">其他模型（手动填写）</option></select>
  {custom && <input aria-label={`${label}自定义名称`} value={value} disabled={disabled} placeholder="填写提供方文档中的 model 标识" onChange={event => onChange(event.target.value)} />}</>;
}
export function ModelsPage() {
  const settings = useAskAiSettings();
  const save = useSaveAskAiSettings();
  const probe = useProbeAskAi();
  const [draft, setDraft] = useState<AskConfig | null>(null);
  const [editor, setEditor] = useState<Editor | null>(null);
  const editorRef = useRef<Editor | null>(null);
  const dirtyRef = useRef(false);
  const revision = useRef(0);
  const token = useRef(0);
  const [dirty, setDirty] = useState(false);
  const [discard, setDiscard] = useState(false);
  const [probeResult, setProbeResult] = useState<{ ok: boolean; error?: string } | null>(null);
  const [detected, setDetected] = useState<Map<string, string>>(new Map());
  const [discovered, setDiscovered] = useState<string[]>([]);
  const [catalogBusy, setCatalogBusy] = useState(false);
  const [catalogMessage, setCatalogMessage] = useState('');
  const [mode, setMode] = useState<'login' | 'api'>('login');
  editorRef.current = editor;
  useEffect(() => { if (settings.data && !dirtyRef.current) setDraft(copyConfig(settings.data)); }, [settings.data]);
  const editConfig = (change: (current: AskConfig) => AskConfig) => {
    dirtyRef.current = true; revision.current++; setDirty(true); save.reset();
    setDraft(current => current ? change(current) : current);
  };
  const openEditor = (provider?: AskProvider, initialPreset: ProviderPreset = PROVIDER_PRESETS.find(item => item.id === 'tencent-tokenhub')!) => {
    const value = provider ? { ...provider } : applyProviderPreset({ id: crypto.randomUUID(), kind: 'openai', baseURL: '', apiKey: '', model: '' }, initialPreset);
    const next = { token: ++token.current, provider: value, original: JSON.stringify(value), isNew: !provider, presetId: presetForProvider(value)?.id ?? 'custom' };
    editorRef.current = next; setEditor(next); setDiscard(false); setProbeResult(null); setDiscovered([]); setCatalogMessage(''); setCatalogBusy(false); probe.reset(); save.reset();
  };
  const closeEditor = (force = false) => {
    if (save.isPending) return;
    if (!force && editorRef.current && JSON.stringify(editorRef.current.provider) !== editorRef.current.original) { setDiscard(true); return; }
    editorRef.current = null; setEditor(null); token.current++; setDiscard(false); setProbeResult(null); setCatalogBusy(false);
  };
  const patch = (update: Partial<AskProvider>, presetId?: string) => {
    setProbeResult(null); setDiscard(false); setCatalogMessage(''); setCatalogBusy(false); probe.reset();
    setEditor(current => {
      if (!current) return current;
      const next = { ...current, provider: { ...current.provider, ...update }, ...(presetId ? { presetId } : {}) };
      editorRef.current = next; return next;
    });
  };
  const persist = (next: AskConfig, done?: () => void) => {
    const at = ++revision.current;
    save.mutate(copyConfig(next), { onSuccess: () => {
      if (revision.current !== at) return;
      setDraft(copyConfig(next)); dirtyRef.current = false; setDirty(false); done?.();
    } });
  };
  const saveEditor = () => {
    if (!draft || !editor) return;
    const current = editor;
    const next = copyConfig(draft);
    next.providers = current.isNew ? [...next.providers, { ...current.provider }] : next.providers.map(p => p.id === current.provider.id ? { ...current.provider } : p);
    if (!next.default && current.isNew) next.default = current.provider.id;
    // Purpose-specific overrides are learner choices, not stale connection fields.
    persist(next, () => { if (editorRef.current?.token === current.token) { editorRef.current = null; setEditor(null); token.current++; } });
  };
  const probeEditor = () => {
    if (!editor) return;
    const currentToken = editor.token, fingerprint = JSON.stringify(editor.provider);
    probe.mutate({ providerId: editor.provider.id, inline: { ...editor.provider } }, {
      onSuccess: result => { if (editorRef.current?.token !== currentToken || JSON.stringify(editorRef.current.provider) !== fingerprint) return;
        setProbeResult(result); if (result.ok) setDetected(old => new Map(old).set(editor.provider.id, fingerprint)); },
      onError: () => { if (editorRef.current?.token === currentToken && JSON.stringify(editorRef.current.provider) === fingerprint) setProbeResult({ ok: false, error: '连接检测失败，请检查地址与密钥。' }); },
    });
  };
  const loadModels = async () => {
    if (!editor) return;
    const currentToken = editor.token, fingerprint = JSON.stringify(editor.provider);
    setCatalogBusy(true); setCatalogMessage('');
    const matches = () => editorRef.current?.token === currentToken && JSON.stringify(editorRef.current.provider) === fingerprint;
    try {
      const result = await discoverModels({ providerId: editor.provider.id, inline: { ...editor.provider } });
      if (matches()) { setDiscovered(result.models.map(model => model.id)); setCatalogMessage(result.source === 'provider' ? '已获取提供方模型目录' : '当前使用核验预置目录'); }
    } catch { if (matches()) setCatalogMessage('模型目录暂不可用，可选择预置或填写自定义模型。'); }
    finally { if (matches()) setCatalogBusy(false); }
  };
  const setBinding = (key: string, providerId: string, model?: string) => editConfig(current => {
    const bindings = { ...current.bindings };
    if (providerId) bindings[key] = { providerId, ...(model ? { model } : {}) }; else {
      delete bindings[key];
      if (key === 'documentStructure') delete bindings.ocr;
    }
    return { ...current, bindings, ...(key === 'teacher' ? { default: providerId } : {}) };
  });
  if (settings.isError && !draft) return <section className={s.section} role="alert">模型配置读取失败</section>;
  if (!draft) return <section className={s.section} aria-busy="true">加载模型配置…</section>;
  const preset = editor ? PROVIDER_PRESETS.find(p => p.id === editor.presetId) : undefined;
  const modelIds = [...new Set([...(preset?.models.map(m => m.id) ?? []), ...discovered, ...(editor?.provider.model ? [editor.provider.model] : [])])];
  const options = draft.providers.map(provider => <option key={provider.id} value={provider.id}>{provider.name || provider.model || '未命名连接'}</option>);
  return <section className={s.section} aria-labelledby="api-connections-title">
    <header className={s.header} data-tour="model-connections"><h2 id="api-connections-title">模型连接</h2><div className={s.actions}>
      {mode === 'api' && <Button size="sm" variant="outline" disabled={save.isPending} onClick={() => openEditor()}><Icon name="plus" size={14} />添加 API 连接</Button>}
      <Button size="sm" loading={save.isPending && !editor} onClick={() => persist(draft)}>保存配置</Button>
    </div></header>
    <div className={s.actions} role="group" aria-label="连接方式"><Button size="sm" variant={mode === 'login' ? 'primary' : 'outline'} onClick={() => setMode('login')}>登录连接</Button><Button size="sm" variant={mode === 'api' ? 'primary' : 'outline'} onClick={() => setMode('api')}>API 连接</Button></div>
    {mode === 'login' && <SIWCConnection config={draft} save={next => persist(next)} />}
    <div className={s.bindings}><label>当前学习模型<select value={draft.bindings?.teacher?.providerId || draft.default} onChange={event => setBinding('teacher', event.target.value)}><option value="">选择连接</option>{options}</select></label><p className={s.saveStatus}>教师、Ask AI、复习稿与练习默认使用此连接。</p></div>
    <div className={s.apiMode} hidden={mode !== 'api'}>
    <div className={s.trial}>
      <div><a href="https://console.cloud.tencent.com/tokenhub/models" target="_blank" rel="noreferrer">领取腾讯云免费体验额度</a><p>领取后创建 API Key，在这里添加连接；额度与有效期以控制台为准。</p></div>
      <Button size="sm" disabled={save.isPending} onClick={() => openEditor(undefined, PROVIDER_PRESETS.find(item => item.id === 'tencent-tokenhub')!)}>配置 TokenHub 试用</Button>
    </div>
    <details className={s.setupGuide} open={!draft.providers.length}>
      <summary>第一次配置？按这五步完成</summary>
      <ol>
        <li><strong>领取额度。</strong>打开上方腾讯云模型广场，登录并按控制台要求开通服务，在“新用户福利”领取体验额度，记下有额度的模型。</li>
        <li><strong>复制密钥。</strong><a href="https://console.cloud.tencent.com/tokenhub/apikey" target="_blank" rel="noreferrer">打开 Key 管理</a>，创建普通 API Key 并复制。免费体验使用普通 API Key。</li>
        <li><strong>添加连接。</strong>点击“配置 TokenHub 试用”，从模型下拉框选刚才有额度的模型，把复制的密钥粘贴到 API Key；接口地址会自动填好。</li>
        <li><strong>检测并保存。</strong>点击“探测连接”，看到“已连通”后点击“保存连接”。失败时检查 Key 是否完整、该模型是否已开通、额度是否可用。</li>
        <li><strong>用于学习。</strong>在下面“教师”中选这个连接，再点击“保存配置”。复习稿、练习和 Ask AI 默认跟随教师。回到项目即可提问。</li>
      </ol>
      <p>其他提供方：点击“添加连接”→选择提供方→选择模型→通过“获取 Key”打开对应平台复制密钥→检测并保存。新增第一个连接会自动成为教师默认；已有连接时需在下方手动切换教师。</p>
    </details>
    {!editor && save.isError && <p className={s.error} role="alert">保存失败，请重试。</p>}
    {dirty && <p className={s.saveStatus} role="status">有未保存的用途配置</p>}
    <div className={s.list} aria-label="已配置模型">
      {!draft.providers.some(provider => provider.kind !== 'siwc') && <p className={s.empty}>添加 API 连接后即可开始学习。</p>}
      {draft.providers.filter(provider => provider.kind !== 'siwc').map(provider => <button key={provider.id} type="button" className={s.preview} aria-label={`编辑 ${provider.name || provider.model || '未命名连接'}`} disabled={save.isPending} onClick={() => openEditor(provider)}>
        <span className={`${s.connectionIcon} ${detected.get(provider.id) === JSON.stringify(provider) ? s.detected : ''}`} title={detected.get(provider.id) === JSON.stringify(provider) ? '已检测' : '尚未检测'}><Icon name="link" /></span>
        <span className={s.previewText}><strong>{provider.name || provider.model || '未命名连接'}</strong><small>{provider.model}</small></span><Icon name="pen" size={14} />
      </button>)}
    </div>
    </div>
    <details className={s.planNotes}><summary>高级设置：按用途单独指定连接</summary><div className={s.bindings}>
      {PURPOSES.filter(([key]) => key !== 'teacher').map(([key, label]) => {
        const binding = draft.bindings?.[key];
        const value = binding?.providerId ?? (key === 'teacher' ? draft.default : key === 'documentStructure' ? draft.bindings?.ocr?.providerId ?? '' : '');
        const missing = value && !draft.providers.some(p => p.id === value);
        const provider = draft.providers.find(p => p.id === value);
        const bindingModels = provider ? [...(presetForProvider(provider)?.models.map(m => m.id) ?? []), provider.model] : [];
        return <div key={key} className={s.bindingRow}><label>{label}<select disabled={save.isPending} value={value} onChange={event => setBinding(key, event.target.value)}>
          <option value="">{key === 'documentStructure' ? '本地免费 OCR（按系统检测）' : key === 'teacher' ? '选择连接' : '使用教师默认'}</option>
          {missing && <option value={value}>连接已移除，请重新选择</option>}{options}
        </select></label>{binding?.providerId && <label className={s.modelOverride}>模型覆盖<ModelSelect key={binding.providerId} label={`${label}模型覆盖`} value={binding.model ?? ''} models={bindingModels} disabled={save.isPending} onChange={model => setBinding(key, binding.providerId, model)} /></label>}</div>;
      })}
      <label>搜索引擎<select value={draft.searchEngine} disabled={save.isPending} onChange={event => editConfig(current => ({ ...current, searchEngine: event.target.value as AskConfig['searchEngine'] }))}><option value="google">Google</option><option value="bing">Bing</option></select></label>
    </div></details>
    <Modal open={!!editor} onOpenChange={open => { if (!open) closeEditor(); }} title={editor?.isNew ? '添加 API 连接' : 'API 连接详情'} description="选择提供方和模型，填写 Key。" className={s.connectionModal}
      footer={<><Button size="sm" variant="ghost" disabled={save.isPending} onClick={() => closeEditor(true)}>取消</Button><Button size="sm" variant="primary" loading={save.isPending} disabled={!editor?.provider.model.trim() || !editor.provider.baseURL.trim()} onClick={saveEditor}>保存连接</Button></>}>
      {editor && <>
        <ol className={s.editorGuide}>
          <li>{preset?.id === 'tencent-tokenhub' ? <>先在<a href="https://console.cloud.tencent.com/tokenhub/models" target="_blank" rel="noreferrer">模型广场</a>领取额度，然后<a href={preset.keyURL} target="_blank" rel="noreferrer">创建普通 API Key</a>并复制。</> : <>在提供方平台开通服务，通过下方“获取 Key”创建并复制 API Key。</>}</li>
          <li>下拉选择已开通的模型，把密钥粘贴到 API Key。提供方的接口地址已经预填。</li>
          <li>点击“探测连接”，成功后“保存连接”；随后在配置页选择教师连接并“保存配置”。</li>
        </ol>
        <div className={s.formGrid}>
        <label className={s.wide}>提供方<select value={editor.presetId} onChange={event => {
          const selected = PROVIDER_PRESETS.find(item => item.id === event.target.value); setDiscovered([]); setCatalogBusy(false);
          if (selected) patch(applyProviderPreset(editor.provider, selected), selected.id); else patch({}, 'custom');
        }}>{(['套餐', '官方', '聚合 / 中转'] as const).map(group => <optgroup key={group} label={group}>{PROVIDER_PRESETS.filter(p => p.group === group).map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</optgroup>)}<option value="custom">自行配置</option></select></label>
        {preset ? <label className={s.wide}>模型<ModelSelect key={`${editor.token}-${preset.id}`} label="模型" value={editor.provider.model} models={modelIds} onChange={model => { const known = preset.models.find(m => m.id === model); patch({ model, contextWindowTokens: known?.contextWindowTokens }); }} /><small>直接选择已开通的模型。填好 Key 后，“获取模型列表”可补充该账号的模型。</small></label> : <>
          <label>显示名称<input value={editor.provider.name ?? ''} onChange={event => patch({ name: event.target.value })} /></label>
          <label>接口协议<select value={editor.provider.kind} onChange={event => patch({ kind: event.target.value as AskProvider['kind'] })}><option value="openai">OpenAI 兼容</option><option value="anthropic">Anthropic</option></select></label>
          <label className={s.wide}>模型名称<input list="connection-models" value={editor.provider.model} onChange={event => patch({ model: event.target.value })} /><datalist id="connection-models">{discovered.map(id => <option key={id} value={id} />)}</datalist></label>
          <label className={s.wide}>Base URL<input value={editor.provider.baseURL} onChange={event => patch({ baseURL: event.target.value })} /></label>
          <label className={s.wide}>上下文 Token<input type="number" min={16384} step={1024} value={editor.provider.contextWindowTokens ?? ''} placeholder="留空使用安全估计" onChange={event => patch({ contextWindowTokens: event.target.value ? Number(event.target.value) : undefined })} /></label>
        </>}
        <label className={s.wide}>API Key<SecretInput value={editor.provider.apiKey} onChange={value => patch({ apiKey: value, apiKeyEnv: '' })} />{editor.provider.apiKeyEnv && !editor.provider.apiKey && <small>已使用环境变量密钥，留空保留。</small>}</label>
        <div className={`${s.flags} ${s.wide}`}><label><input type="checkbox" checked={!!editor.provider.reasoning} onChange={event => patch({ reasoning: event.target.checked })} />显示思考摘要</label>{editor.provider.kind === 'anthropic' && <label><input type="checkbox" checked={!!editor.provider.thinking} onChange={event => patch({ thinking: event.target.checked })} />启用思考</label>}</div>
      </div><div className={s.links}>{preset && <><a href={preset.keyURL} target="_blank" rel="noreferrer">获取 Key</a><a href={preset.docsURL} target="_blank" rel="noreferrer">官方文档</a>{preset.planURL && <a href={preset.planURL} target="_blank" rel="noreferrer">套餐</a>}</>}{preset?.note && <p>{preset.note}</p>}</div>
      <div className={s.cardFooter}><div className={s.actions}><Button size="sm" variant="outline" loading={probe.isPending} onClick={probeEditor}>探测连接</Button><Button size="sm" variant="ghost" loading={catalogBusy} onClick={() => { void loadModels(); }}>获取模型列表</Button></div>
        {!editor.isNew && <Button size="sm" variant="ghost" disabled={save.isPending} onClick={() => { editConfig(current => ({ ...current, providers: current.providers.filter(p => p.id !== editor.provider.id) })); closeEditor(true); }}>删除</Button>}
      </div>{probeResult && <p role="status" className={probeResult.ok ? s.connected : s.error}>{probeResult.ok ? '已连通' : probeResult.error || '连接失败'}</p>}{catalogMessage && <p className={s.saveStatus} role="status">{catalogMessage}</p>}
      {save.isError && <p className={s.error} role="alert">保存失败，输入已保留，请重试。</p>}
      {discard && <div className={s.discard} role="alert"><span>放弃未保存的修改？</span><Button size="sm" variant="ghost" onClick={() => setDiscard(false)}>继续编辑</Button><Button size="sm" onClick={() => closeEditor(true)}>放弃修改</Button></div>}
      </>}
    </Modal>
    <details className={s.planNotes}><summary>套餐说明</summary>{CODING_PLAN_LINKS.map(item => <a key={item.id} href={item.docsURL} target="_blank" rel="noreferrer">{item.name}：{item.note}</a>)}</details>
  </section>;
}
