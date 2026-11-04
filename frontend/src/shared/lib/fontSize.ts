import { create } from 'zustand';

export const FONT_SIZE_KEY = 'lll.ui.fontSize';
export const FONT_SIZES = [16, 18, 20, 22] as const;
export type FontSize = typeof FONT_SIZES[number];
function readFontSize(): FontSize {
  try {
    const value = Number(localStorage.getItem(FONT_SIZE_KEY));
    return FONT_SIZES.includes(value as FontSize) ? value as FontSize : 18;
  } catch { return 18; }
}
function applyFontSize(size: FontSize) {
  document.documentElement.style.setProperty('--ui-font-size', `${size}px`);
}
const initial = readFontSize();
applyFontSize(initial);
export const useFontSize = create<{ size: FontSize; setSize: (size: FontSize) => void }>(set => ({
  size: initial,
  setSize: size => {
    if (!FONT_SIZES.includes(size)) return;
    applyFontSize(size);
    try { localStorage.setItem(FONT_SIZE_KEY, String(size)); } catch { /* Session preference still applies. */ }
    set({ size });
  },
}));
