import { Renderer, type Tokens } from 'marked';

const quoted = String.raw`"(?:\\.|[^"\\\n])*"|'(?:\\.|[^'\\\n])*'`;
const pythonStrings = String.raw`"""(?:\\[\s\S]|(?!""")[^\\])*"""|'''(?:\\[\s\S]|(?!''')[^\\])*'''|` + quoted;
const backtickStrings = String.raw`\x60(?:\\[\s\S]|[^\x60\\])*\x60|` + quoted;
const slashComments = String.raw`//[^\n]*|/\*[\s\S]*?\*/`;

function grammar(comments: string, strings: string, keywords: string, ignoreCase = false) {
  return {
    tokens: new RegExp(
      [comments && '(?<comment>' + comments + ')', '(?<string>' + strings + ')',
        String.raw`(?<number>\b(?:0[xX][\da-fA-F_]+|0[bB][01_]+|\d[\d_]*(?:\.[\d_]+)?(?:[eE][+-]?\d+)?)\b)`,
        String.raw`(?<word>[a-zA-Z_$][\w$]*)`,
      ].filter(Boolean).join('|'), 'g',
    ),
    keywords: new Set(keywords.split(' ')),
    ignoreCase,
  };
}

const python = grammar('#[^\\n]*', pythonStrings, 'and as assert async await break case class continue def del elif else except False finally for from global if import in is lambda match None nonlocal not or pass raise return True try while with yield');
const javascript = grammar(slashComments, backtickStrings, 'abstract as async await break case catch class const continue declare default delete do else enum export extends false finally for from function if implements import in instanceof interface keyof let new null of private protected public readonly return satisfies static super switch this throw true try type typeof undefined var void while yield');
const json = grammar('', quoted, 'true false null');
const go = grammar(slashComments, backtickStrings, 'break case chan const continue default defer else fallthrough false for func go goto if import interface map nil package range return select struct switch true type var');
const cLike = grammar(slashComments, quoted, 'abstract auto bool break byte case catch char class const continue default do double else enum extern false final float for if import int interface long namespace new nullptr null package private protected public return short signed static struct super switch template this throw true try typedef unsigned using void volatile while');
const shell = grammar('#[^\\n]*', quoted, 'case do done elif else esac export fi for function if in local readonly return then until while');
const sql = grammar(String.raw`--[^\n]*|/\*[\s\S]*?\*/`, quoted, 'all alter and as asc between by case create delete desc distinct drop else end exists false from group having in insert into is join left limit not null offset on or order right select set table then true union update values when where with', true);

const languages = new Map([
  ['python', python], ['py', python],
  ['javascript', javascript], ['js', javascript], ['jsx', javascript],
  ['typescript', javascript], ['ts', javascript], ['tsx', javascript],
  ['json', json], ['go', go], ['golang', go],
  ['c', cLike], ['cpp', cLike], ['c++', cLike], ['java', cLike],
  ['sh', shell], ['bash', shell], ['shell', shell], ['zsh', shell],
  ['sql', sql],
]);

const entities: Record<string, string> = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
const escape = (text: string) => text.replace(/[&<>"']/g, character => entities[character]);

export function codeHighlightRenderer() {
  const renderer = new Renderer();
  const renderPlainCode = renderer.code.bind(renderer);
  renderer.code = (token: Tokens.Code) => {
    const language = token.lang?.trim().split(/\s+/, 1)[0].toLowerCase() ?? '';
    const syntax = languages.get(language);
    if (!syntax || token.escaped || token.text.length > 80_000) return renderPlainCode(token);

    const code = token.text.replace(/\n$/, '') + '\n';
    let result = '';
    let offset = 0;
    for (const match of code.matchAll(syntax.tokens)) {
      const [value] = match;
      const groups = match.groups!;
      const kind = groups.comment ? 'comment' : groups.string ? 'string' : groups.number ? 'number'
        : syntax.keywords.has(syntax.ignoreCase ? value.toLowerCase() : value) ? 'keyword' : '';
      result += escape(code.slice(offset, match.index));
      result += kind ? '<span class="syntax-' + kind + '">' + escape(value) + '</span>' : escape(value);
      offset = match.index + value.length;
    }
    return '<pre class="highlighted-code"><code class="language-' + escape(language) + '">' +
      result + escape(code.slice(offset)) + '</code></pre>\n';
  };
  return renderer;
}
