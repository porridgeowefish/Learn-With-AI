import { useAppearance, useUpdateAppearance } from './appearance';
import { ModelsPage } from './ModelsPage';
import { TeacherNotesSettings } from './TeacherNotesSettings';
import { Icon } from '@/shared/primitive/Icon';
import type { ThemePreference } from '@/shared/lib/theme';
import { useHealth } from '@/app/api/health';
import { useUpdateInfo } from './update';
import s from './SettingsPage.module.css';

export function SettingsPage() {
  return <div className={s.host}><div className={s.scroll} data-testid="settings-scroll">
    <header className={s.header}><h1 className={s.title}>设置</h1></header>
    <AppearanceSection />
    <ModelsPage />
    <TeacherNotesSettings />
    <VersionAndDataSection />
    <OpenSourceSection />
  </div></div>;
}
const THEME_OPTIONS: Array<{ id: ThemePreference; name: string; description: string; swatches: string[] }> = [
  { id: 'lychee-paper', name: '荔枝暖纸', description: '温暖、安静的默认阅读主题', swatches: ['#faf8f4', '#fffdf9', '#60775a'] },
  { id: 'mountain-mist', name: '远山雾蓝', description: '清凉克制，适合长时间专注', swatches: ['#f4f7f7', '#fbfdfd', '#557987'] },
  { id: 'wisteria-gray', name: '云海天蓝', description: '白色为主、天蓝点缀的轻盈主题', swatches: ['#f7fbff', '#ffffff', '#69aeda'] },
  { id: 'codex-white', name: 'Codex 白', description: '留白与深灰，清晰专注', swatches: ['#f8f9f8', '#ffffff', '#202321'] },
  { id: 'claude-paper', name: 'Claude 暖纸', description: '暖白与陶土色，柔和舒适', swatches: ['#faf9f5', '#fffdf8', '#b65f43'] },
];

function AppearanceSection() {
  const appearance = useAppearance();
  const update = useUpdateAppearance();
  const selected = update.data?.theme ?? appearance.data?.theme ?? 'lychee-paper';
  return (
    <section className={s.appearance} aria-labelledby="appearance-title">
      <div>
        <h2 id="appearance-title">外观</h2>
        <p>选择一套柔和、可访问的配色。更改会立即应用并保存在本机。</p>
      </div>
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

function VersionAndDataSection() {
  const { data: health } = useHealth();
  const { data: info } = useUpdateInfo();
  const current = info;
  const status = current?.available ? `发现新版本 ${current.latest}` : '';
  return (
    <section className={s.openSource} aria-labelledby="version-data-title">
      <div>
        <h2 id="version-data-title">版本与数据</h2>
        <p>
          {current ? <>当前版本 <strong>{current.current}</strong>{status && <> · {status}</>}</> : '前往 GitHub 下载最新版本。macOS 下载对应芯片的安装包，安装后重新打开应用。'}
        </p>
        <p>
          学习数据目录 <code>{health?.workspace ?? '…'}</code>。项目、笔记、资料、偏好与 API 配置都保存在这里，升级和卸载都不会删除。
        </p>
      </div>
        <div className={s.openSourceActions}>
          <a href="https://github.com/porridgeowefish/Learn-With-AI/releases/latest" target="_blank" rel="noreferrer">GitHub 下载与安装指引</a>
        </div>
    </section>
  );
}

function OpenSourceSection() {
  const repo = 'https://github.com/porridgeowefish/LLL_life_long_learn';
  return (
    <section className={s.openSource} aria-labelledby="open-source-title">
      <div>
        <h2 id="open-source-title">开源与反馈</h2>
        <p>Learn-With-AI 欢迎问题反馈和代码贡献。</p>
      </div>
      <div className={s.openSourceActions}>
        <a href={`${repo}/issues`} target="_blank" rel="noreferrer">报告问题</a>
        <a href={`${repo}/blob/main/CONTRIBUTING.md`} target="_blank" rel="noreferrer">贡献代码</a>
        <a href={repo} target="_blank" rel="noreferrer">查看源码</a>
      </div>
    </section>
  );
}
