export const THEME_STORAGE_KEY = 'lll.ui.theme';

export const themes = ['lychee-paper', 'mountain-mist', 'wisteria-gray', 'codex-white', 'claude-paper'] as const;
export type ThemePreference = (typeof themes)[number];

export function isThemePreference(value: unknown): value is ThemePreference {
  return typeof value === 'string' && (themes as readonly string[]).includes(value);
}

export function applyTheme(preference: ThemePreference): ThemePreference {
  document.documentElement.dataset.theme = preference;
  document.documentElement.dataset.themePreference = preference;
  localStorage.setItem(THEME_STORAGE_KEY, preference);
  return preference;
}
