import { useState } from 'react';
import { Button } from '@/shared/primitive/Button';
import { useAskAiSettings } from './askAi';
import { effortNames, useLearningProfile, useSaveLearningProfile, useLearningMemories, useChangeMemory, type LearningSettings as Settings, type LearningMemory } from './learning';
import s from './TeacherNotesSettings.module.css';
import style from './LearningSettings.module.css';

export function LearningSettings() {
  const profile = useLearningProfile();
  const save = useSaveLearningProfile();
  const models = useAskAiSettings();
  const [manage, setManage] = useState(false);
  const [searchDraft, setSearchDraft] = useState<Settings['search']>();
  const cfg = profile.data?.settings;
  const providers = models.data?.providers ?? [];
  const defaultId = models.data?.bindings?.teacher?.providerId || models.data?.default || '';
  const efforts = profile.data?.capabilities[defaultId] ?? [];
  if (!cfg) return <section className={`${s.card} ${style.card}`} role="status">{profile.isError ? '回复设置读取失败，请刷新重试。' : '加载回复设置…'}</section>;
  const update = (patch: Partial<Settings>) => save.mutate({ ...cfg, ...patch });
  return <>
    <section className={`${s.card} ${style.card}`} aria-labelledby="reply-settings-title">
      <h2 className={s.title} id="reply-settings-title">回复设置</h2>
      <div className={style.rows}>
        <div className={style.row}><span>回复长度</span><div className={style.segments} role="group" aria-label="回复长度">
          {([['short', '短'], ['medium', '中'], ['long', '长']] as const).map(([value, label]) => <button key={value} type="button" disabled={save.isPending} aria-pressed={cfg.length === value} onClick={() => update({ length: value })}>{label}</button>)}
        </div></div>
        <div className={style.row}><span>默认思考强度<small>也可以在对话框中调整</small></span>
          <select className={s.input} aria-label="默认思考强度" disabled={save.isPending || !efforts.length} value={efforts.includes(cfg.effort) ? cfg.effort : efforts.includes('medium') ? 'medium' : ''} onChange={e => update({ effort: e.target.value })}>
            {!efforts.length && <option value="">当前模型不支持调节</option>}{efforts.map(v => <option key={v} value={v}>{effortNames[v]}</option>)}
          </select>
        </div>
        <div className={style.row}><span>显示思考摘要</span><Toggle label="显示思考摘要" checked={cfg.showReasoning} disabled={save.isPending} onChange={showReasoning => update({ showReasoning })} /></div>
      </div>
    </section>
    <section className={`${s.card} ${style.card}`} aria-labelledby="search-settings-title">
      <header className={s.header}><div><h2 id="search-settings-title" className={s.title}>联网搜索</h2><p className={s.description}>配置后，在对话框中按需开启搜索。</p></div>
        <Toggle label="启用联网搜索" checked={cfg.search.enabled} disabled={save.isPending} onChange={enabled => {
          save.reset();
          if (enabled) setSearchDraft({ ...cfg.search });
          else { setSearchDraft(undefined); update({ search: { ...cfg.search, enabled: false } }); }
        }} />
      </header>
      {cfg.search.enabled && !searchDraft && <div className={style.row}><span className={s.hint}>{({chatgpt:'ChatGPT 原生搜索', 'zhipu-plan':'智谱 Plan · 自动复用模型连接', zhipu:'智谱普通 API', deepseek:'DeepSeek 搜索', yuanbao:'元宝搜索'} as Record<string,string>)[cfg.search.provider]}</span><Button variant="ghost" size="sm" onClick={() => setSearchDraft({ ...cfg.search })}>修改配置</Button></div>}
      <p className={s.hint}>配置智谱 Coding Plan 后自动接入套餐搜索；手动选择的搜索服务优先。DeepSeek 与元宝使用各自的搜索额度。</p>
      {searchDraft && <div className={style.configuration}>
        <p className={s.hint}>完成配置并验证后，开关才会保持开启。验证会执行一次搜索。</p>
        <label className={s.field}>搜索来源<select aria-label="搜索来源" className={s.input} value={searchDraft.provider} disabled={save.isPending} onChange={e => setSearchDraft({ ...searchDraft, provider: e.target.value, providerId: '', apiKey: '', hasKey: false })}>
          <option value="">选择搜索来源</option><option value="chatgpt">ChatGPT 原生搜索</option><option value="zhipu-plan">智谱 Coding Plan（复用连接）</option><option value="zhipu">智谱普通 API（按次计费）</option><option value="deepseek">DeepSeek 搜索</option><option value="yuanbao">元宝搜索</option>
        </select></label>
        {searchDraft.provider === 'chatgpt' && <label className={s.field}>ChatGPT 连接<select aria-label="搜索连接" className={s.input} value={searchDraft.providerId} disabled={save.isPending} onChange={e => setSearchDraft({ ...searchDraft, providerId: e.target.value })}><option value="">选择已登录连接</option>{providers.filter(p => p.kind === 'siwc').map(p => <option key={p.id} value={p.id}>{p.name || p.model}</option>)}</select><small className={s.hint}>使用该连接的模型和账户额度。</small></label>}
        {['zhipu','deepseek','yuanbao'].includes(searchDraft.provider) && <label className={s.field}>API Key<input className={s.input} type="password" autoComplete="off" value={searchDraft.apiKey || ''} placeholder={searchDraft.hasKey ? '已保存，留空保留' : '填写对应服务 API Key'} disabled={save.isPending} onChange={e => setSearchDraft({ ...searchDraft, apiKey: e.target.value })} /></label>}
        <div className={style.actions}><Button variant="outline" size="sm" loading={save.isPending} onClick={() => save.mutate({ ...cfg, search: { ...searchDraft, enabled: true } }, { onSuccess: () => setSearchDraft(undefined), onError: () => {} })}>验证并开启</Button><Button variant="ghost" size="sm" disabled={save.isPending} onClick={() => setSearchDraft(undefined)}>取消</Button></div>
      </div>}
    </section>
    <section className={`${s.card} ${style.card}`} aria-labelledby="memory-settings-title">
      <header className={s.header}><div><h2 id="memory-settings-title" className={s.title}>学习记忆</h2><p className={s.description}>记住你明确提到的学习背景、目标和偏好。</p></div><Toggle label="启用学习记忆" checked={cfg.memoryEnabled} disabled={save.isPending} onChange={memoryEnabled => update({ memoryEnabled })} /></header>
      <div className={style.row}><p className={s.hint}>{cfg.memoryEnabled ? '只整理开启后的新对话。相关内容会交给当前教师模型处理。' : '关闭后不再整理或使用记忆，已保存的内容仍可管理。'}</p><Button variant="ghost" size="sm" onClick={() => setManage(!manage)}>{manage ? '收起记忆' : '管理记忆'}</Button></div>
      {manage && <MemoryManager />}
    </section>
    {save.isError && <p className={s.error} role="alert">{(save.error as Error).message}</p>}
  </>;
}

function Toggle({ label, checked, disabled, onChange }: { label: string; checked: boolean; disabled: boolean; onChange: (checked: boolean) => void }) {
  return <label className={s.toggle}><input type="checkbox" role="switch" aria-label={label} checked={checked} disabled={disabled} onChange={e => onChange(e.target.checked)} /><span className={s.track} aria-hidden="true" /></label>;
}

function MemoryManager() {
  const query = useLearningMemories(true);
  const change = useChangeMemory();
  const [filter, setFilter] = useState('');
  const [clear, setClear] = useState(false);
  const memories = query.data?.memories ?? [];
  return <div className={style.configuration}>
    <div className={style.row}><input className={s.input} aria-label="搜索学习记忆" placeholder="搜索记忆" value={filter} onChange={e => setFilter(e.target.value)} /><Button size="sm" variant="ghost" disabled={!memories.length || change.isPending} onClick={() => setClear(true)}>清空</Button></div>
    {clear && <div className={style.actions}><span className={s.hint}>清空全部学习记忆？旧对话不会自动重建。</span><Button size="sm" variant="outline" loading={change.isPending} onClick={() => change.mutate({ id: 'all' }, { onSuccess: () => setClear(false) })}>确认清空</Button><Button size="sm" variant="ghost" onClick={() => setClear(false)}>取消</Button></div>}
    {query.isPending ? <p className={s.hint}>加载记忆…</p> : query.isError ? <p className={s.error}>记忆读取失败，请重试。</p> : !memories.length ? <p className={s.hint}>还没有学习记忆。开启后，会从新对话中逐步整理。</p> : memories.filter(m => m.text.includes(filter)).map(m => <MemoryRow key={m.id} memory={m} />)}
    {query.data?.lastError && <p className={s.hint}>{query.data.lastError}</p>}
    {change.isError && <p className={s.error} role="alert">{(change.error as Error).message}</p>}
  </div>;
}
function MemoryRow({ memory: m }: { memory: LearningMemory }) {
  const change = useChangeMemory();
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(m.text);
  return <article className={style.memory}>
    {editing ? <textarea className={s.input} aria-label="编辑记忆" maxLength={500} value={text} onChange={e => setText(e.target.value)} /> : <p>{m.text}</p>}
    <div className={style.actions}>
      {editing ? <><Button size="sm" variant="outline" loading={change.isPending} disabled={!text.trim()} onClick={() => change.mutate({ id: m.id, text }, { onSuccess: () => setEditing(false) })}>保存</Button><Button size="sm" variant="ghost" onClick={() => { setText(m.text); setEditing(false); }}>取消</Button></> : <><Button size="sm" variant="ghost" onClick={() => { setText(m.text); setEditing(true); }}>编辑</Button><Button size="sm" variant="ghost" loading={change.isPending} onClick={() => change.mutate({ id: m.id })}>删除</Button></>}
      <details className={style.source}><summary>来源</summary><p>{m.edited ? '你已编辑此记忆。原始来源：' : ''}{m.evidence}</p><a href={`/project/${encodeURIComponent(m.sourceProject)}/teacher?message=${encodeURIComponent(m.messageId)}`}>查看对话</a></details>
    </div>
    {change.isError && <p className={s.error} role="alert">保存失败：{(change.error as Error).message}</p>}
  </article>;
}
