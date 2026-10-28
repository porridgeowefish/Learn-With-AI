// useMarkdown — render markdown to safe HTML with robust LaTeX math
// support. Wraps marked + DOMPurify + lazy-loaded mermaid.
//
// MATH PIPELINE (designed to be robust against marked's lexer eating
// backslash escapes inside math):
//
//   1. Protect literals and pre-scan dollar / backslash math delimiters
//      BEFORE handing the markdown to marked's HTML renderer.
//   2. Replace each math span with a unique plaintext placeholder
//      with a source-unique prefix — these survive marked unchanged.
//   3. Run marked.parse on the placeholder-stripped markdown.
//   4. Render each captured math span to KaTeX HTML directly.
//   5. Splice the rendered HTML back into the marked output.
//   6. DOMPurify-sanitize the final HTML (whitelist includes KaTeX tags).
//
// This bypasses marked-katex-extension (which runs INSIDE marked's lexer
// and was eating `\Sigma` etc.) and gives us full control over the math
// extraction order.

import { useMemo } from 'react';

import { marked } from 'marked';
import DOMPurify from 'dompurify';
import { extractLatex } from '@/shared/lib/latexMarkup';
import { extractMathHTML } from '@/shared/lib/mathMarkup';

export interface MermaidBlock {
  id: string;
  code: string;
}

export interface MarkdownRender {
  html: string;
  mermaid: MermaidBlock[];
  interactive: Array<{ id: string; code: string }>;
}

// Configure marked once. We do NOT add the KaTeX extension here — math
// extraction happens before marked renders HTML (see extractLatex).
marked.setOptions({
  gfm: true,
  breaks: false,
  async: false,
});

const svgPurifier = DOMPurify(window);
svgPurifier.addHook('uponSanitizeAttribute', (_node, data) => {
  const name = data.attrName.toLowerCase();
  const value = data.attrValue.trim();
  if ((name === 'href' || name === 'xlink:href') && !/^(?:#|data:image\/(?:png|jpeg|jpg|gif|webp);base64,)/i.test(value)) data.keepAttr = false;
  // Keep local markers and clipping paths, without allowing external SVG resources.
  if (/url\s*\(/i.test(value) && !/^url\(\s*['"]?#[\w:.-]+['"]?\s*\)$/i.test(value)) data.keepAttr = false;
});

function extractSVG(md: string): { stripped: string; placeholders: Map<string, string> } {
  const placeholders = new Map<string, string>();
  let index = 0;
  let stripped = md.replace(/```svg\s*\n([\s\S]*?)```/gi, (_, source: string) => {
    const id = `INLINEVECTOR${index++}X`;
    const clean = svgPurifier.sanitize(source.trim(), {
      USE_PROFILES: { svg: true, svgFilters: true, html: false },
      FORBID_TAGS: ['script', 'foreignObject', 'iframe', 'object', 'embed', 'style', 'animate', 'set'],
      FORBID_ATTR: ['onload', 'onclick', 'onerror', 'onbegin', 'style'],
    });
    placeholders.set(id, clean);
    return `<div class="inline-svg" data-inline-svg="svg-${index - 1}">${id}</div>`;
  });
  // Streaming tail: an svg fence whose closing ``` has not arrived yet must
  // not fall through to marked — an unclosed fence turns the entire rest of
  // the message into one giant <pre> code wall.
  const danglingSvg = /```svg\s*\n[\s\S]*$/i.exec(stripped);
  if (danglingSvg) {
    stripped = stripped.slice(0, danglingSvg.index)
      + '<div class="mermaid-placeholder"><span class="mermaid-pending">图示正在生成…</span></div>';
  }
  return { stripped, placeholders };
}

function extractMermaid(md: string): { mdStripped: string; blocks: MermaidBlock[] } {
  const blocks: MermaidBlock[] = [];
  const re = /```[ \t]*mermaid[ \t]*\r?\n([\s\S]*?)```/gi;
  let i = 0;
  let mdStripped = md.replace(re, (_, code: string) => {
    const id = `mermaid-${i++}`;
    blocks.push({ id, code: code.trim() });
    return `<div data-mermaid-id="${id}" class="mermaid-placeholder"><span class="mermaid-pending">图表将在回答完成后渲染</span></div>`;
  });
  // Streaming tail: same unclosed-fence hazard as svg — stage the unfinished
  // code as a pending block instead of letting marked build a code wall.
  const dangling = /```[ \t]*mermaid[ \t]*\r?\n[\s\S]*$/i.exec(mdStripped);
  if (dangling) {
    const code = mdStripped.slice(dangling.index + dangling[0].indexOf('\n') + 1);
    mdStripped = mdStripped.slice(0, dangling.index)
      + '<div data-mermaid-id="mermaid-pending" class="mermaid-placeholder"><span class="mermaid-pending">图表正在生成…</span></div>';
    blocks.push({ id: 'mermaid-pending', code: code.trim() });
  }
  return { mdStripped, blocks };
}

// healDanglingFence — guard against an unterminated plain code fence.
// marked follows CommonMark: an unclosed fence swallows every character
// after it into one giant <pre> (raw line breaks + code background —
// the "text disappears into a box" glitch). svg/mermaid already guard
// their own fences in their extractors; this covers plain ```/~~~ ones:
//   - empty tail: drop the stray opening fence (no empty box flash).
//   - streaming: append a temporary closing fence so the code that HAS
//     arrived renders as a stable block until the real closer lands.
//   - final: if the tail still looks like code, close the fence; if it
//     looks like prose the model never meant to fence, drop the opening
//     fence and hand the text back to normal markdown rendering.
function healDanglingFence(md: string, streaming: boolean): string {
  // Tolerate \r\n line endings like the mermaid extractor does; healed
  // output is only rebuilt when a dangling fence exists, and marked is
  // fine with plain \n.
  const lines = md.split('\n').map((line) => line.replace(/\r$/, ''));
  const fence = /^( {0,3})(`{3,}|~{3,})(.*)$/;
  let openMarker = '';
  let openLength = 0;
  let openLine = -1;
  for (let i = 0; i < lines.length; i++) {
    const m = fence.exec(lines[i]);
    if (!m) continue;
    const marker = m[2][0];
    const info = m[3].trim();
    if (openMarker === '') {
      // A backtick fence's info string must not contain backticks —
      // otherwise the line is just prose/inline-code, not a fence.
      if (marker === '`' && info.includes('`')) continue;
      openMarker = marker;
      openLength = m[2].length;
      openLine = i;
    } else if (marker === openMarker && m[2].length >= openLength && info === '') {
      openMarker = '';
    }
  }
  if (openMarker === '') return md;
  const tail = lines.slice(openLine + 1);
  if (!tail.join('\n').trim()) return lines.slice(0, openLine).join('\n');
  if (streaming || looksLikeCode(tail)) return `${md}\n\`\`\``;
  return [...lines.slice(0, openLine), ...tail].join('\n');
}

// looksLikeCode — cheap heuristic over non-blank lines: code tends to
// carry assignment/brace characters, leading indentation, or keyword
// openings. Prose (especially CJK prose) rarely hits any of these.
function looksLikeCode(lines: string[]): boolean {
  const content = lines.filter((line) => line.trim());
  if (content.length === 0) return false;
  const codeish = content.filter((line) =>
    /[{};=<>]/.test(line)
    || /^ {2,}\S/.test(line)
    || /^(?:def |class |function |const |let |var |import |from |return |if |for |while |print\(|console\.)/.test(line));
  return codeish.length * 4 >= content.length;
}

// KaTeX emits MathML + nested HTML spans. DOMPurify's default allowlist
// drops MathML tags, so we extend it. KaTeX tags + attributes here are
// the minimum needed to preserve the rendering.
const KATEX_TAGS = [
  'math', 'mrow', 'mi', 'mo', 'mn', 'ms', 'msub', 'msup', 'msubsup',
  'mfrac', 'mroot', 'msqrt', 'mtext', 'mspace', 'mtable', 'mtr', 'mtd',
  'semantics', 'annotation', 'annotation-xml', 'menclose', 'merror',
  'mfenced', 'mover', 'munder', 'munderover', 'mstyle', 'mphantom',
];
const KATEX_ATTRS = [
  'class', 'style', 'mathvariant', 'encoding', 'xmlns', 'display',
  'scriptlevel', 'stretchy', 'fence', 'separator', 'lspace', 'rspace',
  'movablelimits', 'form', 'columnalign', 'rowalign', 'columnspacing',
  'rowspacing', 'columnlines', 'rowlines', 'frame', 'framespacing',
  'equalrows', 'equalcolumns', 'side', 'width', 'align', 'bevelled',
  'open', 'close', 'separators', 'accent', 'accentunder', 'dir',
  'linethickness', 'notation', 'position', 'subscriptshift',
  'superscriptshift', 'depth', 'height', 'voffset', 'lquote', 'rquote',
  'selection', 'href', 'mathbackground', 'mathcolor', 'mathsize',
];

DOMPurify.addHook('uponSanitizeAttribute', (_node, data) => {
  if (data.attrName === 'style') {
    const v = String(data.attrValue || '');
    if (/url\(|expression\(|javascript:/i.test(v)) {
      data.keepAttr = false;
      return;
    }
    data.keepAttr = true;
  }
});

DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  if (node.tagName?.toLowerCase() !== 'img') return;
  const src = node.getAttribute('src') ?? '';
  if (/^(?:https?:)?\/\//i.test(src)) {
    node.removeAttribute('src');
    node.setAttribute('data-remote-image-blocked', 'true');
  }
});

export function renderMarkdown(input: string, options?: { streaming?: boolean }): MarkdownRender {
  const streaming = options?.streaming ?? false;

  // GUI scripts remain opaque to marked/math and never enter host innerHTML.
  const interactive: MarkdownRender['interactive'] = [];
  const register = (code: string) => {
    if (interactive.length >= 8 || code.length > 200_000) return '<p>交互图示超过显示预算。</p>';
    const id = `gui-${interactive.length}`; interactive.push({ id, code });
    return `<div data-interactive-id="${id}" class="interactive-placeholder">${streaming ? '交互图示将在回复完成后显示…' : ''}</div>`;
  };
  const withoutFencedGUI = input.replace(/```[ \t]*html-interactive[ \t]*\r?\n([\s\S]*?)```/gi, (_, code: string) => register(code))
    .replace(/```[ \t]*html-interactive[ \t]*\r?\n[\s\S]*$/i, '<div class="mermaid-placeholder">交互图示正在生成…</div>');
  const withoutGUI = extractMathHTML(withoutFencedGUI, register);

  // Step 1: extract math BEFORE marked touches anything.
  const { stripped, placeholders } = extractLatex(withoutGUI);

  // Step 2: fenced SVG is treated as display media, sanitized before it can
  // enter the general Markdown HTML pipeline. External fetches and active SVG
  // features are forbidden; local fragment references remain available.
  const svg = extractSVG(stripped);

  // Step 3: extract mermaid blocks (placeholder form).
  const { mdStripped, blocks } = extractMermaid(svg.stripped);

  // Step 3.5: heal an unterminated plain code fence before marked can
  // turn the rest of the message into one giant <pre>.
  const healed = healDanglingFence(mdStripped, streaming);

  // Step 4: parse remaining markdown.
  const rawHtml = marked.parse(healed) as string;

  // Step 4: splice KaTeX-rendered HTML back into placeholders.
  // Placeholder IDs are source-unique alphanumeric tokens,
  // which survived marked unchanged. replaceAll handles duplicates.
  let withMath = rawHtml;
  for (const [id, html] of placeholders) {
    withMath = withMath.split(id).join(html);
  }
  for (const [id, clean] of svg.placeholders) {
    withMath = withMath.split(id).join(clean);
  }

  // Step 5: sanitize. KaTeX tags/attrs are whitelisted so the MathML
  // half of the output (used for screen readers + copy) survives.
  const safe = DOMPurify.sanitize(withMath, {
    ADD_ATTR: ['data-mermaid-id', 'data-inline-svg', 'data-interactive-id', 'viewBox', ...KATEX_ATTRS],
    ADD_TAGS: KATEX_TAGS,
    FORBID_TAGS: ['script', 'foreignObject', 'iframe', 'object', 'embed'],
  });

  return { html: safe, mermaid: blocks, interactive };
}

export function useMarkdown(input: string, streaming = false): MarkdownRender {
  return useMemo<MarkdownRender>(() => renderMarkdown(input, { streaming }), [input, streaming]);
}
