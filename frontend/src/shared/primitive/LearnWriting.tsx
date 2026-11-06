/** The pen follows the same SVG path as the ink; no generated frames or fonts. */
export function LearnWriting() {
  return <picture>
    <source media="(prefers-reduced-motion: reduce)" srcSet="/images/learn-writing/learn-still.svg" />
    <img src="/images/learn-writing/learn.svg" alt="" width={128} height={50} style={{ display: 'block' }} />
  </picture>;
}
