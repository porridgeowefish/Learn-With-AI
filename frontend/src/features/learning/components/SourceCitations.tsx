import type { SourceCitation } from '../api/learningWorkspace';
import s from './SourceCitations.module.css';

export function SourceCitations({ citations, sourceNames }: { citations: SourceCitation[]; sourceNames: Map<string, string> }) {
  if (!citations.length) return null;
  return <ul className={s.sources} aria-label="本次查阅来源">
    {citations.map((item, index) => <li key={item.evidenceId}>
      {index + 1} · {sourceNames.get(item.sourceId) ?? '资料'} · {item.displayRange}
    </li>)}
  </ul>;
}
