import { useState } from 'react';
import { useSearchParams, Link } from 'react-router-dom';
import { useAppearance, useUpdateAppearance } from './appearance';
import { ModelsPage } from './ModelsPage';
import { AccountSettings } from './AccountSettings';
import { WorkspaceSettings } from './WorkspaceSettings';
import { TeacherNotesSettings } from './TeacherNotesSettings';
import { LearningSettings } from './LearningSettings';
import { Icon } from '@/shared/primitive/Icon';
import type { ThemePreference } from '@/shared/lib/theme';
import { useOnboardingStore } from '@/shared/store/slices/onboarding';
import { Button } from '@/shared/primitive/Button';
import { UpdatePanel } from './UpdatePanel';
import s from './SettingsPage.module.css';
import { FONT_SIZES, useFontSize, type FontSize } from '@/shared/lib/fontSize';

const categories = [ ['general', '常规'], ['appearance', '外观'], ['models', '模型连接'], ['learning', '回复、搜索与记忆'], ['notes', '教师笔记'], ['storage', '存储与数据'], ['usage', 'Token 用量'], ['about', '更新与关于'] ] as const;

const visibleCategories = ([['account', '个人信息'], ...categories.filter(([id]) => id !== 'usage')] as const);
export function SettingsPage() {
  const startGuide = useOnboardingStore(state => state.start);
  const [params, setParams] = useSearchParams();
  const [filter, setFilter] = useState('');
  const requested = params.get('section');
  const section = visibleCategories.some(([id]) => id === requested) ? requested : requested === 'usage' ? 'account' : 'general';
  return <div className={s.host}>
    <aside className={s.navigation}><h1>设置</h1><input aria-label="搜索设置" placeholder="搜索设置" value={filter} onChange={e => setFilter(e.target.value)} /><nav aria-label="设置分类">{visibleCategories.filter(([,name]) => name.includes(filter)).map(([id,name]) => <button key={id} type="button" aria-current={section === id ? 'page' : undefined} onClick={() => setParams({section:id})}>{name}</button>)}</nav></aside>
    <div className={s.scroll} data-testid="settings-scroll" key={section}>
      <header className={s.header}><h2 className={s.title}>{visibleCategories.find(([id]) => id === section)?.[1]}</h2></header>
      {section === 'account' && <AccountSettings />}
      {section === 'general' && <section className={s.appearance}><h2>学习工作台</h2><p>在左侧管理外观、模型、搜索与学习数据。</p>{false}<Link to="/settings?section=learning">管理回复与联网搜索</Link><div><Button onClick={startGuide}>开始屏幕向导</Button></div></section>}
      {section === 'appearance' && <AppearanceSection />}
      {section === 'models' && <ModelsPage />}
      {section === 'learning' && <LearningSettings />}
      {section === 'notes' && <TeacherNotesSettings />}
      {section === 'storage' && <WorkspaceSettings />}
      {section === 'usage' && <section className={s.appearance}><h2>Token 用量</h2><p>查看各模型与项目的历史使用情况。</p><Link to="/usage">打开用量统计</Link></section>}
      {section === 'about' && <><UpdatePanel /><OpenSourceSection /></>}
    </div>
  </div>;
}
const THEME_OPTIONS: Array<{ id: ThemePreference; name: string; description: string; swatches: string[] }> = [
  { id: 'lychee-paper', name: '荔枝暖纸', description: '温暖、安静的默认阅读主题', swatches: ['#faf8f4', '#fffdf9', '#60775a'] },
  { id: 'mountain-mist', name: '远山雾蓝', description: '清凉克制，适合长时间专注', swatches: ['#f4f7f7', '#fbfdfd', '#557987'] },
  { id: 'wisteria-gray', name: '云海天蓝', description: '白色为主、天蓝点缀的轻盈主题', swatches: ['#f7fbff', '#ffffff', '#69aeda'] },
  { id: 'codex-white', name: 'Codex 白', description: '留白与深灰，清晰专注', swatches: ['#f8f9f8', '#ffffff', '#202321'] },
  { id: 'claude-paper', name: 'Claude 暖纸', description: '暖白与陶土色，柔和舒适', swatches: ['#faf9f5', '#fffdf8', '#b65f43'] },
];

function AppearanceSection() {
  const fontSize = useFontSize(state => state.size);
  const setFontSize = useFontSize(state => state.setSize);
  const appearance = useAppearance();
  const update = useUpdateAppearance();
  const selected = update.data?.theme ?? appearance.data?.theme ?? 'lychee-paper';
  return (
    <section className={s.appearance} aria-labelledby="appearance-title">
      <div>
        <h2 id="appearance-title">外观</h2>
        <p>选择一套柔和、可访问的配色。更改会立即应用并保存在本机。</p>
      </div>
      <label className={s.fontSize}>
        <span><strong>界面字号</strong><small>同时调整主页、设置和学习页面，保留文字层级。更改立即生效。</small></span>
        <select aria-label="界面字号" value={fontSize} onChange={event => setFontSize(Number(event.target.value) as FontSize)}>
          {FONT_SIZES.map((size, index) => <option key={size} value={size}>{['标准', '舒适（默认）', '大号', '特大'][index]}</option>)}
        </select>
      </label>
      <div className={s.themeChoices} role="radiogroup" aria-label="界面主题">
        {THEME_OPTIONS.map((theme) => (
          <button
            key={theme.id}
            type="button"
            role="radio"
            aria-checked={selected === theme.id}
            className={`${s.themeChoice} ${selected === theme.id ? s.themeSelected : ''}`}
            onClick={() => update.mutate(theme.id)}
          >
            <span className={s.themePreview} aria-hidden="true">
              {theme.swatches.map((color) => <i key={color} style={{ background: color }} />)}
            </span>
            <span><strong>{theme.name}</strong><small>{theme.description}</small></span>
            {selected === theme.id && <Icon name="check" size={14} />}
          </button>
        ))}
      </div>
      {update.isError && <div className={s.error}>主题保存失败：{(update.error as Error).message}</div>}
    </section>
  );
}

function OpenSourceSection() {
  const repo = 'https://github.com/porridgeowefish/Learn-With-AI';
  return (
    <section className={s.openSource} aria-labelledby="open-source-title">
      <div>
        <h2 id="open-source-title">开源与反馈</h2>
        <p>Learn-With-AI 欢迎问题反馈和代码贡献。</p>
      </div>
      <div className={s.settingsActions}>
        <a href={`${repo}/issues`} target="_blank" rel="noreferrer">报告问题</a>
        <a href={`${repo}/blob/main/CONTRIBUTING.md`} target="_blank" rel="noreferrer">贡献代码</a>
        <a href={repo} target="_blank" rel="noreferrer">查看源码</a>
      </div>
    </section>
  );
}
