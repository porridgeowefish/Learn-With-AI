import { create } from 'zustand';

const KEY = 'lll.onboarding.screen.v2';
interface Progress { active: boolean; step: number; slug: string; dismissed: boolean }
const initial: Progress = { active: false, step: 0, slug: '', dismissed: false };
function restore(): Progress {
  try {
    const saved = JSON.parse(localStorage.getItem(KEY) ?? 'null');
    if (saved && Number.isInteger(saved.step) && saved.step >= 0 && saved.step < 8 && typeof saved.slug === 'string' && typeof saved.active === 'boolean' && typeof saved.dismissed === 'boolean') return { active: saved.active, step: saved.step, slug: saved.slug, dismissed: saved.dismissed };
  } catch { /* The tour also works when browser storage is unavailable. */ }
  return initial;
}
interface TourState extends Progress {
  start: () => void; resume: () => void; pause: () => void; finish: () => void;
  move: (step: number) => void; selectProject: (slug: string) => void;
}
export const useOnboardingStore = create<TourState>((set, get) => {
  const update = (patch: Partial<Progress>) => {
    set(patch);
    const { active, step, slug, dismissed } = get();
    try { localStorage.setItem(KEY, JSON.stringify({ active, step, slug, dismissed })); } catch { /* Keep this session usable. */ }
  };
  return { ...restore(), start: () => update({ active: true, step: 0, slug: '' }), resume: () => update({ active: true }), pause: () => update({ active: false, dismissed: true }), finish: () => update({ active: false, step: 0, dismissed: true }), move: step => update({ step: Math.max(0, Math.min(7, step)) }), selectProject: slug => update({ slug, step: 4 }) };
});
