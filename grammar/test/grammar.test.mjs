// Tests for the TextMate grammar, run with vscode-textmate, the engine VS Code uses.
//
//     npm install && npm test
//
// The cases are those of docs/sketchy/pygments-tony/test_lexer.py, so that the grammar and the
// lexer are held to one account of the syntax; where they differ, it says why. The sweeps at
// the end tokenize every ```tony block and every .tony file in the repository, and fail on any
// character the grammar gives no scope a theme colours.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { createRequire } from 'node:module';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

import vsctm from 'vscode-textmate';
import oniguruma from 'vscode-oniguruma';

const here = fileURLToPath(new URL('.', import.meta.url));
const REPO = join(here, '../..');

const require = createRequire(import.meta.url);
const wasm = readFileSync(require.resolve('vscode-oniguruma/release/onig.wasm'));
await oniguruma.loadWASM(wasm.buffer.slice(wasm.byteOffset, wasm.byteOffset + wasm.byteLength));

const registry = new vsctm.Registry({
  onigLib: Promise.resolve({
    createOnigScanner: (sources) => new oniguruma.OnigScanner(sources),
    createOnigString: (s) => new oniguruma.OnigString(s),
  }),
  loadGrammar: async (scopeName) => {
    if (scopeName !== 'source.tony') return null;
    const path = join(here, '../syntaxes/tony.tmLanguage.json');
    return vsctm.parseRawGrammar(readFileSync(path, 'utf8'), path);
  },
});
const grammar = await registry.loadGrammar('source.tony');

// What a token is, for these tests: the outermost scope under the root that is not a meta
// scope, with '.tony' dropped. A quoted string is one string.quoted.double, not its quotes and
// its content apart, and a key inside a block literal's opening line is a key.
function kind(scopes) {
  const s = scopes.slice(1).find((x) => !x.startsWith('meta.'));
  return s === undefined ? '' : s.replace(/\.tony$/, '');
}

// The grammar's output, a [kind, text] pair per token: adjacent tokens of one kind are joined,
// text is trimmed, and tokens that are only whitespace are dropped.
function toks(code) {
  const out = [];
  let stack = vsctm.INITIAL;
  for (const line of code.replace(/\n$/, '').split('\n')) {
    const r = grammar.tokenizeLine(line, stack);
    let prev = null;
    for (const t of r.tokens) {
      const text = line.slice(t.startIndex, t.endIndex);
      const k = kind(t.scopes);
      if (prev && prev[0] === k && k !== '') prev[1] += text;
      else out.push((prev = [k, text]));
    }
    stack = r.ruleStack;
  }
  return out.map(([k, v]) => [k, v.trim()]).filter(([, v]) => v !== '');
}

const check = (code, ...expected) => assert.deepEqual(toks(code), expected, `tokenizing ${JSON.stringify(code)}`);

const KEY = 'entity.name.tag';
const SEP = 'punctuation.separator.key-value.mapping';
const LIT = 'string.unquoted.plain';
const DQ = 'string.quoted.double';
const SQ = 'string.quoted.single';
const INT = 'constant.numeric.integer';
const FLOAT = 'constant.numeric.float';
const ITEM = 'punctuation.definition.block.sequence.item';
const COMMA = 'punctuation.separator';
const MAP_B = 'punctuation.definition.mapping.begin';
const MAP_E = 'punctuation.definition.mapping.end';
const SEQ_B = 'punctuation.definition.sequence.begin';
const SEQ_E = 'punctuation.definition.sequence.end';
const TAG = 'storage.type.tag';
const COMMENT = 'comment.line.number-sign';
const BLOCK = 'keyword.control.flow.block-scalar.literal';
const BODY = 'string.unquoted.block';

test('keys and values', () => {
  check('name: blog-api\n', [KEY, 'name'], [SEP, ':'], [LIT, 'blog-api']);
  check('count: 3\n', [KEY, 'count'], [SEP, ':'], [INT, '3']);
  check('k: null\n', [KEY, 'k'], [SEP, ':'], ['constant.language.null', 'null']);
  check('k: true\n', [KEY, 'k'], [SEP, ':'], ['constant.language.boolean', 'true']);
  // space before the ':' is allowed, where the lexer reads a literal
  check('k : v\n', [KEY, 'k'], [SEP, ':'], [LIT, 'v']);
  // a bracket inside a key's bracket keeps it a key
  check('.[a[0]]: v\n', [KEY, '.[a[0]]'], [SEP, ':'], [LIT, 'v']);
  // a key named for a constant is still a key
  check('null: 1\n', [KEY, 'null'], [SEP, ':'], [INT, '1']);
  // and a literal that merely starts with one is neither
  check('nullable\n', [LIT, 'nullable']);
});

test('literal punctuation', () => {
  // docs/tony.md, "Literals and Punctuation".
  // ':' inside a literal, on both sides of the separator
  check('a:b: "a:b"\n', [KEY, 'a:b'], [SEP, ':'], [DQ, '"a:b"']);
  check('resolver: resolvers:security:security-review\n',
    [KEY, 'resolver'], [SEP, ':'], [LIT, 'resolvers:security:security-review']);
  // '-' and '.' and '/' inside a literal
  check('- tony-format/context\n', [ITEM, '-'], [LIT, 'tony-format/context']);
  check('document: my-file.yaml\n', [KEY, 'document'], [SEP, ':'], [LIT, 'my-file.yaml']);
  // leading '$' and '.', and balanced brackets
  check('$y: "$y"\n', [KEY, '$y'], [SEP, ':'], [DQ, '"$y"']);
  check('.[x]: ".[x]"\n', [KEY, '.[x]'], [SEP, ':'], [DQ, '".[x]"']);
  check('fields: .array(.field)\n', [KEY, 'fields'], [SEP, ':'], [LIT, '.array(.field)']);
  // an un-opened close character truncates the literal
  check('{a:b}\n', [MAP_B, '{'], [LIT, 'a:b'], [MAP_E, '}']);
});

test('numbers', () => {
  // docs/tony.md, "Numbers" -- rules 1, 3 and 4.
  check('{ replicas: 3, ratio: 1.5, big: 1e9, neg: -2.5 }\n',
    [MAP_B, '{'],
    [KEY, 'replicas'], [SEP, ':'], [INT, '3'], [COMMA, ','],
    [KEY, 'ratio'], [SEP, ':'], [FLOAT, '1.5'], [COMMA, ','],
    [KEY, 'big'], [SEP, ':'], [FLOAT, '1e9'], [COMMA, ','],
    [KEY, 'neg'], [SEP, ':'], [FLOAT, '-2.5'],
    [MAP_E, '}']);
  // rule 3: a run holding a letter is a string, all of it
  for (const quantity of ['100m', '1Gi', '30s', '1h30m']) {
    check(`k: ${quantity}\n`, [KEY, 'k'], [SEP, ':'], [LIT, quantity]);
  }
  // rule 4: three or more '.'-separated groups of digits is a string
  check('addr: 192.168.1.1\n', [KEY, 'addr'], [SEP, ':'], [LIT, '192.168.1.1']);
  // notation is part of the number
  check('{ h: 0x1f, o: 0o644, b: 0b1010 }\n',
    [MAP_B, '{'],
    [KEY, 'h'], [SEP, ':'], ['constant.numeric.integer.hexadecimal', '0x1f'], [COMMA, ','],
    [KEY, 'o'], [SEP, ':'], ['constant.numeric.integer.octal', '0o644'], [COMMA, ','],
    [KEY, 'b'], [SEP, ':'], ['constant.numeric.integer.binary', '0b1010'],
    [MAP_E, '}']);
  // a sparse-array key is a key, not a value
  check('0: hello\n', ['entity.name.tag.index', '0'], [SEP, ':'], [LIT, 'hello']);
});

test('tags', () => {
  check('!my-tag 2\n', [TAG, '!my-tag'], [INT, '2']);
  check('document: !tovalue.file my-file.yaml\n',
    [KEY, 'document'], [SEP, ':'], [TAG, '!tovalue.file'], [LIT, 'my-file.yaml']);
  // arguments, including the commas within them, belong to the tag
  check('f: !tag1.tag2(a,b) 22\n', [KEY, 'f'], [SEP, ':'], [TAG, '!tag1.tag2(a,b)'], [INT, '22']);
  check('f: !retag(tag1.tag2(a,b),tag2(z).other(x))\n',
    [KEY, 'f'], [SEP, ':'], [TAG, '!retag(tag1.tag2(a,b),tag2(z).other(x))']);
  // an unclosed argument list leaves no character unpainted
  assert.deepEqual(unscoped('k: !tag(a', 't', 1), []);
  // a tag in a key set ends at the ',' that separates elements
  check('[!a, !b]\n', [SEQ_B, '['], [TAG, '!a'], [COMMA, ','], [TAG, '!b'], [SEQ_E, ']']);
});

test('comments and separators', () => {
  check('---\n', ['entity.other.document.begin', '---']);
  // a separator may carry a comment; the lexer takes `---` there for a literal
  check('--- # doc\n', ['entity.other.document.begin', '---'], [COMMENT, '# doc']);
  check('# a comment\n', [COMMENT, '# a comment']);
  // no preceding space required
  check('k: v# tail\n', [KEY, 'k'], [SEP, ':'], [LIT, 'v'], [COMMENT, '# tail']);
  check('[1, 2, 3,]\n',
    [SEQ_B, '['], [INT, '1'], [COMMA, ','], [INT, '2'], [COMMA, ','], [INT, '3'], [COMMA, ','],
    [SEQ_E, ']']);
  check('[1 2 3]\n', [SEQ_B, '['], [INT, '1'], [INT, '2'], [INT, '3'], [SEQ_E, ']']);
});

test('block literals', () => {
  // The lexer paints a body as one token; a TextMate grammar works a line at a time, so here
  // it is a token per line.
  check('|\n  hello\n  I am a block literal\n',
    [BLOCK, '|'], [BODY, 'hello'], [BODY, 'I am a block literal']);
  check('|-\n  chopped\n', [BLOCK, '|-'], [BODY, 'chopped']);
  // opened by a key, and by a merge key
  check('k: |\n  hello\n', [KEY, 'k'], [SEP, ':'], [BLOCK, '|'], [BODY, 'hello']);
  check('<<: |\n  {{ $k | quote}}: {{ $v | quote -}}\n',
    ['support.constant.merge-key', '<<'], [SEP, ':'], [BLOCK, '|'],
    [BODY, '{{ $k | quote}}: {{ $v | quote -}}']);
  // the body is what is indented past the opening line: a sibling of the key that opened it
  // is not part of it
  check('a:\n  k: |\n    hello\n  j: 2\n',
    [KEY, 'a'], [SEP, ':'],
    [KEY, 'k'], [SEP, ':'], [BLOCK, '|'], [BODY, 'hello'],
    [KEY, 'j'], [SEP, ':'], [INT, '2']);
  // '|' is only a marker at the end of a line
  check('k: a|b\n', [KEY, 'k'], [SEP, ':'], [LIT, 'a|b']);
  // a blank line inside the body does not end it
  check('k: |\n  one\n\n  two\nj: 1\n',
    [KEY, 'k'], [SEP, ':'], [BLOCK, '|'], [BODY, 'one'], [BODY, 'two'],
    [KEY, 'j'], [SEP, ':'], [INT, '1']);
});

test('block literals: where the lexer and the grammar differ', () => {
  // The opening line may carry a comment, which is not part of the value ("Block Literals" in
  // docs/tony.md). The lexer requires the '|' to end its line.
  check('key: |- # what this holds\n  the value\n',
    [KEY, 'key'], [SEP, ':'], [BLOCK, '|-'], [COMMENT, '# what this holds'], [BODY, 'the value']);
  // Each '- ' on the opening line is a level, and a field after it one more: here the content
  // is at 4, so the sibling field at 2 is not content. The lexer measures from the line's
  // indentation alone, and takes `j:` into the body.
  check('- k: |\n    content\n  j: "sibling"\n',
    [ITEM, '-'], [KEY, 'k'], [SEP, ':'], [BLOCK, '|'], [BODY, 'content'],
    [KEY, 'j'], [SEP, ':'], [DQ, '"sibling"']);
  check('- - |\n    two markers\n  - x\n',
    [ITEM, '-'], [ITEM, '-'], [BLOCK, '|'], [BODY, 'two markers'], [ITEM, '-'], [LIT, 'x']);
  check('- |\n  one marker\n- x\n',
    [ITEM, '-'], [BLOCK, '|'], [BODY, 'one marker'], [ITEM, '-'], [LIT, 'x']);
  // and a comment line that ends in '|' is a comment
  check('  # a | b |\nk: 1\n', [COMMENT, '# a | b |'], [KEY, 'k'], [SEP, ':'], [INT, '1']);
});

test('block literals: what does and does not add a level', () => {
  // A tag adds no level: the parser puts `- !exec |`'s body at 2, and `- - !tag |`'s at 4.
  check('- !exec |\n  ls /dev/null\n- true\n',
    [ITEM, '-'], [TAG, '!exec'], [BLOCK, '|'], [BODY, 'ls /dev/null'],
    [ITEM, '-'], ['constant.language.boolean', 'true']);
  check('- - !tag |\n    four\n  - x\n',
    [ITEM, '-'], [ITEM, '-'], [TAG, '!tag'], [BLOCK, '|'], [BODY, 'four'], [ITEM, '-'], [LIT, 'x']);
  check('k: !tag |\n  body\nj: 1\n',
    [KEY, 'k'], [SEP, ':'], [TAG, '!tag'], [BLOCK, '|'], [BODY, 'body'], [KEY, 'j'], [SEP, ':'], [INT, '1']);
  // A quoted key may hold '#'; only an unquoted one cannot.
  check('"a # b": |\n  body\nj: 1\n',
    [DQ, '"a # b"'], [SEP, ':'], [BLOCK, '|'], [BODY, 'body'], [KEY, 'j'], [SEP, ':'], [INT, '1']);
  // A field before markers counts as one too: `k: - |` holds ["body\n"], with the body at 4.
  check('k: - |\n    body\nj: 1\n',
    [KEY, 'k'], [SEP, ':'], [ITEM, '-'], [BLOCK, '|'], [BODY, 'body'], [KEY, 'j'], [SEP, ':'], [INT, '1']);
  check('- k: - |\n      body\n- x\n',
    [ITEM, '-'], [KEY, 'k'], [SEP, ':'], [ITEM, '-'], [BLOCK, '|'], [BODY, 'body'], [ITEM, '-'], [LIT, 'x']);
  // A file with CRLF line ends, should a host pass the '\r' on.
  check('k: |\r\n  body\r\nj: 1\r\n',
    [KEY, 'k'], [SEP, ':'], [BLOCK, '|'], [BODY, 'body'], [KEY, 'j'], [SEP, ':'], [INT, '1']);
  // Every depth up to MAX_DEPTH in build.mjs, which is past any this repository uses.
  for (let n = 1; n <= 8; n++) {
    const markers = '- '.repeat(n);
    check(`${markers}|\n${'  '.repeat(n)}body\n- x\n`,
      ...Array(n).fill([ITEM, '-']), [BLOCK, '|'], [BODY, 'body'], [ITEM, '-'], [LIT, 'x']);
  }
});

test('strings', () => {
  check("b: 'single \"quoted\"'\n", [KEY, 'b'], [SEP, ':'], [SQ, "'single \"quoted\"'"]);
  // folded strings are one per line
  check('c:\n  "part one"\n  " and two"\n',
    [KEY, 'c'], [SEP, ':'], [DQ, '"part one"'], [DQ, '" and two"']);
  // an escaped quote does not end the string
  check('k: "a \\" b"\n', [KEY, 'k'], [SEP, ':'], [DQ, '"a \\" b"']);
  // a '.[' or '$[' that is not closed inside a string ends at the string's closing quote, which
  // stays the string's
  for (const open of ['.[', '$[']) {
    check(`sep: "${open}"\nj: 1\n`, [KEY, 'sep'], [SEP, ':'], [DQ, `"${open}"`], [KEY, 'j'], [SEP, ':'], [INT, '1']);
    const tokens = inner(`k: '${open}b' c`);
    assert.deepEqual(tokens.at(-2), ['punctuation.definition.string.end', "'"]);
    assert.deepEqual(tokens.at(-1), [LIT, 'c']);
  }
  // nor does an unclosed '[' within one, at any depth: each stops at the quote, and the line
  check('sep: ".[["\nj: 1\n', [KEY, 'sep'], [SEP, ':'], [DQ, '".[["'], [KEY, 'j'], [SEP, ':'], [INT, '1']);
  assert.deepEqual(inner(`k: ".[ 'a ]" # c`).slice(-3),
    [['punctuation.definition.string.end', '"'], ['punctuation.definition.comment', '#'], [COMMENT, ' c']]);
  // an unterminated string ends at the line, taking nothing after it
  check('k: "oops\nj: 1\n', [KEY, 'k'], [SEP, ':'], [DQ, '"oops'], [KEY, 'j'], [SEP, ':'], [INT, '1']);
});

// The innermost scope of each token on a one-line document, without '.tony', for what the
// outermost-scope view of toks() cannot see: an interpolation inside a string, say.
function inner(code) {
  return grammar.tokenizeLine(code, vsctm.INITIAL).tokens
    .map((t) => [t.scopes.at(-1).replace(/\.tony$/, ''), code.slice(t.startIndex, t.endIndex)])
    .filter(([, v]) => v.trim() !== '');
}

const OPEN = 'punctuation.section.embedded.begin';
const CLOSE = 'punctuation.section.embedded.end';
const VAR = 'variable';
const TYPE = 'support.type.builtin';
const CALL = 'entity.name.function';
const OP = 'keyword.operator';

test('expressions: where the grammar goes past the lexer', () => {
  // docs/eval.md. The lexer paints an expression as one literal; the grammar tells its names,
  // calls, fields and operators apart.
  // the types tony-base defines, parametric ones included, are types
  assert.deepEqual(inner('k: .[string]'),
    [[KEY, 'k'], [SEP, ':'], [OPEN, '.['], [TYPE, 'string'], [CLOSE, ']']]);
  assert.deepEqual(inner('k: .[array(nullable(string))]'),
    [[KEY, 'k'], [SEP, ':'], [OPEN, '.['], [TYPE, 'array'], ['punctuation', '('], [TYPE, 'nullable'],
      ['punctuation', '('], [TYPE, 'string'], ['punctuation', ')'], ['punctuation', ')'], [CLOSE, ']']]);
  assert.deepEqual(inner('else: .[tony-base:ir]'),
    [[KEY, 'else'], [SEP, ':'], [OPEN, '.['], [TYPE, 'tony-base:ir'], [CLOSE, ']']]);
  // and any other name is a variable, holding '-' and ':' as a schema's names do
  assert.deepEqual(inner('k: .[repository]').map(([k]) => k), [KEY, SEP, OPEN, VAR, CLOSE]);
  assert.deepEqual(inner('k: .[my-ctx:thing]').map(([k]) => k), [KEY, SEP, OPEN, VAR, CLOSE]);
  assert.deepEqual(inner('k: .[stringly]').map(([k]) => k), [KEY, SEP, OPEN, VAR, CLOSE]);
  // '-' joins a name only to letters, so `count-1` is a subtraction, painted as `a.b-c`'s is
  assert.deepEqual(inner('k: .[count-1]').map(([k]) => k), [KEY, SEP, OPEN, VAR, OP, 'constant.numeric', CLOSE]);
  assert.deepEqual(inner('k: .[a.b-c]').map(([k]) => k),
    [KEY, SEP, OPEN, VAR, 'punctuation', 'variable.other.property', OP, VAR, CLOSE]);
  // expr-lang's words, and '#' for the element a predicate is applied to -- quoted, as the
  // parser takes an expression holding spaces
  assert.deepEqual(inner(`k: '.[all(xs, # > 0) and x in ys]'`).map(([k]) => k),
    [KEY, SEP, 'punctuation.definition.string.begin', OPEN, CALL, 'punctuation', VAR, 'punctuation',
      'variable.language', OP, 'constant.numeric', 'punctuation', 'keyword.operator.word', VAR,
      'keyword.operator.word', VAR, CLOSE, 'punctuation.definition.string.end']);
  // `key` is a type only with its parameter, as tony-base defines it
  assert.deepEqual(inner('k: .[key(name)]').map(([k]) => k),
    [KEY, SEP, OPEN, TYPE, 'punctuation', VAR, 'punctuation', CLOSE]);
  assert.deepEqual(inner('v: .[getpath("$.version").String]'),
    [[KEY, 'v'], [SEP, ':'], [OPEN, '.['], [CALL, 'getpath'], ['punctuation', '('],
      ['punctuation.definition.string.begin', '"'], [DQ, '$.version'], ['punctuation.definition.string.end', '"'],
      ['punctuation', ')'], ['punctuation', '.'], ['variable.other.property', 'String'], [CLOSE, ']']]);
  assert.deepEqual(inner('d: .[a?.b ?? "unset"]').map(([k]) => k),
    [KEY, SEP, OPEN, VAR, OP, 'variable.other.property', OP,
      'punctuation.definition.string.begin', DQ, 'punctuation.definition.string.end', CLOSE]);
  // a ']' inside a string or a nested index does not close it
  assert.deepEqual(inner('k: .[m["a]"]]').at(-1), [CLOSE, ']']);
  assert.equal(inner('k: .[m["a]"]]').filter(([k]) => k === CLOSE).length, 1);
  // in a quoted string, as a build file writes a condition
  assert.deepEqual(inner(`- if: '.[ key != "" ]'`).map(([k]) => k),
    [ITEM, KEY, SEP, 'punctuation.definition.string.begin', OPEN, VAR, OP,
      'punctuation.definition.string.begin', 'punctuation.definition.string.end', CLOSE,
      'punctuation.definition.string.end']);
  // a key that looks like one is still a key, as in docs/tony.md
  check('.[x]: 1\n', [KEY, '.[x]'], [SEP, ':'], [INT, '1']);
  // and an unclosed one ends at the line
  check('k: .[x\nj: 1\n',
    [KEY, 'k'], [SEP, ':'], [OPEN, '.['], [VAR, 'x'], [KEY, 'j'], [SEP, ':'], [INT, '1']);
});

test('interpolations: in literals, strings and block literals', () => {
  // between two runs of literal
  assert.deepEqual(inner('image: $[images.prefix]-api$[images.suffix]'),
    [[KEY, 'image'], [SEP, ':'],
      [OPEN, '$['], [VAR, 'images'], ['punctuation', '.'], ['variable.other.property', 'prefix'], [CLOSE, ']'],
      [LIT, '-api'],
      [OPEN, '$['], [VAR, 'images'], ['punctuation', '.'], ['variable.other.property', 'suffix'], [CLOSE, ']']]);
  // inside a string, which keeps its own scope around it
  const s = grammar.tokenizeLine('k: "ns-$[namespace]"', vsctm.INITIAL).tokens
    .find((t) => t.scopes.at(-1) === 'variable.tony');
  assert.deepEqual(s.scopes, ['source.tony', 'string.quoted.double.tony', 'meta.interpolation.tony', 'variable.tony']);
  // inside a block literal's body
  const first = grammar.tokenizeLine('k: |', vsctm.INITIAL);
  const body = grammar.tokenizeLine('  hello $[name]', first.ruleStack).tokens
    .find((t) => t.scopes.at(-1) === 'variable.tony');
  assert.deepEqual(body.scopes, ['source.tony', 'meta.block-literal.tony', 'string.unquoted.block.tony',
    'meta.interpolation.tony', 'variable.tony']);
  // an unclosed '[' inside one in a block literal ends with the line, not the document
  check('k: |\n  $[a[\nj: 1\n',
    [KEY, 'k'], [SEP, ':'], [BLOCK, '|'], [BODY, '$[a['], [KEY, 'j'], [SEP, ':'], [INT, '1']);
  // a key that starts with one is a key, as the lexer has it: `{"$[x]": "v"}`
  check('$[x]: v\n', [KEY, '$[x]'], [SEP, ':'], [LIT, 'v']);
  // '$' alone is a literal's
  check('$y: "$y"\n', [KEY, '$y'], [SEP, ':'], [DQ, '"$y"']);
});

// Characters the grammar gives no scope a theme colours -- none but the root's and meta scopes,
// which only group -- reported as file:line.
function unscoped(code, where, firstLine) {
  const bad = [];
  let stack = vsctm.INITIAL;
  code.split('\n').forEach((line, i) => {
    const r = grammar.tokenizeLine(line, stack);
    for (const t of r.tokens) {
      const text = line.slice(t.startIndex, t.endIndex);
      if (t.scopes.every((s) => s === 'source.tony' || s.startsWith('meta.')) && text.trim() !== '') bad.push(`${where}:${firstLine + i}: ${JSON.stringify(text)}`);
    }
    stack = r.ruleStack;
  });
  return bad;
}

function* walk(dir, ext) {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    if (e.name.startsWith('.') || e.name === 'node_modules' || e.name === 'site') continue;
    const p = join(dir, e.name);
    if (e.isDirectory()) yield* walk(p, ext);
    else if (e.name.endsWith(ext)) yield p;
  }
}

test('a long line tokenizes in linear time', () => {
  // VS Code tokenizes lines up to 20,000 characters. These two took 8.4s and 7.6s when the key
  // and call rules were retried from every column of a run.
  for (const line of ['k: ' + 'a:'.repeat(20000), 'k: .[' + 'a-'.repeat(20000) + 'a]']) {
    const start = performance.now();
    grammar.tokenizeLine(line, vsctm.INITIAL);
    const ms = performance.now() - start;
    assert.ok(ms < 1000, `${line.slice(0, 12)}... took ${Math.round(ms)}ms`);
  }
});

test('the sweep reports what only a meta scope covers', () => {
  // '~' is no expr-lang operator, so nothing inside an expression scopes it
  assert.deepEqual(unscoped('k: .[x ~ y]', 't', 1), ['t:1: " ~ "']);
  assert.deepEqual(unscoped('k: .[x]', 't', 1), []);
});

test('every ```tony block in the repository is scoped', () => {
  const fence = /^([ \t]*)```+tony[^\n]*\n([\s\S]*?)^\1```+/gm;
  let blocks = 0;
  const bad = [];
  for (const md of walk(REPO, '.md')) {
    const text = readFileSync(md, 'utf8');
    for (const m of text.matchAll(fence)) {
      blocks++;
      const line = text.slice(0, m.index).split('\n').length + 1;
      bad.push(...unscoped(m[2], relative(REPO, md), line));
    }
  }
  assert.ok(blocks > 100, `expected the repository to hold tony blocks, found ${blocks}`);
  assert.deepEqual(bad.slice(0, 40), [], `unscoped characters in ${blocks} blocks`);
});

test('every .tony file in the repository is scoped', () => {
  let files = 0;
  const bad = [];
  for (const f of walk(REPO, '.tony')) {
    files++;
    bad.push(...unscoped(readFileSync(f, 'utf8'), relative(REPO, f), 1));
  }
  assert.ok(files > 50, `expected the repository to hold .tony files, found ${files}`);
  assert.deepEqual(bad.slice(0, 40), [], `unscoped characters in ${files} files`);
});
