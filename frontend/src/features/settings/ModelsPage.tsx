import { useEffect, useRef, useState } from 'react';
import { discoverModels, useAskAiSettings, useProbeAskAi, useSaveAskAiSettings, type AskConfig, type AskProvider } from './askAi';
import { applyProviderPreset, presetForProvider, PROVIDER_PRESETS } from './providerPresets';
import { Button } from '@/shared/primitive/Button';
import { Icon } from '@/shared/primitive/Icon';
import { Modal } from '@/shared/primitive/Modal';
import { SecretInput } from '@/shared/primitive/SecretInput';
import { SIWCConnection } from './SIWCConnection';
import s from './ModelsPage.module.css';

const copyConfig = (config: AskConfig): AskConfig => ({ ...config, providers: config.providers.map(p => ({ ...p })),
  bindings: Object.fromEntries(Object.entries(config.bindings ?? {}).map(([key, value]) => [key, { ...value }])),
});
const PURPOSES = [ ['teacher', '教师'], ['review', '复习稿'], ['practice', '练习'],
  ['disciplineMap', '学科地图'], ['documentStructure', '文档公式识别'] ] as const;
const API_SOURCES = [
  { id: 'tencent-tokenhub', label: '腾讯云 TokenHub' },
  { id: 'openai', label: 'OpenAI' },
  { id: 'deepseek', label: 'DeepSeek' },
  { id: 'gemini', label: 'Google Gemini' },
  { id: 'claude', label: 'Claude' },
  { id: 'groq', label: 'Groq' },
] as const;
const SIMPLE_PRESET_IDS = API_SOURCES.map(source => source.id);
interface Editor { token: number; provider: AskProvider; original: string; isNew: boolean; presetId: string; needsProtocol?: boolean }

function ModelSelect({ value, models, label, disabled, onChange }: { value: string; models: string[]; label: string; disabled?: boolean; onChange: (model: string) => void }) {
  const [custom, setCustom] = useState(false);
  const [typed, setTyped] = useState(value);
  const ids = [...new Set([...models, ...(value ? [value] : [])])];
  return <><select aria-label={label} value={custom ? '__custom__' : value} disabled={disabled} onChange={event => {
    if (event.target.value === '__custom__') { setTyped(value); setCustom(true); }
    else { setCustom(false); onChange(event.target.value); }
  }}><option value="">使用连接模型</option>{ids.map(id => <option key={id} value={id}>{id}</option>)}<option value="__custom__">其他模型（手动填写）</option></select>
  {custom && <input aria-label={`${label}自定义名称`} value={typed} disabled={disabled} placeholder="填写提供方文档中的 model 标识"
    onChange={event => setTyped(event.target.value)} onBlur={event => { if (event.target.value.trim() !== value) onChange(event.target.value.trim()); }}
    onKeyDown={event => { if (event.key === 'Enter') event.currentTarget.blur(); }} />}</>;
}
export function ModelsPage() {
  const settings = useAskAiSettings();
  const save = useSaveAskAiSettings();
  const probe = useProbeAskAi();
  const [draft, setDraft] = useState<AskConfig | null>(null);
  const draftRef = useRef<AskConfig | null>(null);
  const [editor, setEditor] = useState<Editor | null>(null);
  const editorRef = useRef<Editor | null>(null);
  const dirtyRef = useRef(false);
  const revision = useRef(0);
  const token = useRef(0);
  const [dirty, setDirty] = useState(false);
  const [discard, setDiscard] = useState(false);
  const [remove, setRemove] = useState(false);
  const [probeResult, setProbeResult] = useState<{ ok: boolean; error?: string } | null>(null);
  const [detected, setDetected] = useState<Map<string, string>>(new Map());
  const [discovered, setDiscovered] = useState<string[]>([]);
  const [catalogBusy, setCatalogBusy] = useState(false);
  const [catalogMessage, setCatalogMessage] = useState('');
  const [mode, setMode] = useState<'login' | 'api'>('api');
  editorRef.current = editor;
  useEffect(() => {
    if (settings.data && !dirtyRef.current) {
      draftRef.current = copyConfig(settings.data);
      setDraft(draftRef.current);
    }
  }, [settings.data]);
  const openEditor = (provider?: AskProvider) => {
    const value = provider ? { ...provider } : applyProviderPreset({ id: crypto.randomUUID(), kind: 'openai', baseURL: '', apiKey: '', model: '' }, PROVIDER_PRESETS.find(preset => preset.id === 'tencent-tokenhub')!);
    const next = { token: ++token.current, provider: value, original: JSON.stringify(value), isNew: !provider, presetId: presetForProvider(value)?.id ?? 'custom' };
    editorRef.current = next; setEditor(next); setDiscard(false); setRemove(false); setProbeResult(null); setDiscovered([]); setCatalogMessage(''); setCatalogBusy(false); probe.reset();
  };
  const closeEditor = (force = false, saved = false) => {
    if (save.isPending && !saved) return;
    if (!force && editorRef.current && JSON.stringify(editorRef.current.provider) !== editorRef.current.original) { setDiscard(true); return; }
    editorRef.current = null; setEditor(null); token.current++; setDiscard(false); setRemove(false); setProbeResult(null); setCatalogBusy(false);
  };
  const patch = (update: Partial<AskProvider>, source?: { presetId: string; needsProtocol?: boolean }) => {
    setProbeResult(null); setDiscard(false); setCatalogMessage(''); setCatalogBusy(false); probe.reset();
    setEditor(current => {
      if (!current) return current;
      const next = { ...current, provider: { ...current.provider, ...update },
        ...(source ? { presetId: source.presetId, needsProtocol: source.needsProtocol } : {}) };
      editorRef.current = next; return next;
    });
  };
  const persist = (next: AskConfig, done?: () => void) => {
    const at = ++revision.current;
    dirtyRef.current = true; setDirty(true);
    if (!done) { draftRef.current = next; setDraft(next); }
    save.mutate(copyConfig(next), { onSuccess: () => {
      if (revision.current !== at) return;
      draftRef.current = next; setDraft(next); dirtyRef.current = false; setDirty(false); done?.();
    } });
  };
  const editConfig = (change: (current: AskConfig) => AskConfig) => {
    if (draftRef.current) persist(change(draftRef.current));
  };
  const saveEditor = () => {
    if (!draftRef.current || !editor) return;
    const current = editor;
    const next = copyConfig(draftRef.current);
    next.providers = current.isNew ? [...next.providers, { ...current.provider }] : next.providers.map(p => p.id === current.provider.id ? { ...current.provider } : p);
    if (!next.default && current.isNew) next.default = current.provider.id;
    // Purpose-specific overrides are learner choices, not stale connection fields.
    persist(next, () => { if (editorRef.current?.token === current.token) closeEditor(true, true); });
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
  return <section className={`${s.section}${` ${s.flat}`}`} aria-labelledby="api-connections-title">
    <header className={s.header} data-tour="model-connections"><div><h2 id="api-connections-title">自配模型连接</h2><p className={s.intro}>使用自己的 API Key，或连接现有的 ChatGPT 账号。</p></div>
      {!editor && (save.isError ? <div className={s.actions}><p className={s.error} role="alert">保存失败。</p><Button size="sm" onClick={() => persist(draft)}>重试保存</Button></div>
        : <p className={s.saveStatus} role="status">{dirty ? '正在保存…' : '更改自动保存'}</p>)}
    </header>
    {<div className={s.connectionTabs} role="group" aria-label="连接方式">
      <Button variant="ghost" size="sm" aria-pressed={mode === 'api'} onClick={() => setMode('api')}>自配 API</Button>
      <Button variant="ghost" size="sm" aria-pressed={mode === 'login'} onClick={() => setMode('login')}>ChatGPT 登录</Button>
    </div>}
    {mode === 'login' && <SIWCConnection config={draft} save={next => persist(next)} />}
    <div className={s.bindings}><label>当前学习模型<select disabled={!!editor} value={draft.bindings?.teacher?.providerId || draft.default} onChange={event => setBinding('teacher', event.target.value)}><option value="">选择连接</option>{options}</select></label><p className={s.saveStatus}>教师、复习稿与练习默认使用此连接。</p></div>
    <div className={s.apiMode} hidden={mode !== 'api'}>
    <div className={s.apiSetup}><div><h3>{'已保存的 API 连接'}</h3><p>{'选择服务商后填写 API Key，即可检测并保存连接。'}</p></div><Button size="sm" variant={'primary'} disabled={save.isPending} onClick={() => openEditor()}><Icon name="plus" size={14} />添加 API 连接</Button></div>
    <div className={s.list} aria-label="已配置模型">
      {!draft.providers.some(provider => provider.kind !== 'siwc') && <p className={s.empty}>添加 API 连接后即可开始学习。</p>}
      {draft.providers.filter(provider => provider.kind !== 'siwc').map(provider => <button key={provider.id} type="button" className={s.preview} aria-label={`编辑 ${provider.name || provider.model || '未命名连接'}`} disabled={save.isPending} onClick={() => openEditor(provider)}>
        <span className={`${s.connectionIcon} ${detected.get(provider.id) === JSON.stringify(provider) ? s.detected : ''}`} title={detected.get(provider.id) === JSON.stringify(provider) ? '已检测' : '尚未检测'}><Icon name="link" /></span>
        <span className={s.previewText}><strong>{provider.name || provider.model || '未命名连接'}</strong><small>{provider.model}</small></span><Icon name="pen" size={14} />
      </button>)}
    </div>
    <div className={s.apiSources}><h3>获取 API Key</h3><p>从你使用的服务商获取 Key，再回到上方添加连接。</p><div className={s.sourceLinks}>{API_SOURCES.map(source => { const provider = PROVIDER_PRESETS.find(item => item.id === source.id); return provider && <a key={source.id} href={provider.keyURL} target="_blank" rel="noreferrer">{source.label}<span aria-hidden="true">↗</span></a>; })}</div></div>
    </div>
    <details className={s.planNotes}><summary>高级设置：按用途单独指定连接</summary><div className={s.bindings}>
      {PURPOSES.filter(([key]) => key !== 'teacher').map(([key, label]) => {
        const binding = draft.bindings?.[key];
        const value = binding?.providerId ?? (key === 'teacher' ? draft.default : key === 'documentStructure' ? draft.bindings?.ocr?.providerId ?? '' : '');
        const missing = value && !draft.providers.some(p => p.id === value);
        const provider = draft.providers.find(p => p.id === value);
        const bindingModels = provider ? [...(presetForProvider(provider)?.models.map(m => m.id) ?? []), provider.model] : [];
        return <div key={key} className={s.bindingRow}><label>{label}<select disabled={!!editor} value={value} onChange={event => setBinding(key, event.target.value)}>
          <option value="">{key === 'documentStructure' ? '本地免费 OCR（按系统检测）' : key === 'teacher' ? '选择连接' : '使用教师默认'}</option>
          {missing && <option value={value}>连接已移除，请重新选择</option>}{options}
        </select></label>{binding?.providerId && <label className={s.modelOverride}>模型覆盖<ModelSelect key={binding.providerId} label={`${label}模型覆盖`} value={binding.model ?? ''} models={bindingModels} disabled={!!editor} onChange={model => setBinding(key, binding.providerId, model)} /></label>}</div>;
      })}
    </div></details>
    <Modal open={!!editor} onOpenChange={open => { if (!open) closeEditor(); }} title={editor?.isNew ? '添加 API 连接' : 'API 连接详情'} description="选择来源、模型，填写 API Key。" className={s.connectionModal}
      footer={<><Button size="sm" variant="ghost" disabled={save.isPending} onClick={() => closeEditor(true)}>取消</Button><Button size="sm" variant="primary" loading={save.isPending} disabled={editor?.needsProtocol || !editor?.provider.model.trim() || !editor.provider.baseURL.trim()} onClick={saveEditor}>保存连接</Button></>}>
      {editor && <>
        <div className={s.formGrid}>
        <label className={s.wide}>服务商<select value={editor.presetId} onChange={event => {
          const selected = PROVIDER_PRESETS.find(item => item.id === event.target.value); setDiscovered([]); setCatalogBusy(false);
          const blank: AskProvider = { id: editor.provider.id, kind: 'openai', name: '', model: '', baseURL: '', apiKey: '',
            apiKeyEnv: undefined, contextWindowTokens: undefined, reasoning: false, thinking: false };
          if (selected) patch(applyProviderPreset(blank, selected), { presetId: selected.id }); else patch(blank, { presetId: 'custom', needsProtocol: true });
        }}>{SIMPLE_PRESET_IDS.map(id => { const source = API_SOURCES.find(item => item.id === id)!; return <option key={id} value={id}>{source.label}</option>; })}{editor.presetId !== 'custom' && !SIMPLE_PRESET_IDS.includes(editor.presetId as typeof SIMPLE_PRESET_IDS[number]) && <option value={editor.presetId}>{preset?.name}</option>}<option value="custom">其他来源（高级配置）</option></select></label>
        {preset ? <label className={s.wide}>模型<ModelSelect key={`${editor.token}-${preset.id}`} label="模型" value={editor.provider.model} models={modelIds} onChange={model => { const known = preset.models.find(m => m.id === model); patch({ model, contextWindowTokens: known?.contextWindowTokens }); }} /><small>选择账号已开通的模型；不确定时先用默认项检测连接。</small></label> : <>
          <label>显示名称<input value={editor.provider.name ?? ''} onChange={event => patch({ name: event.target.value })} /></label>
          <label>接口协议<select value={editor.needsProtocol ? '' : editor.provider.kind} onChange={event => patch({ kind: event.target.value as AskProvider['kind'] }, { presetId: 'custom' })}><option value="" disabled>选择接口协议</option><option value="openai">OpenAI 兼容</option><option value="anthropic">Anthropic</option></select></label>
          <label className={s.wide}>模型名称<input list="connection-models" value={editor.provider.model} onChange={event => patch({ model: event.target.value })} /><datalist id="connection-models">{discovered.map(id => <option key={id} value={id} />)}</datalist></label>
          <label className={s.wide}>Base URL<input value={editor.provider.baseURL} onChange={event => patch({ baseURL: event.target.value })} /></label>
          <label className={s.wide}>上下文 Token<input type="number" min={16384} step={1024} value={editor.provider.contextWindowTokens ?? ''} placeholder="留空使用安全估计" onChange={event => patch({ contextWindowTokens: event.target.value ? Number(event.target.value) : undefined })} /></label>
        </>}
        <label className={s.wide}>API Key<SecretInput value={editor.provider.apiKey} onChange={value => patch({ apiKey: value, apiKeyEnv: '' })} />{editor.provider.apiKeyEnv && !editor.provider.apiKey && <small>已使用环境变量密钥，留空保留。</small>}</label>
        <details className={s.wide}><summary>高级选项</summary><p className={s.presetNote}>不同服务商使用不同地址和协议，已为所选来源自动填写。其他来源可自行配置。</p><div className={s.flags}><label><input type="checkbox" checked={!!editor.provider.reasoning} onChange={event => patch({ reasoning: event.target.checked })} />显示思考摘要</label>{editor.provider.kind === 'anthropic' && <label><input type="checkbox" checked={!!editor.provider.thinking} onChange={event => patch({ thinking: event.target.checked })} />启用思考</label>}</div><Button size="sm" variant="ghost" loading={catalogBusy} onClick={() => { void loadModels(); }}>获取模型列表</Button></details>
      </div>{preset?.note && <p className={s.presetNote}>{preset.note}</p>}
      <div className={s.cardFooter}><div className={s.actions}><Button size="sm" variant="outline" loading={probe.isPending} onClick={probeEditor}>探测连接</Button></div>
        {!editor.isNew && <Button size="sm" variant="ghost" disabled={save.isPending} onClick={() => setRemove(true)}>删除</Button>}
      </div>{probeResult && <p role="status" className={probeResult.ok ? s.connected : s.error}>{probeResult.ok ? '已连通' : probeResult.error || '连接失败'}</p>}{catalogMessage && <p className={s.saveStatus} role="status">{catalogMessage}</p>}
      {save.isError && <p className={s.error} role="alert">保存失败，输入已保留，请重试。</p>}
      {remove && <div className={s.discard} role="alert"><span>删除此 API 连接？</span><Button size="sm" variant="ghost" disabled={save.isPending} onClick={() => setRemove(false)}>保留连接</Button><Button size="sm" loading={save.isPending} onClick={() => {
        const config = draftRef.current;
        if (config) persist({ ...config, providers: config.providers.filter(p => p.id !== editor.provider.id) }, () => closeEditor(true, true));
      }}>确认删除</Button></div>}
      {discard && <div className={s.discard} role="alert"><span>放弃未保存的修改？</span><Button size="sm" variant="ghost" onClick={() => setDiscard(false)}>继续编辑</Button><Button size="sm" onClick={() => closeEditor(true)}>放弃修改</Button></div>}
      </>}
    </Modal>
  </section>;
}
