import katex from 'katex';
import { marked } from 'marked';

// Extract before Markdown escaping. Code, HTML and diagram source stay opaque.
export function extractLatex(input: string): { stripped: string; placeholders: Map<string, string> } {
  let prefix = 'LLLATEX';
  while (input.includes(prefix)) prefix += 'X';
  const literals = new Map<string, string>();
  const placeholders = new Map<string, string>();
  const protect = (raw: string) => {
    const id = `${prefix}LITERAL${literals.size}X`;
    literals.set(id, raw);
    return id;
  };
  let cursor = 0;
  let source = '';
  for (const token of marked.lexer(input)) {
    const start = input.indexOf(token.raw, cursor);
    if (start < 0) continue;
    source += input.slice(cursor, start);
    source += token.type === 'code' || token.type === 'html' ? protect(token.raw) : token.raw;
    cursor = start + token.raw.length;
  }
  source += input.slice(cursor);
  source = source.replace(/(`+)([\s\S]*?)\1(?!`)/g, protect);
  source = source.replace(/<(code|pre)\b[^>]*>[\s\S]*?<\/\1\s*>/gi, protect);
  source = source.replace(/<\/?[a-zA-Z][^>]*>/g, protect);

  const escaped = (at: number) => {
    let count = 0;
    while (at > 0 && source[--at] === '\\') count++;
    return count % 2 !== 0;
  };
  let stripped = '';
  for (let i = 0; i < source.length;) {
    const opener = ['$$', '\\[', '\\(', '$'].find(value => source.startsWith(value, i));
    if (!opener || escaped(i) || (opener === '$' && /\s/.test(source[i + 1] ?? ' '))) {
      stripped += source[i++];
      continue;
    }
    const closer = opener === '\\[' ? '\\]' : opener === '\\(' ? '\\)' : opener;
    const displayMode = opener === '$$' || opener === '\\[';
    const start = i + opener.length;
    let end = source.indexOf(closer, start);
    while (end >= 0 && (escaped(end) || (opener === '$' && (/\s/.test(source[end - 1]) || /\d/.test(source[end + 1] ?? ''))))) {
      end = source.indexOf(closer, end + closer.length);
    }
    if (end < 0 || (!displayMode && /[\r\n]/.test(source.slice(start, end)))) {
      stripped += source.slice(i, start);
      i = start;
      continue;
    }
    const code = source.slice(start, end);
    // A delimiter must never consume a protected literal or another math opener.
    if (!code.trim() || code.includes(prefix) || (opener === '$' && code.includes('$'))) {
      stripped += opener;
      i = start;
      continue;
    }
    const id = `${prefix}MATH${placeholders.size}X`;
    placeholders.set(id, katex.renderToString(code.trim(), {
      displayMode, throwOnError: false, output: 'html', strict: 'ignore', trust: false,
    }));
    stripped += id;
    i = end + closer.length;
  }
  // Outer literals can contain placeholders protected earlier (e.g. HTML code).
  for (const [id, raw] of [...literals].reverse()) stripped = stripped.split(id).join(raw);
  return { stripped, placeholders };
}
