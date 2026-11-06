import { useTeacherMascot, type TeacherMascot as Mascot } from '@/shared/lib/teacherMascot';

export function TeacherMascot({ character, size = 56, animated = true }: { character?: Mascot; size?: number; animated?: boolean }) {
  const selected = useTeacherMascot(state => state.mascot);
  const root = `/images/teachers/${character ?? selected}`;
  return <picture>
    <source media="(prefers-reduced-motion: reduce)" srcSet={`${root}.png`} />
    <img src={`${root}.${animated ? 'gif' : 'png'}`} alt="" width={size} height={size} style={{ display: 'block', objectFit: 'contain' }} />
  </picture>;
}
