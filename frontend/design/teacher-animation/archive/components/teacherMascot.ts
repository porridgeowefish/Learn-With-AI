import { create } from 'zustand';

export type TeacherMascot = 'penguin' | 'ghost';
const key = 'lll.ui.teacherMascot';
function initial(): TeacherMascot {
  try { return localStorage.getItem(key) === 'ghost' ? 'ghost' : 'penguin'; }
  catch { return 'penguin'; }
}
export const useTeacherMascot = create<{ mascot: TeacherMascot; setMascot: (value: TeacherMascot) => void }>(set => ({
  mascot: initial(),
  setMascot: mascot => {
    if (mascot !== 'penguin' && mascot !== 'ghost') return;
    try { localStorage.setItem(key, mascot); } catch { /* Keep the session selection. */ }
    set({ mascot });
  },
}));
