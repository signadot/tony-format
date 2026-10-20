// Writes syntaxes/tony.tmLanguage.json, the TextMate grammar for Tony.
//
//     node build.mjs
//
// The grammar is built here rather than written as JSON so that its regexes can be assembled
// from named, commented parts, as docs/sketchy/pygments-tony/pygments_tony.py assembles its
// own. The two are meant to agree construct for construct: test/grammar.test.mjs is a port of
// that lexer's tests, and a difference between them is a bug in one or the other.
//
// The scope each construct is given is the one TextMate themes -- and GitHub -- already colour,
// chosen to match YAML's grammar where the construct is YAML's:
//
//     construct              scope
//     tag                    storage.type.tag.tony
//     comment                comment.line.number-sign.tony
//     object key             entity.name.tag.tony
//     sparse-array key       entity.name.tag.index.tony
//     merge key '<<'         support.constant.merge-key.tony
//     ':'                    punctuation.separator.key-value.mapping.tony
//     '-'                    punctuation.definition.block.sequence.item.tony
//     ',' and brackets       punctuation.separator / punctuation.definition.{mapping,sequence}
//     literal                string.unquoted.plain.tony
//     quoted string          string.quoted.{single,double}.tony
//     block literal          keyword.control.flow.block-scalar.literal.tony, then
//                            string.unquoted.block.tony for its body
//     number                 constant.numeric.*.tony
//     null, true, false      constant.language.{null,boolean}.tony
//     '---'                  entity.other.document.begin.tony
//     '.[x]', '$[x]'         meta.embedded.expression.tony, meta.interpolation.tony, with
//                            punctuation.section.embedded.{begin,end}.tony at either end
//       a name in one        variable.tony
//       a call              entity.name.function.tony
//       a field after '.'    variable.other.property.tony
//       an operator          keyword.operator.tony
//
// The lexer stops at that last row: it paints an expression as the literal it is to the
// parser. Expressions are a tenth of the lines of tony in this repository -- `.[string]` in
// every schema -- so painting them one colour left most of a document in two.

import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// A literal is a maximal run of non-whitespace characters, as defined in "Literals and
// Punctuation" in docs/tony.md. Whitespace ends it, and so does any punctuation outside the set
// Tony allows -- of which ',' '#' and the quote characters are the ones that turn up in practice.
const LIT_CH = String.raw`[^\s,#'"\[\]{}():]`;

// ':' is part of the literal ("a:b", "resolvers:security:review") unless it is the key
// separator, which is what a following space, ',' or end of line makes it.
const LIT_COLON = String.raw`:(?![\s,]|$)`;

// The paired characters belong to a literal when they are balanced: ".[x]", "$[y]", "f(a)".
// Nesting deeper than this is not worth a regex.
const LIT_PAIR = String.raw`\[[^\[\]\s]*\]|\{[^{}\s]*\}|\([^()\s]*\)`;

// A literal stops where an interpolation starts, so that the '$[x]' in "prefix-$[x]-suffix" is
// highlighted as one, between two runs of literal.
const LITERAL = String.raw`(?:(?!\$\[)${LIT_CH}|${LIT_COLON}|${LIT_PAIR})+`;

// A key is a literal that a ':' separator follows.
const IS_KEY = String.raw`(?=:(?:\s|$))`;

// A number is a number only when the literal run ends where the number does: "100m", "1.2.3"
// and "30s" are digit-leading strings, per "Numbers" in docs/tony.md, and a rule that stopped at
// the number-shaped prefix would paint the tail of each of them as something else.
const NUM_END = `(?!${LIT_CH}|${LIT_COLON})`;

// A tag is '!' followed by non-whitespace, with '.' composing tags and '(...)' carrying
// arguments that are themselves tags: "!retag(tag1.tag2(a,b),tag2(z))". Arguments hold commas,
// so they are matched as a unit rather than left to end the tag.
const TAG = String.raw`!(?:[^\s,#'"\[\]{}()]|\((?:[^()]|\([^()]*\))*\))+`;

// A block literal's body is what is indented to its content's level, and that level is not the
// column the '|' lands on but the opening line's, per "Block Literals" in docs/tony.md: one
// level in from the line, where each '- ' on the line is a level of its own and a field after
// the markers is one more.
//
//     k: |            content at 2        - |          content at 2
//     |               content at 2        - - |        content at 4
//     - k: |          content at 4        - - k: |     content at 6
//
// A TextMate end pattern can repeat what its begin matched but cannot count it, so each depth
// is a rule of its own, and the opening line's indentation is the \1 the end pattern compares
// against. The body runs until a line that is neither indented that far nor blank.
//
// MARKERS(n) is n '- ' markers; FIELD is anything else that ends in the space before the '|',
// a key or a merge key, so long as it is not itself a marker and holds no comment.
const MARKERS = (n) => '(?:- )'.repeat(n);
const FIELD = String.raw`(?!- )[^\s#][^\n#]*?[ \t]`;

function blockLiteral(depth) {
  // The prefixes that put content `depth` levels in: depth markers alone, or depth-1 markers
  // and a field. At depth 1 the first of those is a bare '- ' or nothing at all.
  const alone = depth === 1 ? `(?:${MARKERS(1)})?` : MARKERS(depth);
  const prefix = `(?:${alone}|${MARKERS(depth - 1)}${FIELD})`;
  return {
    name: 'meta.block-literal.tony',
    begin: String.raw`^([ \t]*)(${prefix})(\|[-+]?)([ \t]*(?:#.*)?)$`,
    beginCaptures: {
      2: { patterns: [{ include: '#inline' }] },
      3: { name: 'keyword.control.flow.block-scalar.literal.tony' },
      4: { patterns: [{ include: '#comment' }] },
    },
    end: String.raw`^(?!\1[ \t]{${2 * depth}}|[ \t]*$)`,
    contentName: 'string.unquoted.block.tony',
    patterns: [{ include: '#interpolation' }],
  };
}

const grammar = {
  $schema: 'https://raw.githubusercontent.com/martinring/tmlanguage/master/tmlanguage.json',
  name: 'Tony',
  scopeName: 'source.tony',
  fileTypes: ['tony'],
  patterns: [
    { include: '#document-separator' },
    { include: '#comment' },
    { include: '#block-literal' },
    { include: '#inline' },
  ],
  repository: {
    'document-separator': {
      match: String.raw`^---[ \t]*$`,
      name: 'entity.other.document.begin.tony',
    },

    // Unlike YAML a comment needs no preceding space, which is also why '#' ends a literal.
    comment: {
      match: '(#).*$',
      name: 'comment.line.number-sign.tony',
      captures: { 1: { name: 'punctuation.definition.comment.tony' } },
    },

    // Deepest first: the prefixes are exclusive, but a reader should not have to check that.
    'block-literal': {
      patterns: [4, 3, 2, 1].map(blockLiteral),
    },

    // Everything that can appear within a line. The order is the lexer's, and it matters where
    // two rules match at the same column: keys before constants, so that "null: x" reads as a
    // key, and numbers before literals.
    inline: {
      patterns: [
        { include: '#comment' },
        { match: '<<' + IS_KEY, name: 'support.constant.merge-key.tony' },
        { match: TAG, name: 'storage.type.tag.tony' },
        // Sparse-array keys, which are base-10 integers
        { match: String.raw`-?\d+` + IS_KEY, name: 'entity.name.tag.index.tony' },
        { include: '#string' },
        { include: '#number' },
        { match: LITERAL + IS_KEY, name: 'entity.name.tag.tony' },
        // After keys, so that ".[x]: v" is the key it is in docs/tony.md
        { include: '#expression' },
        { include: '#interpolation' },
        { match: 'null' + NUM_END, name: 'constant.language.null.tony' },
        { match: '(?:true|false)' + NUM_END, name: 'constant.language.boolean.tony' },
        { match: String.raw`:(?=\s|$)`, name: 'punctuation.separator.key-value.mapping.tony' },
        { match: String.raw`-(?=\s|$)`, name: 'punctuation.definition.block.sequence.item.tony' },
        { match: ',', name: 'punctuation.separator.tony' },
        { match: String.raw`\{`, name: 'punctuation.definition.mapping.begin.tony' },
        { match: String.raw`\}`, name: 'punctuation.definition.mapping.end.tony' },
        { match: String.raw`\[`, name: 'punctuation.definition.sequence.begin.tony' },
        { match: String.raw`\]`, name: 'punctuation.definition.sequence.end.tony' },
        { match: LITERAL, name: 'string.unquoted.plain.tony' },
      ],
    },

    // The two quotes share one escaping grammar, per "Quoting" in docs/tony.md. A string that is
    // not closed ends at the line rather than running on, so a missing quote spoils one line and
    // not the rest of the document.
    string: {
      patterns: [
        quoted('"', 'double'),
        quoted("'", 'single'),
      ],
    },

    expression: embedded(String.raw`\.\[`, 'meta.embedded.expression.tony'),
    interpolation: embedded(String.raw`\$\[`, 'meta.interpolation.tony'),

    // The inside of one: enough of expr-lang to tell names, calls, fields and operators apart.
    // A name may hold '-' and one ':' because a schema's names do: ".[tony-base:ir]".
    expr: {
      patterns: [
        // "\]" is a ']' that does not close the expression
        { match: String.raw`\\.`, name: 'constant.character.escape.tony' },
        { include: '#string' },
        {
          begin: String.raw`\[`,
          end: String.raw`\]`,
          beginCaptures: { 0: { name: 'punctuation.section.brackets.begin.tony' } },
          endCaptures: { 0: { name: 'punctuation.section.brackets.end.tony' } },
          patterns: [{ include: '#expr' }],
        },
        { match: String.raw`\b\d+(?:\.\d+)?\b`, name: 'constant.numeric.tony' },
        { match: String.raw`\b(?:true|false|nil|null)\b`, name: 'constant.language.tony' },
        { match: String.raw`[A-Za-z_][\w-]*(?=\()`, name: 'entity.name.function.tony' },
        { match: String.raw`(?<=\.)[A-Za-z_]\w*`, name: 'variable.other.property.tony' },
        { match: String.raw`[A-Za-z_$][\w-]*(?::[A-Za-z_][\w-]*)?`, name: 'variable.tony' },
        { match: String.raw`\?\?|\?\.|==|!=|<=|>=|&&|\|\||[-+*/%<>!?:]`, name: 'keyword.operator.tony' },
        { match: '[.,()]', name: 'punctuation.tony' },
      ],
    },

    number: {
      patterns: [
        { match: String.raw`-?0[xX][0-9a-fA-F]+` + NUM_END, name: 'constant.numeric.integer.hexadecimal.tony' },
        { match: String.raw`-?0[oO][0-7]+` + NUM_END, name: 'constant.numeric.integer.octal.tony' },
        { match: String.raw`-?0[bB][01]+` + NUM_END, name: 'constant.numeric.integer.binary.tony' },
        { match: String.raw`-?\d+(?:\.\d+)?[eE][-+]?\d+` + NUM_END, name: 'constant.numeric.float.tony' },
        { match: String.raw`-?\d+\.\d+` + NUM_END, name: 'constant.numeric.float.tony' },
        { match: String.raw`-?\d+` + NUM_END, name: 'constant.numeric.integer.tony' },
      ],
    },
  },
};

function quoted(q, kind) {
  return {
    name: `string.quoted.${kind}.tony`,
    begin: q,
    beginCaptures: { 0: { name: 'punctuation.definition.string.begin.tony' } },
    end: `(${q})|$`,
    endCaptures: { 1: { name: 'punctuation.definition.string.end.tony' } },
    patterns: [
      { match: String.raw`\\.`, name: 'constant.character.escape.tony' },
      { include: '#expression' },
      { include: '#interpolation' },
    ],
  };
}

// '.[x]' is the value of an expr-lang expression and '$[x]' a string holding it, per
// docs/eval.md. Both are evaluated only where an !eval says so, and are text everywhere else;
// they are highlighted everywhere, because that is where a reader looks for them -- in a quoted
// string too, which is how a build file writes a condition: `if: '.[ key != "" ]'`. Like a
// string, one that is not closed ends at the line.
function embedded(open, name) {
  return {
    name,
    begin: open,
    beginCaptures: { 0: { name: 'punctuation.section.embedded.begin.tony' } },
    end: String.raw`(\])|$`,
    endCaptures: { 1: { name: 'punctuation.section.embedded.end.tony' } },
    patterns: [{ include: '#expr' }],
  };
}

// The JSON is committed, because it is what linguist and editors read. --check fails if it is
// not what this file builds, which is how an edit to the JSON alone is caught.
const out = fileURLToPath(new URL('syntaxes/tony.tmLanguage.json', import.meta.url));
const json = JSON.stringify(grammar, null, 2) + '\n';
if (process.argv.includes('--check')) {
  if (readFileSync(out, 'utf8') !== json) {
    console.error(`${out} is stale: run node build.mjs`);
    process.exit(1);
  }
} else {
  writeFileSync(out, json);
}
