#include <tree_sitter/parser.h>

#if defined(__GNUC__) || defined(__clang__)
#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wmissing-field-initializers"
#endif

#define LANGUAGE_VERSION 14
#define STATE_COUNT 102
#define LARGE_STATE_COUNT 16
#define SYMBOL_COUNT 60
#define ALIAS_COUNT 0
#define TOKEN_COUNT 33
#define EXTERNAL_TOKEN_COUNT 0
#define FIELD_COUNT 0
#define MAX_ALIAS_SEQUENCE_LENGTH 6
#define PRODUCTION_ID_COUNT 1

enum {
  sym_comment = 1,
  anon_sym_DASH_DASH_DASH = 2,
  aux_sym_document_separator_token1 = 3,
  anon_sym_LBRACE = 4,
  anon_sym_RBRACE = 5,
  anon_sym_COMMA = 6,
  anon_sym_COLON = 7,
  anon_sym_LBRACK = 8,
  anon_sym_RBRACK = 9,
  anon_sym_BANG = 10,
  aux_sym_tag_token1 = 11,
  anon_sym_DOT = 12,
  anon_sym_LPAREN = 13,
  anon_sym_RPAREN = 14,
  anon_sym_DQUOTE = 15,
  aux_sym_string_token1 = 16,
  anon_sym_SQUOTE = 17,
  aux_sym_string_token2 = 18,
  sym_string_escape = 19,
  anon_sym_PIPE = 20,
  aux_sym_block_literal_token1 = 21,
  anon_sym_DOLLAR_LBRACK = 22,
  aux_sym_interpolation_token1 = 23,
  anon_sym_DOT_LBRACK = 24,
  sym_literal = 25,
  aux_sym_number_token1 = 26,
  aux_sym_number_token2 = 27,
  aux_sym_number_token3 = 28,
  aux_sym_number_token4 = 29,
  anon_sym_true = 30,
  anon_sym_false = 31,
  sym_null = 32,
  sym_source_file = 33,
  sym__top_level = 34,
  sym_document_separator = 35,
  sym__value = 36,
  sym_bracketed_map = 37,
  sym__map_content = 38,
  sym__map_entry = 39,
  sym_bracketed_array = 40,
  sym__array_content = 41,
  sym__key = 42,
  sym_tag = 43,
  sym__tag_arguments = 44,
  sym_tagged_value = 45,
  sym_tagged_key = 46,
  sym_string = 47,
  sym_block_literal = 48,
  sym_interpolation = 49,
  sym_node_replacement = 50,
  sym_number = 51,
  sym_boolean = 52,
  aux_sym_source_file_repeat1 = 53,
  aux_sym__map_content_repeat1 = 54,
  aux_sym__array_content_repeat1 = 55,
  aux_sym_tag_repeat1 = 56,
  aux_sym_string_repeat1 = 57,
  aux_sym_string_repeat2 = 58,
  aux_sym_block_literal_repeat1 = 59,
};

static const char * const ts_symbol_names[] = {
  [ts_builtin_sym_end] = "end",
  [sym_comment] = "comment",
  [anon_sym_DASH_DASH_DASH] = "---",
  [aux_sym_document_separator_token1] = "document_separator_token1",
  [anon_sym_LBRACE] = "{",
  [anon_sym_RBRACE] = "}",
  [anon_sym_COMMA] = ",",
  [anon_sym_COLON] = ":",
  [anon_sym_LBRACK] = "[",
  [anon_sym_RBRACK] = "]",
  [anon_sym_BANG] = "!",
  [aux_sym_tag_token1] = "tag_token1",
  [anon_sym_DOT] = ".",
  [anon_sym_LPAREN] = "(",
  [anon_sym_RPAREN] = ")",
  [anon_sym_DQUOTE] = "\"",
  [aux_sym_string_token1] = "string_token1",
  [anon_sym_SQUOTE] = "'",
  [aux_sym_string_token2] = "string_token2",
  [sym_string_escape] = "string_escape",
  [anon_sym_PIPE] = "|",
  [aux_sym_block_literal_token1] = "block_literal_token1",
  [anon_sym_DOLLAR_LBRACK] = "$[",
  [aux_sym_interpolation_token1] = "interpolation_token1",
  [anon_sym_DOT_LBRACK] = ".[",
  [sym_literal] = "literal",
  [aux_sym_number_token1] = "number_token1",
  [aux_sym_number_token2] = "number_token2",
  [aux_sym_number_token3] = "number_token3",
  [aux_sym_number_token4] = "number_token4",
  [anon_sym_true] = "true",
  [anon_sym_false] = "false",
  [sym_null] = "null",
  [sym_source_file] = "source_file",
  [sym__top_level] = "_top_level",
  [sym_document_separator] = "document_separator",
  [sym__value] = "_value",
  [sym_bracketed_map] = "bracketed_map",
  [sym__map_content] = "_map_content",
  [sym__map_entry] = "_map_entry",
  [sym_bracketed_array] = "bracketed_array",
  [sym__array_content] = "_array_content",
  [sym__key] = "_key",
  [sym_tag] = "tag",
  [sym__tag_arguments] = "_tag_arguments",
  [sym_tagged_value] = "tagged_value",
  [sym_tagged_key] = "tagged_key",
  [sym_string] = "string",
  [sym_block_literal] = "block_literal",
  [sym_interpolation] = "interpolation",
  [sym_node_replacement] = "node_replacement",
  [sym_number] = "number",
  [sym_boolean] = "boolean",
  [aux_sym_source_file_repeat1] = "source_file_repeat1",
  [aux_sym__map_content_repeat1] = "_map_content_repeat1",
  [aux_sym__array_content_repeat1] = "_array_content_repeat1",
  [aux_sym_tag_repeat1] = "tag_repeat1",
  [aux_sym_string_repeat1] = "string_repeat1",
  [aux_sym_string_repeat2] = "string_repeat2",
  [aux_sym_block_literal_repeat1] = "block_literal_repeat1",
};

static const TSSymbol ts_symbol_map[] = {
  [ts_builtin_sym_end] = ts_builtin_sym_end,
  [sym_comment] = sym_comment,
  [anon_sym_DASH_DASH_DASH] = anon_sym_DASH_DASH_DASH,
  [aux_sym_document_separator_token1] = aux_sym_document_separator_token1,
  [anon_sym_LBRACE] = anon_sym_LBRACE,
  [anon_sym_RBRACE] = anon_sym_RBRACE,
  [anon_sym_COMMA] = anon_sym_COMMA,
  [anon_sym_COLON] = anon_sym_COLON,
  [anon_sym_LBRACK] = anon_sym_LBRACK,
  [anon_sym_RBRACK] = anon_sym_RBRACK,
  [anon_sym_BANG] = anon_sym_BANG,
  [aux_sym_tag_token1] = aux_sym_tag_token1,
  [anon_sym_DOT] = anon_sym_DOT,
  [anon_sym_LPAREN] = anon_sym_LPAREN,
  [anon_sym_RPAREN] = anon_sym_RPAREN,
  [anon_sym_DQUOTE] = anon_sym_DQUOTE,
  [aux_sym_string_token1] = aux_sym_string_token1,
  [anon_sym_SQUOTE] = anon_sym_SQUOTE,
  [aux_sym_string_token2] = aux_sym_string_token2,
  [sym_string_escape] = sym_string_escape,
  [anon_sym_PIPE] = anon_sym_PIPE,
  [aux_sym_block_literal_token1] = aux_sym_block_literal_token1,
  [anon_sym_DOLLAR_LBRACK] = anon_sym_DOLLAR_LBRACK,
  [aux_sym_interpolation_token1] = aux_sym_interpolation_token1,
  [anon_sym_DOT_LBRACK] = anon_sym_DOT_LBRACK,
  [sym_literal] = sym_literal,
  [aux_sym_number_token1] = aux_sym_number_token1,
  [aux_sym_number_token2] = aux_sym_number_token2,
  [aux_sym_number_token3] = aux_sym_number_token3,
  [aux_sym_number_token4] = aux_sym_number_token4,
  [anon_sym_true] = anon_sym_true,
  [anon_sym_false] = anon_sym_false,
  [sym_null] = sym_null,
  [sym_source_file] = sym_source_file,
  [sym__top_level] = sym__top_level,
  [sym_document_separator] = sym_document_separator,
  [sym__value] = sym__value,
  [sym_bracketed_map] = sym_bracketed_map,
  [sym__map_content] = sym__map_content,
  [sym__map_entry] = sym__map_entry,
  [sym_bracketed_array] = sym_bracketed_array,
  [sym__array_content] = sym__array_content,
  [sym__key] = sym__key,
  [sym_tag] = sym_tag,
  [sym__tag_arguments] = sym__tag_arguments,
  [sym_tagged_value] = sym_tagged_value,
  [sym_tagged_key] = sym_tagged_key,
  [sym_string] = sym_string,
  [sym_block_literal] = sym_block_literal,
  [sym_interpolation] = sym_interpolation,
  [sym_node_replacement] = sym_node_replacement,
  [sym_number] = sym_number,
  [sym_boolean] = sym_boolean,
  [aux_sym_source_file_repeat1] = aux_sym_source_file_repeat1,
  [aux_sym__map_content_repeat1] = aux_sym__map_content_repeat1,
  [aux_sym__array_content_repeat1] = aux_sym__array_content_repeat1,
  [aux_sym_tag_repeat1] = aux_sym_tag_repeat1,
  [aux_sym_string_repeat1] = aux_sym_string_repeat1,
  [aux_sym_string_repeat2] = aux_sym_string_repeat2,
  [aux_sym_block_literal_repeat1] = aux_sym_block_literal_repeat1,
};

static const TSSymbolMetadata ts_symbol_metadata[] = {
  [ts_builtin_sym_end] = {
    .visible = false,
    .named = true,
  },
  [sym_comment] = {
    .visible = true,
    .named = true,
  },
  [anon_sym_DASH_DASH_DASH] = {
    .visible = true,
    .named = false,
  },
  [aux_sym_document_separator_token1] = {
    .visible = false,
    .named = false,
  },
  [anon_sym_LBRACE] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_RBRACE] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_COMMA] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_COLON] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_LBRACK] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_RBRACK] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_BANG] = {
    .visible = true,
    .named = false,
  },
  [aux_sym_tag_token1] = {
    .visible = false,
    .named = false,
  },
  [anon_sym_DOT] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_LPAREN] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_RPAREN] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_DQUOTE] = {
    .visible = true,
    .named = false,
  },
  [aux_sym_string_token1] = {
    .visible = false,
    .named = false,
  },
  [anon_sym_SQUOTE] = {
    .visible = true,
    .named = false,
  },
  [aux_sym_string_token2] = {
    .visible = false,
    .named = false,
  },
  [sym_string_escape] = {
    .visible = true,
    .named = true,
  },
  [anon_sym_PIPE] = {
    .visible = true,
    .named = false,
  },
  [aux_sym_block_literal_token1] = {
    .visible = false,
    .named = false,
  },
  [anon_sym_DOLLAR_LBRACK] = {
    .visible = true,
    .named = false,
  },
  [aux_sym_interpolation_token1] = {
    .visible = false,
    .named = false,
  },
  [anon_sym_DOT_LBRACK] = {
    .visible = true,
    .named = false,
  },
  [sym_literal] = {
    .visible = true,
    .named = true,
  },
  [aux_sym_number_token1] = {
    .visible = false,
    .named = false,
  },
  [aux_sym_number_token2] = {
    .visible = false,
    .named = false,
  },
  [aux_sym_number_token3] = {
    .visible = false,
    .named = false,
  },
  [aux_sym_number_token4] = {
    .visible = false,
    .named = false,
  },
  [anon_sym_true] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_false] = {
    .visible = true,
    .named = false,
  },
  [sym_null] = {
    .visible = true,
    .named = true,
  },
  [sym_source_file] = {
    .visible = true,
    .named = true,
  },
  [sym__top_level] = {
    .visible = false,
    .named = true,
  },
  [sym_document_separator] = {
    .visible = true,
    .named = true,
  },
  [sym__value] = {
    .visible = false,
    .named = true,
  },
  [sym_bracketed_map] = {
    .visible = true,
    .named = true,
  },
  [sym__map_content] = {
    .visible = false,
    .named = true,
  },
  [sym__map_entry] = {
    .visible = false,
    .named = true,
  },
  [sym_bracketed_array] = {
    .visible = true,
    .named = true,
  },
  [sym__array_content] = {
    .visible = false,
    .named = true,
  },
  [sym__key] = {
    .visible = false,
    .named = true,
  },
  [sym_tag] = {
    .visible = true,
    .named = true,
  },
  [sym__tag_arguments] = {
    .visible = false,
    .named = true,
  },
  [sym_tagged_value] = {
    .visible = true,
    .named = true,
  },
  [sym_tagged_key] = {
    .visible = true,
    .named = true,
  },
  [sym_string] = {
    .visible = true,
    .named = true,
  },
  [sym_block_literal] = {
    .visible = true,
    .named = true,
  },
  [sym_interpolation] = {
    .visible = true,
    .named = true,
  },
  [sym_node_replacement] = {
    .visible = true,
    .named = true,
  },
  [sym_number] = {
    .visible = true,
    .named = true,
  },
  [sym_boolean] = {
    .visible = true,
    .named = true,
  },
  [aux_sym_source_file_repeat1] = {
    .visible = false,
    .named = false,
  },
  [aux_sym__map_content_repeat1] = {
    .visible = false,
    .named = false,
  },
  [aux_sym__array_content_repeat1] = {
    .visible = false,
    .named = false,
  },
  [aux_sym_tag_repeat1] = {
    .visible = false,
    .named = false,
  },
  [aux_sym_string_repeat1] = {
    .visible = false,
    .named = false,
  },
  [aux_sym_string_repeat2] = {
    .visible = false,
    .named = false,
  },
  [aux_sym_block_literal_repeat1] = {
    .visible = false,
    .named = false,
  },
};

static const TSSymbol ts_alias_sequences[PRODUCTION_ID_COUNT][MAX_ALIAS_SEQUENCE_LENGTH] = {
  [0] = {0},
};

static const uint16_t ts_non_terminal_alias_map[] = {
  0,
};

static const TSStateId ts_primary_state_ids[STATE_COUNT] = {
  [0] = 0,
  [1] = 1,
  [2] = 2,
  [3] = 3,
  [4] = 4,
  [5] = 4,
  [6] = 6,
  [7] = 7,
  [8] = 8,
  [9] = 4,
  [10] = 10,
  [11] = 4,
  [12] = 6,
  [13] = 10,
  [14] = 14,
  [15] = 7,
  [16] = 16,
  [17] = 17,
  [18] = 18,
  [19] = 19,
  [20] = 20,
  [21] = 21,
  [22] = 22,
  [23] = 23,
  [24] = 24,
  [25] = 25,
  [26] = 26,
  [27] = 27,
  [28] = 28,
  [29] = 29,
  [30] = 30,
  [31] = 19,
  [32] = 17,
  [33] = 18,
  [34] = 34,
  [35] = 35,
  [36] = 36,
  [37] = 37,
  [38] = 38,
  [39] = 28,
  [40] = 40,
  [41] = 37,
  [42] = 36,
  [43] = 38,
  [44] = 44,
  [45] = 45,
  [46] = 46,
  [47] = 47,
  [48] = 48,
  [49] = 49,
  [50] = 34,
  [51] = 35,
  [52] = 40,
  [53] = 53,
  [54] = 35,
  [55] = 55,
  [56] = 34,
  [57] = 57,
  [58] = 40,
  [59] = 35,
  [60] = 34,
  [61] = 17,
  [62] = 20,
  [63] = 40,
  [64] = 18,
  [65] = 19,
  [66] = 66,
  [67] = 67,
  [68] = 68,
  [69] = 67,
  [70] = 70,
  [71] = 71,
  [72] = 72,
  [73] = 28,
  [74] = 74,
  [75] = 75,
  [76] = 76,
  [77] = 77,
  [78] = 78,
  [79] = 79,
  [80] = 80,
  [81] = 81,
  [82] = 82,
  [83] = 83,
  [84] = 84,
  [85] = 85,
  [86] = 80,
  [87] = 87,
  [88] = 88,
  [89] = 84,
  [90] = 88,
  [91] = 91,
  [92] = 92,
  [93] = 85,
  [94] = 94,
  [95] = 95,
  [96] = 94,
  [97] = 97,
  [98] = 83,
  [99] = 94,
  [100] = 85,
  [101] = 101,
};

static inline bool sym_literal_character_set_1(int32_t c) {
  return (c < '='
    ? (c < '('
      ? (c < '$'
        ? c == '!'
        : c <= '%')
      : (c <= '*' || (c >= '-' && c <= ':')))
    : (c <= '=' || (c < 'a'
      ? (c < '_'
        ? (c >= '@' && c <= ']')
        : c <= '_')
      : (c <= '{' || (c >= '}' && c <= '~')))));
}

static inline bool sym_literal_character_set_2(int32_t c) {
  return (c < '='
    ? (c < '('
      ? (c < '$'
        ? c == '!'
        : c <= '%')
      : (c <= '*' || (c >= '.' && c <= ':')))
    : (c <= '=' || (c < 'a'
      ? (c < '_'
        ? (c >= '@' && c <= ']')
        : c <= '_')
      : (c <= '{' || (c >= '}' && c <= '~')))));
}

static inline bool sym_literal_character_set_3(int32_t c) {
  return (c < '='
    ? (c < '('
      ? (c < '$'
        ? c == '!'
        : c <= '%')
      : (c <= '*' || (c >= '-' && c <= ':')))
    : (c <= '=' || (c < 'b'
      ? (c < '_'
        ? (c >= '@' && c <= ']')
        : c <= '_')
      : (c <= '{' || (c >= '}' && c <= '~')))));
}

static inline bool sym_literal_character_set_4(int32_t c) {
  return (c < '='
    ? (c < '('
      ? (c < '$'
        ? c == '!'
        : c <= '%')
      : (c <= '*' || (c >= '-' && c <= ':')))
    : (c <= '=' || (c < 'g'
      ? (c < '_'
        ? (c >= '@' && c <= ']')
        : c <= '_')
      : (c <= '{' || (c >= '}' && c <= '~')))));
}

static bool ts_lex(TSLexer *lexer, TSStateId state) {
  START_LEXER();
  eof = lexer->eof(lexer);
  switch (state) {
    case 0:
      if (eof) ADVANCE(28);
      if (lookahead == '\n') ADVANCE(34);
      if (lookahead == '!') ADVANCE(44);
      if (lookahead == '"') ADVANCE(50);
      if (lookahead == '#') ADVANCE(32);
      if (lookahead == '$') ADVANCE(60);
      if (lookahead == '\'') ADVANCE(52);
      if (lookahead == '(') ADVANCE(48);
      if (lookahead == ')') ADVANCE(49);
      if (lookahead == ',') ADVANCE(40);
      if (lookahead == '-') ADVANCE(57);
      if (lookahead == '.') ADVANCE(47);
      if (lookahead == '0') ADVANCE(58);
      if (lookahead == ':') ADVANCE(41);
      if (lookahead == '[') ADVANCE(42);
      if (lookahead == ']') ADVANCE(43);
      if (lookahead == 'f') ADVANCE(62);
      if (lookahead == 'n') ADVANCE(64);
      if (lookahead == 't') ADVANCE(63);
      if (lookahead == '{') ADVANCE(38);
      if (lookahead == '|') ADVANCE(55);
      if (lookahead == '}') ADVANCE(39);
      if (lookahead == '\t' ||
          lookahead == '\r' ||
          lookahead == ' ') ADVANCE(34);
      if (('1' <= lookahead && lookahead <= '9')) ADVANCE(59);
      if (lookahead == '%' ||
          ('*' <= lookahead && lookahead <= '/') ||
          lookahead == '=' ||
          ('@' <= lookahead && lookahead <= '\\') ||
          lookahead == '_' ||
          ('a' <= lookahead && lookahead <= '~')) ADVANCE(65);
      if (lookahead != 0) ADVANCE(56);
      END_STATE();
    case 1:
      if (lookahead == '\n') ADVANCE(34);
      if (lookahead == '#') ADVANCE(32);
      if (lookahead == ')') ADVANCE(49);
      if (lookahead == ',') ADVANCE(40);
      if (lookahead == '\t' ||
          lookahead == '\r' ||
          lookahead == ' ') ADVANCE(34);
      if (lookahead != 0) ADVANCE(56);
      END_STATE();
    case 2:
      if (lookahead == '\n') ADVANCE(34);
      if (lookahead == '#') ADVANCE(32);
      if (lookahead == ',') ADVANCE(40);
      if (lookahead == ']') ADVANCE(43);
      if (lookahead == '\t' ||
          lookahead == '\r' ||
          lookahead == ' ') ADVANCE(34);
      if (lookahead != 0) ADVANCE(56);
      END_STATE();
    case 3:
      if (lookahead == '\n') ADVANCE(34);
      if (lookahead == '#') ADVANCE(32);
      if (lookahead == ',') ADVANCE(40);
      if (lookahead == '}') ADVANCE(39);
      if (lookahead == '\t' ||
          lookahead == '\r' ||
          lookahead == ' ') ADVANCE(34);
      if (lookahead != 0) ADVANCE(56);
      END_STATE();
    case 4:
      if (lookahead == '!') ADVANCE(44);
      if (lookahead == '"') ADVANCE(50);
      if (lookahead == '#') ADVANCE(32);
      if (lookahead == '$') ADVANCE(77);
      if (lookahead == '\'') ADVANCE(52);
      if (lookahead == '(') ADVANCE(48);
      if (lookahead == ')') ADVANCE(49);
      if (lookahead == ',') ADVANCE(40);
      if (lookahead == '-') ADVANCE(76);
      if (lookahead == '.') ADVANCE(47);
      if (lookahead == '0') ADVANCE(97);
      if (lookahead == '[') ADVANCE(42);
      if (lookahead == ']') ADVANCE(43);
      if (lookahead == 'f') ADVANCE(79);
      if (lookahead == 'n') ADVANCE(88);
      if (lookahead == 't') ADVANCE(85);
      if (lookahead == '{') ADVANCE(38);
      if (lookahead == '|') ADVANCE(55);
      if (lookahead == '}') ADVANCE(39);
      if (lookahead == '\t' ||
          lookahead == '\n' ||
          lookahead == '\r' ||
          lookahead == ' ') ADVANCE(34);
      if (('1' <= lookahead && lookahead <= '9')) ADVANCE(98);
      if (lookahead == '%' ||
          ('*' <= lookahead && lookahead <= '/') ||
          lookahead == '=' ||
          ('@' <= lookahead && lookahead <= '\\') ||
          lookahead == '_' ||
          ('a' <= lookahead && lookahead <= '~')) ADVANCE(94);
      END_STATE();
    case 5:
      if (lookahead == '!') ADVANCE(44);
      if (lookahead == '"') ADVANCE(50);
      if (lookahead == '#') ADVANCE(32);
      if (lookahead == '$') ADVANCE(77);
      if (lookahead == '\'') ADVANCE(52);
      if (lookahead == ')') ADVANCE(49);
      if (lookahead == ',') ADVANCE(40);
      if (lookahead == '-') ADVANCE(76);
      if (lookahead == '.') ADVANCE(78);
      if (lookahead == '0') ADVANCE(97);
      if (lookahead == '[') ADVANCE(42);
      if (lookahead == ']') ADVANCE(43);
      if (lookahead == 'f') ADVANCE(79);
      if (lookahead == 'n') ADVANCE(88);
      if (lookahead == 't') ADVANCE(85);
      if (lookahead == '{') ADVANCE(38);
      if (lookahead == '|') ADVANCE(55);
      if (lookahead == '}') ADVANCE(39);
      if (lookahead == '\t' ||
          lookahead == '\n' ||
          lookahead == '\r' ||
          lookahead == ' ') ADVANCE(34);
      if (('1' <= lookahead && lookahead <= '9')) ADVANCE(98);
      if (lookahead == '%' ||
          ('*' <= lookahead && lookahead <= '/') ||
          lookahead == '=' ||
          ('@' <= lookahead && lookahead <= '\\') ||
          lookahead == '_' ||
          ('a' <= lookahead && lookahead <= '~')) ADVANCE(94);
      END_STATE();
    case 6:
      if (lookahead == '!') ADVANCE(44);
      if (lookahead == '"') ADVANCE(50);
      if (lookahead == '#') ADVANCE(32);
      if (lookahead == '$') ADVANCE(77);
      if (lookahead == '\'') ADVANCE(52);
      if (lookahead == ',') ADVANCE(40);
      if (lookahead == '.') ADVANCE(78);
      if (lookahead == '}') ADVANCE(39);
      if (lookahead == '\t' ||
          lookahead == '\n' ||
          lookahead == '\r' ||
          lookahead == ' ') ADVANCE(34);
      if (lookahead == '%' ||
          ('*' <= lookahead && lookahead <= '/') ||
          lookahead == '=' ||
          ('@' <= lookahead && lookahead <= 'Z') ||
          lookahead == '\\' ||
          lookahead == '_' ||
          ('a' <= lookahead && lookahead <= 'z') ||
          lookahead == '~') ADVANCE(94);
      END_STATE();
    case 7:
      if (lookahead == '"') ADVANCE(50);
      if (lookahead == '#') ADVANCE(29);
      if (lookahead == '$') ADVANCE(12);
      if (lookahead == '\\') ADVANCE(13);
      if (lookahead == '\t' ||
          lookahead == '\n' ||
          lookahead == '\r' ||
          lookahead == ' ') ADVANCE(35);
      if (lookahead != 0) ADVANCE(51);
      END_STATE();
    case 8:
      if (lookahead == '#') ADVANCE(32);
      if (lookahead == '(') ADVANCE(48);
      if (lookahead == '.') ADVANCE(46);
      if (lookahead == ':') ADVANCE(41);
      if (lookahead == '\t' ||
          lookahead == '\n' ||
          lookahead == '\r' ||
          lookahead == ' ') ADVANCE(34);
      END_STATE();
    case 9:
      if (lookahead == '#') ADVANCE(32);
      if (lookahead == '\t' ||
          lookahead == '\n' ||
          lookahead == '\r' ||
          lookahead == ' ') ADVANCE(34);
      if (('$' <= lookahead && lookahead <= '&') ||
          lookahead == '*' ||
          lookahead == '+' ||
          ('-' <= lookahead && lookahead <= ':') ||
          lookahead == '=' ||
          ('@' <= lookahead && lookahead <= 'Z') ||
          lookahead == '^' ||
          lookahead == '_' ||
          ('a' <= lookahead && lookahead <= 'z') ||
          lookahead == '~') ADVANCE(45);
      END_STATE();
    case 10:
      if (lookahead == '#') ADVANCE(30);
      if (lookahead == '\'') ADVANCE(52);
      if (lookahead == '\\') ADVANCE(13);
      if (lookahead == '\t' ||
          lookahead == '\n' ||
          lookahead == '\r' ||
          lookahead == ' ') ADVANCE(36);
      if (lookahead != 0) ADVANCE(53);
      END_STATE();
    case 11:
      if (lookahead == '#') ADVANCE(31);
      if (lookahead == '\t' ||
          lookahead == '\n' ||
          lookahead == '\r' ||
          lookahead == ' ') ADVANCE(37);
      if (lookahead != 0 &&
          lookahead != ']') ADVANCE(68);
      END_STATE();
    case 12:
      if (lookahead == '[') ADVANCE(66);
      END_STATE();
    case 13:
      if (lookahead == 'u') ADVANCE(24);
      if (lookahead == '"' ||
          lookahead == '\'' ||
          lookahead == '\\' ||
          lookahead == 'b' ||
          lookahead == 'f' ||
          lookahead == 'n' ||
          lookahead == 'r' ||
          lookahead == 't') ADVANCE(54);
      END_STATE();
    case 14:
      if (lookahead == '+' ||
          lookahead == '-') ADVANCE(18);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(99);
      END_STATE();
    case 15:
      if (lookahead == '+' ||
          lookahead == '-') ADVANCE(19);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(96);
      END_STATE();
    case 16:
      if (('0' <= lookahead && lookahead <= '7')) ADVANCE(101);
      END_STATE();
    case 17:
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(95);
      END_STATE();
    case 18:
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(99);
      END_STATE();
    case 19:
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(96);
      END_STATE();
    case 20:
      if (('0' <= lookahead && lookahead <= '9') ||
          ('A' <= lookahead && lookahead <= 'F') ||
          ('a' <= lookahead && lookahead <= 'f')) ADVANCE(100);
      END_STATE();
    case 21:
      if (('0' <= lookahead && lookahead <= '9') ||
          ('A' <= lookahead && lookahead <= 'F') ||
          ('a' <= lookahead && lookahead <= 'f')) ADVANCE(54);
      END_STATE();
    case 22:
      if (('0' <= lookahead && lookahead <= '9') ||
          ('A' <= lookahead && lookahead <= 'F') ||
          ('a' <= lookahead && lookahead <= 'f')) ADVANCE(21);
      END_STATE();
    case 23:
      if (('0' <= lookahead && lookahead <= '9') ||
          ('A' <= lookahead && lookahead <= 'F') ||
          ('a' <= lookahead && lookahead <= 'f')) ADVANCE(22);
      END_STATE();
    case 24:
      if (('0' <= lookahead && lookahead <= '9') ||
          ('A' <= lookahead && lookahead <= 'F') ||
          ('a' <= lookahead && lookahead <= 'f')) ADVANCE(23);
      END_STATE();
    case 25:
      if (eof) ADVANCE(28);
      if (lookahead == '\n') ADVANCE(34);
      if (lookahead == '!') ADVANCE(44);
      if (lookahead == '"') ADVANCE(50);
      if (lookahead == '#') ADVANCE(32);
      if (lookahead == '$') ADVANCE(60);
      if (lookahead == '\'') ADVANCE(52);
      if (lookahead == '-') ADVANCE(57);
      if (lookahead == '.') ADVANCE(61);
      if (lookahead == '0') ADVANCE(58);
      if (lookahead == '[') ADVANCE(42);
      if (lookahead == 'f') ADVANCE(62);
      if (lookahead == 'n') ADVANCE(64);
      if (lookahead == 't') ADVANCE(63);
      if (lookahead == '{') ADVANCE(38);
      if (lookahead == '|') ADVANCE(55);
      if (lookahead == '\t' ||
          lookahead == '\r' ||
          lookahead == ' ') ADVANCE(34);
      if (('1' <= lookahead && lookahead <= '9')) ADVANCE(59);
      if (lookahead == '%' ||
          lookahead == '*' ||
          lookahead == '+' ||
          lookahead == '/' ||
          lookahead == '=' ||
          ('@' <= lookahead && lookahead <= '\\') ||
          lookahead == '_' ||
          ('a' <= lookahead && lookahead <= 'z') ||
          lookahead == '~') ADVANCE(65);
      if (lookahead != 0) ADVANCE(56);
      END_STATE();
    case 26:
      if (eof) ADVANCE(28);
      if (lookahead == '!') ADVANCE(44);
      if (lookahead == '"') ADVANCE(50);
      if (lookahead == '#') ADVANCE(32);
      if (lookahead == '$') ADVANCE(77);
      if (lookahead == '\'') ADVANCE(52);
      if (lookahead == '(') ADVANCE(48);
      if (lookahead == '-') ADVANCE(72);
      if (lookahead == '.') ADVANCE(47);
      if (lookahead == '0') ADVANCE(97);
      if (lookahead == '[') ADVANCE(42);
      if (lookahead == 'f') ADVANCE(79);
      if (lookahead == 'n') ADVANCE(88);
      if (lookahead == 't') ADVANCE(85);
      if (lookahead == '{') ADVANCE(38);
      if (lookahead == '|') ADVANCE(55);
      if (lookahead == '\t' ||
          lookahead == '\n' ||
          lookahead == '\r' ||
          lookahead == ' ') ADVANCE(34);
      if (('1' <= lookahead && lookahead <= '9')) ADVANCE(98);
      if (lookahead == '%' ||
          lookahead == '*' ||
          lookahead == '+' ||
          lookahead == '/' ||
          lookahead == '=' ||
          ('@' <= lookahead && lookahead <= '\\') ||
          lookahead == '_' ||
          ('a' <= lookahead && lookahead <= 'z') ||
          lookahead == '~') ADVANCE(94);
      END_STATE();
    case 27:
      if (eof) ADVANCE(28);
      if (lookahead == '!') ADVANCE(44);
      if (lookahead == '"') ADVANCE(50);
      if (lookahead == '#') ADVANCE(32);
      if (lookahead == '$') ADVANCE(77);
      if (lookahead == '\'') ADVANCE(52);
      if (lookahead == ')') ADVANCE(49);
      if (lookahead == ',') ADVANCE(40);
      if (lookahead == '-') ADVANCE(72);
      if (lookahead == '.') ADVANCE(78);
      if (lookahead == '0') ADVANCE(97);
      if (lookahead == ':') ADVANCE(41);
      if (lookahead == '[') ADVANCE(42);
      if (lookahead == ']') ADVANCE(43);
      if (lookahead == 'f') ADVANCE(79);
      if (lookahead == 'n') ADVANCE(88);
      if (lookahead == 't') ADVANCE(85);
      if (lookahead == '{') ADVANCE(38);
      if (lookahead == '|') ADVANCE(55);
      if (lookahead == '}') ADVANCE(39);
      if (lookahead == '\t' ||
          lookahead == '\n' ||
          lookahead == '\r' ||
          lookahead == ' ') ADVANCE(34);
      if (('1' <= lookahead && lookahead <= '9')) ADVANCE(98);
      if (lookahead == '%' ||
          ('*' <= lookahead && lookahead <= '/') ||
          lookahead == '=' ||
          ('@' <= lookahead && lookahead <= '\\') ||
          lookahead == '_' ||
          ('a' <= lookahead && lookahead <= '~')) ADVANCE(94);
      END_STATE();
    case 28:
      ACCEPT_TOKEN(ts_builtin_sym_end);
      END_STATE();
    case 29:
      ACCEPT_TOKEN(sym_comment);
      if (lookahead == '\n') ADVANCE(51);
      if (lookahead == '"' ||
          lookahead == '$' ||
          lookahead == '\\') ADVANCE(32);
      if (lookahead != 0) ADVANCE(29);
      END_STATE();
    case 30:
      ACCEPT_TOKEN(sym_comment);
      if (lookahead == '\n') ADVANCE(53);
      if (lookahead == '\'' ||
          lookahead == '\\') ADVANCE(32);
      if (lookahead != 0) ADVANCE(30);
      END_STATE();
    case 31:
      ACCEPT_TOKEN(sym_comment);
      if (lookahead == '\n') ADVANCE(68);
      if (lookahead == ']') ADVANCE(32);
      if (lookahead != 0) ADVANCE(31);
      END_STATE();
    case 32:
      ACCEPT_TOKEN(sym_comment);
      if (lookahead != 0 &&
          lookahead != '\n') ADVANCE(32);
      END_STATE();
    case 33:
      ACCEPT_TOKEN(anon_sym_DASH_DASH_DASH);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 34:
      ACCEPT_TOKEN(aux_sym_document_separator_token1);
      END_STATE();
    case 35:
      ACCEPT_TOKEN(aux_sym_document_separator_token1);
      if (lookahead != 0 &&
          lookahead != '"' &&
          lookahead != '$' &&
          lookahead != '\\') ADVANCE(51);
      END_STATE();
    case 36:
      ACCEPT_TOKEN(aux_sym_document_separator_token1);
      if (lookahead != 0 &&
          lookahead != '\'' &&
          lookahead != '\\') ADVANCE(53);
      END_STATE();
    case 37:
      ACCEPT_TOKEN(aux_sym_document_separator_token1);
      if (lookahead != 0 &&
          lookahead != ']') ADVANCE(68);
      END_STATE();
    case 38:
      ACCEPT_TOKEN(anon_sym_LBRACE);
      END_STATE();
    case 39:
      ACCEPT_TOKEN(anon_sym_RBRACE);
      END_STATE();
    case 40:
      ACCEPT_TOKEN(anon_sym_COMMA);
      END_STATE();
    case 41:
      ACCEPT_TOKEN(anon_sym_COLON);
      END_STATE();
    case 42:
      ACCEPT_TOKEN(anon_sym_LBRACK);
      END_STATE();
    case 43:
      ACCEPT_TOKEN(anon_sym_RBRACK);
      END_STATE();
    case 44:
      ACCEPT_TOKEN(anon_sym_BANG);
      END_STATE();
    case 45:
      ACCEPT_TOKEN(aux_sym_tag_token1);
      if (('$' <= lookahead && lookahead <= '&') ||
          lookahead == '*' ||
          lookahead == '+' ||
          ('-' <= lookahead && lookahead <= ':') ||
          lookahead == '=' ||
          ('@' <= lookahead && lookahead <= 'Z') ||
          lookahead == '^' ||
          lookahead == '_' ||
          ('a' <= lookahead && lookahead <= 'z') ||
          lookahead == '~') ADVANCE(45);
      END_STATE();
    case 46:
      ACCEPT_TOKEN(anon_sym_DOT);
      END_STATE();
    case 47:
      ACCEPT_TOKEN(anon_sym_DOT);
      if (lookahead == '[') ADVANCE(69);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 48:
      ACCEPT_TOKEN(anon_sym_LPAREN);
      END_STATE();
    case 49:
      ACCEPT_TOKEN(anon_sym_RPAREN);
      END_STATE();
    case 50:
      ACCEPT_TOKEN(anon_sym_DQUOTE);
      END_STATE();
    case 51:
      ACCEPT_TOKEN(aux_sym_string_token1);
      if (lookahead != 0 &&
          lookahead != '"' &&
          lookahead != '$' &&
          lookahead != '\\') ADVANCE(51);
      END_STATE();
    case 52:
      ACCEPT_TOKEN(anon_sym_SQUOTE);
      END_STATE();
    case 53:
      ACCEPT_TOKEN(aux_sym_string_token2);
      if (lookahead != 0 &&
          lookahead != '\'' &&
          lookahead != '\\') ADVANCE(53);
      END_STATE();
    case 54:
      ACCEPT_TOKEN(sym_string_escape);
      END_STATE();
    case 55:
      ACCEPT_TOKEN(anon_sym_PIPE);
      END_STATE();
    case 56:
      ACCEPT_TOKEN(aux_sym_block_literal_token1);
      END_STATE();
    case 57:
      ACCEPT_TOKEN(aux_sym_block_literal_token1);
      if (lookahead == '-') ADVANCE(73);
      if (lookahead == '0') ADVANCE(74);
      if (('1' <= lookahead && lookahead <= '9')) ADVANCE(75);
      if (sym_literal_character_set_2(lookahead)) ADVANCE(94);
      END_STATE();
    case 58:
      ACCEPT_TOKEN(aux_sym_block_literal_token1);
      if (lookahead == '.') ADVANCE(17);
      if (lookahead == 'E' ||
          lookahead == 'e') ADVANCE(14);
      if (lookahead == 'O' ||
          lookahead == 'o') ADVANCE(16);
      if (lookahead == 'X' ||
          lookahead == 'x') ADVANCE(20);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(98);
      END_STATE();
    case 59:
      ACCEPT_TOKEN(aux_sym_block_literal_token1);
      if (lookahead == '.') ADVANCE(17);
      if (lookahead == 'E' ||
          lookahead == 'e') ADVANCE(14);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(98);
      END_STATE();
    case 60:
      ACCEPT_TOKEN(aux_sym_block_literal_token1);
      if (lookahead == '[') ADVANCE(67);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 61:
      ACCEPT_TOKEN(aux_sym_block_literal_token1);
      if (lookahead == '[') ADVANCE(69);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 62:
      ACCEPT_TOKEN(aux_sym_block_literal_token1);
      if (lookahead == 'a') ADVANCE(82);
      if (sym_literal_character_set_3(lookahead)) ADVANCE(94);
      END_STATE();
    case 63:
      ACCEPT_TOKEN(aux_sym_block_literal_token1);
      if (lookahead == 'r') ADVANCE(87);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 64:
      ACCEPT_TOKEN(aux_sym_block_literal_token1);
      if (lookahead == 'u') ADVANCE(84);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 65:
      ACCEPT_TOKEN(aux_sym_block_literal_token1);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 66:
      ACCEPT_TOKEN(anon_sym_DOLLAR_LBRACK);
      END_STATE();
    case 67:
      ACCEPT_TOKEN(anon_sym_DOLLAR_LBRACK);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 68:
      ACCEPT_TOKEN(aux_sym_interpolation_token1);
      if (lookahead != 0 &&
          lookahead != ']') ADVANCE(68);
      END_STATE();
    case 69:
      ACCEPT_TOKEN(anon_sym_DOT_LBRACK);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 70:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == '+') ADVANCE(18);
      if (lookahead == '-') ADVANCE(92);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(92);
      if (sym_literal_character_set_2(lookahead)) ADVANCE(94);
      END_STATE();
    case 71:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == '+') ADVANCE(19);
      if (lookahead == '-') ADVANCE(92);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(92);
      if (sym_literal_character_set_2(lookahead)) ADVANCE(94);
      END_STATE();
    case 72:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == '-') ADVANCE(73);
      if (lookahead == '0') ADVANCE(74);
      if (('1' <= lookahead && lookahead <= '9')) ADVANCE(75);
      if (sym_literal_character_set_2(lookahead)) ADVANCE(94);
      END_STATE();
    case 73:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == '-') ADVANCE(33);
      if (sym_literal_character_set_2(lookahead)) ADVANCE(94);
      END_STATE();
    case 74:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == '.') ADVANCE(91);
      if (lookahead == 'E' ||
          lookahead == 'e') ADVANCE(70);
      if (lookahead == 'O' ||
          lookahead == 'o') ADVANCE(90);
      if (lookahead == 'X' ||
          lookahead == 'x') ADVANCE(93);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(75);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 75:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == '.') ADVANCE(91);
      if (lookahead == 'E' ||
          lookahead == 'e') ADVANCE(70);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(75);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 76:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == '0') ADVANCE(74);
      if (('1' <= lookahead && lookahead <= '9')) ADVANCE(75);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 77:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == '[') ADVANCE(67);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 78:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == '[') ADVANCE(69);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 79:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == 'a') ADVANCE(82);
      if (sym_literal_character_set_3(lookahead)) ADVANCE(94);
      END_STATE();
    case 80:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == 'e') ADVANCE(102);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 81:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == 'e') ADVANCE(103);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 82:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == 'l') ADVANCE(86);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 83:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == 'l') ADVANCE(104);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 84:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == 'l') ADVANCE(83);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 85:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == 'r') ADVANCE(87);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 86:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == 's') ADVANCE(81);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 87:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == 'u') ADVANCE(80);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 88:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == 'u') ADVANCE(84);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 89:
      ACCEPT_TOKEN(sym_literal);
      if (lookahead == 'E' ||
          lookahead == 'e') ADVANCE(71);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(89);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 90:
      ACCEPT_TOKEN(sym_literal);
      if (('0' <= lookahead && lookahead <= '7')) ADVANCE(90);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 91:
      ACCEPT_TOKEN(sym_literal);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(89);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 92:
      ACCEPT_TOKEN(sym_literal);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(92);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 93:
      ACCEPT_TOKEN(sym_literal);
      if (('0' <= lookahead && lookahead <= '9') ||
          ('A' <= lookahead && lookahead <= 'F') ||
          ('a' <= lookahead && lookahead <= 'f')) ADVANCE(93);
      if (sym_literal_character_set_4(lookahead)) ADVANCE(94);
      END_STATE();
    case 94:
      ACCEPT_TOKEN(sym_literal);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 95:
      ACCEPT_TOKEN(aux_sym_number_token1);
      if (lookahead == 'E' ||
          lookahead == 'e') ADVANCE(15);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(95);
      END_STATE();
    case 96:
      ACCEPT_TOKEN(aux_sym_number_token1);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(96);
      END_STATE();
    case 97:
      ACCEPT_TOKEN(aux_sym_number_token2);
      if (lookahead == '.') ADVANCE(17);
      if (lookahead == 'E' ||
          lookahead == 'e') ADVANCE(14);
      if (lookahead == 'O' ||
          lookahead == 'o') ADVANCE(16);
      if (lookahead == 'X' ||
          lookahead == 'x') ADVANCE(20);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(98);
      END_STATE();
    case 98:
      ACCEPT_TOKEN(aux_sym_number_token2);
      if (lookahead == '.') ADVANCE(17);
      if (lookahead == 'E' ||
          lookahead == 'e') ADVANCE(14);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(98);
      END_STATE();
    case 99:
      ACCEPT_TOKEN(aux_sym_number_token2);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(99);
      END_STATE();
    case 100:
      ACCEPT_TOKEN(aux_sym_number_token3);
      if (('0' <= lookahead && lookahead <= '9') ||
          ('A' <= lookahead && lookahead <= 'F') ||
          ('a' <= lookahead && lookahead <= 'f')) ADVANCE(100);
      END_STATE();
    case 101:
      ACCEPT_TOKEN(aux_sym_number_token4);
      if (('0' <= lookahead && lookahead <= '7')) ADVANCE(101);
      END_STATE();
    case 102:
      ACCEPT_TOKEN(anon_sym_true);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 103:
      ACCEPT_TOKEN(anon_sym_false);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    case 104:
      ACCEPT_TOKEN(sym_null);
      if (sym_literal_character_set_1(lookahead)) ADVANCE(94);
      END_STATE();
    default:
      return false;
  }
}

static const TSLexMode ts_lex_modes[STATE_COUNT] = {
  [0] = {.lex_state = 0},
  [1] = {.lex_state = 27},
  [2] = {.lex_state = 27},
  [3] = {.lex_state = 27},
  [4] = {.lex_state = 27},
  [5] = {.lex_state = 5},
  [6] = {.lex_state = 5},
  [7] = {.lex_state = 5},
  [8] = {.lex_state = 5},
  [9] = {.lex_state = 5},
  [10] = {.lex_state = 5},
  [11] = {.lex_state = 5},
  [12] = {.lex_state = 5},
  [13] = {.lex_state = 5},
  [14] = {.lex_state = 5},
  [15] = {.lex_state = 5},
  [16] = {.lex_state = 27},
  [17] = {.lex_state = 4},
  [18] = {.lex_state = 4},
  [19] = {.lex_state = 4},
  [20] = {.lex_state = 27},
  [21] = {.lex_state = 27},
  [22] = {.lex_state = 27},
  [23] = {.lex_state = 27},
  [24] = {.lex_state = 27},
  [25] = {.lex_state = 27},
  [26] = {.lex_state = 27},
  [27] = {.lex_state = 27},
  [28] = {.lex_state = 4},
  [29] = {.lex_state = 27},
  [30] = {.lex_state = 27},
  [31] = {.lex_state = 26},
  [32] = {.lex_state = 26},
  [33] = {.lex_state = 26},
  [34] = {.lex_state = 25},
  [35] = {.lex_state = 25},
  [36] = {.lex_state = 5},
  [37] = {.lex_state = 5},
  [38] = {.lex_state = 5},
  [39] = {.lex_state = 26},
  [40] = {.lex_state = 25},
  [41] = {.lex_state = 27},
  [42] = {.lex_state = 27},
  [43] = {.lex_state = 27},
  [44] = {.lex_state = 27},
  [45] = {.lex_state = 6},
  [46] = {.lex_state = 6},
  [47] = {.lex_state = 7},
  [48] = {.lex_state = 7},
  [49] = {.lex_state = 7},
  [50] = {.lex_state = 2},
  [51] = {.lex_state = 2},
  [52] = {.lex_state = 1},
  [53] = {.lex_state = 10},
  [54] = {.lex_state = 1},
  [55] = {.lex_state = 10},
  [56] = {.lex_state = 1},
  [57] = {.lex_state = 10},
  [58] = {.lex_state = 3},
  [59] = {.lex_state = 3},
  [60] = {.lex_state = 3},
  [61] = {.lex_state = 8},
  [62] = {.lex_state = 7},
  [63] = {.lex_state = 2},
  [64] = {.lex_state = 8},
  [65] = {.lex_state = 8},
  [66] = {.lex_state = 0},
  [67] = {.lex_state = 0},
  [68] = {.lex_state = 0},
  [69] = {.lex_state = 0},
  [70] = {.lex_state = 0},
  [71] = {.lex_state = 0},
  [72] = {.lex_state = 0},
  [73] = {.lex_state = 8},
  [74] = {.lex_state = 0},
  [75] = {.lex_state = 0},
  [76] = {.lex_state = 0},
  [77] = {.lex_state = 0},
  [78] = {.lex_state = 0},
  [79] = {.lex_state = 27},
  [80] = {.lex_state = 0},
  [81] = {.lex_state = 27},
  [82] = {.lex_state = 11},
  [83] = {.lex_state = 0},
  [84] = {.lex_state = 11},
  [85] = {.lex_state = 9},
  [86] = {.lex_state = 0},
  [87] = {.lex_state = 0},
  [88] = {.lex_state = 0},
  [89] = {.lex_state = 11},
  [90] = {.lex_state = 0},
  [91] = {.lex_state = 0},
  [92] = {.lex_state = 0},
  [93] = {.lex_state = 9},
  [94] = {.lex_state = 9},
  [95] = {.lex_state = 0},
  [96] = {.lex_state = 9},
  [97] = {.lex_state = 27},
  [98] = {.lex_state = 0},
  [99] = {.lex_state = 9},
  [100] = {.lex_state = 9},
  [101] = {.lex_state = 0},
};

static const uint16_t ts_parse_table[LARGE_STATE_COUNT][SYMBOL_COUNT] = {
  [0] = {
    [ts_builtin_sym_end] = ACTIONS(1),
    [sym_comment] = ACTIONS(3),
    [anon_sym_DASH_DASH_DASH] = ACTIONS(1),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(1),
    [anon_sym_RBRACE] = ACTIONS(1),
    [anon_sym_COMMA] = ACTIONS(1),
    [anon_sym_COLON] = ACTIONS(1),
    [anon_sym_LBRACK] = ACTIONS(1),
    [anon_sym_RBRACK] = ACTIONS(1),
    [anon_sym_BANG] = ACTIONS(1),
    [anon_sym_DOT] = ACTIONS(1),
    [anon_sym_LPAREN] = ACTIONS(1),
    [anon_sym_RPAREN] = ACTIONS(1),
    [anon_sym_DQUOTE] = ACTIONS(1),
    [anon_sym_SQUOTE] = ACTIONS(1),
    [anon_sym_PIPE] = ACTIONS(1),
    [aux_sym_block_literal_token1] = ACTIONS(1),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(1),
    [anon_sym_DOT_LBRACK] = ACTIONS(1),
    [sym_literal] = ACTIONS(1),
    [aux_sym_number_token1] = ACTIONS(1),
    [aux_sym_number_token2] = ACTIONS(1),
    [aux_sym_number_token3] = ACTIONS(1),
    [aux_sym_number_token4] = ACTIONS(1),
    [anon_sym_true] = ACTIONS(1),
    [anon_sym_false] = ACTIONS(1),
    [sym_null] = ACTIONS(1),
  },
  [1] = {
    [sym_source_file] = STATE(91),
    [sym__top_level] = STATE(2),
    [sym_document_separator] = STATE(2),
    [sym__value] = STATE(2),
    [sym_bracketed_map] = STATE(2),
    [sym_bracketed_array] = STATE(2),
    [sym_tag] = STATE(4),
    [sym_tagged_value] = STATE(2),
    [sym_string] = STATE(2),
    [sym_block_literal] = STATE(2),
    [sym_interpolation] = STATE(2),
    [sym_node_replacement] = STATE(2),
    [sym_number] = STATE(2),
    [sym_boolean] = STATE(2),
    [aux_sym_source_file_repeat1] = STATE(2),
    [ts_builtin_sym_end] = ACTIONS(5),
    [sym_comment] = ACTIONS(3),
    [anon_sym_DASH_DASH_DASH] = ACTIONS(7),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(9),
    [anon_sym_LBRACK] = ACTIONS(11),
    [anon_sym_BANG] = ACTIONS(13),
    [anon_sym_DQUOTE] = ACTIONS(15),
    [anon_sym_SQUOTE] = ACTIONS(17),
    [anon_sym_PIPE] = ACTIONS(19),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(21),
    [anon_sym_DOT_LBRACK] = ACTIONS(23),
    [sym_literal] = ACTIONS(25),
    [aux_sym_number_token1] = ACTIONS(27),
    [aux_sym_number_token2] = ACTIONS(27),
    [aux_sym_number_token3] = ACTIONS(27),
    [aux_sym_number_token4] = ACTIONS(27),
    [anon_sym_true] = ACTIONS(29),
    [anon_sym_false] = ACTIONS(29),
    [sym_null] = ACTIONS(25),
  },
  [2] = {
    [sym__top_level] = STATE(3),
    [sym_document_separator] = STATE(3),
    [sym__value] = STATE(3),
    [sym_bracketed_map] = STATE(3),
    [sym_bracketed_array] = STATE(3),
    [sym_tag] = STATE(4),
    [sym_tagged_value] = STATE(3),
    [sym_string] = STATE(3),
    [sym_block_literal] = STATE(3),
    [sym_interpolation] = STATE(3),
    [sym_node_replacement] = STATE(3),
    [sym_number] = STATE(3),
    [sym_boolean] = STATE(3),
    [aux_sym_source_file_repeat1] = STATE(3),
    [ts_builtin_sym_end] = ACTIONS(31),
    [sym_comment] = ACTIONS(3),
    [anon_sym_DASH_DASH_DASH] = ACTIONS(7),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(9),
    [anon_sym_LBRACK] = ACTIONS(11),
    [anon_sym_BANG] = ACTIONS(13),
    [anon_sym_DQUOTE] = ACTIONS(15),
    [anon_sym_SQUOTE] = ACTIONS(17),
    [anon_sym_PIPE] = ACTIONS(19),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(21),
    [anon_sym_DOT_LBRACK] = ACTIONS(23),
    [sym_literal] = ACTIONS(33),
    [aux_sym_number_token1] = ACTIONS(27),
    [aux_sym_number_token2] = ACTIONS(27),
    [aux_sym_number_token3] = ACTIONS(27),
    [aux_sym_number_token4] = ACTIONS(27),
    [anon_sym_true] = ACTIONS(29),
    [anon_sym_false] = ACTIONS(29),
    [sym_null] = ACTIONS(33),
  },
  [3] = {
    [sym__top_level] = STATE(3),
    [sym_document_separator] = STATE(3),
    [sym__value] = STATE(3),
    [sym_bracketed_map] = STATE(3),
    [sym_bracketed_array] = STATE(3),
    [sym_tag] = STATE(4),
    [sym_tagged_value] = STATE(3),
    [sym_string] = STATE(3),
    [sym_block_literal] = STATE(3),
    [sym_interpolation] = STATE(3),
    [sym_node_replacement] = STATE(3),
    [sym_number] = STATE(3),
    [sym_boolean] = STATE(3),
    [aux_sym_source_file_repeat1] = STATE(3),
    [ts_builtin_sym_end] = ACTIONS(35),
    [sym_comment] = ACTIONS(3),
    [anon_sym_DASH_DASH_DASH] = ACTIONS(37),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(40),
    [anon_sym_LBRACK] = ACTIONS(43),
    [anon_sym_BANG] = ACTIONS(46),
    [anon_sym_DQUOTE] = ACTIONS(49),
    [anon_sym_SQUOTE] = ACTIONS(52),
    [anon_sym_PIPE] = ACTIONS(55),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(58),
    [anon_sym_DOT_LBRACK] = ACTIONS(61),
    [sym_literal] = ACTIONS(64),
    [aux_sym_number_token1] = ACTIONS(67),
    [aux_sym_number_token2] = ACTIONS(67),
    [aux_sym_number_token3] = ACTIONS(67),
    [aux_sym_number_token4] = ACTIONS(67),
    [anon_sym_true] = ACTIONS(70),
    [anon_sym_false] = ACTIONS(70),
    [sym_null] = ACTIONS(64),
  },
  [4] = {
    [sym__value] = STATE(25),
    [sym_bracketed_map] = STATE(25),
    [sym_bracketed_array] = STATE(25),
    [sym_tag] = STATE(4),
    [sym_tagged_value] = STATE(25),
    [sym_string] = STATE(25),
    [sym_block_literal] = STATE(25),
    [sym_interpolation] = STATE(25),
    [sym_node_replacement] = STATE(25),
    [sym_number] = STATE(25),
    [sym_boolean] = STATE(25),
    [ts_builtin_sym_end] = ACTIONS(73),
    [sym_comment] = ACTIONS(3),
    [anon_sym_DASH_DASH_DASH] = ACTIONS(75),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(73),
    [anon_sym_LBRACK] = ACTIONS(73),
    [anon_sym_BANG] = ACTIONS(73),
    [anon_sym_DQUOTE] = ACTIONS(73),
    [anon_sym_SQUOTE] = ACTIONS(73),
    [anon_sym_PIPE] = ACTIONS(73),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(75),
    [anon_sym_DOT_LBRACK] = ACTIONS(75),
    [sym_literal] = ACTIONS(75),
    [aux_sym_number_token1] = ACTIONS(75),
    [aux_sym_number_token2] = ACTIONS(75),
    [aux_sym_number_token3] = ACTIONS(75),
    [aux_sym_number_token4] = ACTIONS(75),
    [anon_sym_true] = ACTIONS(75),
    [anon_sym_false] = ACTIONS(75),
    [sym_null] = ACTIONS(75),
  },
  [5] = {
    [sym__value] = STATE(25),
    [sym_bracketed_map] = STATE(25),
    [sym_bracketed_array] = STATE(25),
    [sym_tag] = STATE(5),
    [sym_tagged_value] = STATE(25),
    [sym_string] = STATE(25),
    [sym_block_literal] = STATE(25),
    [sym_interpolation] = STATE(25),
    [sym_node_replacement] = STATE(25),
    [sym_number] = STATE(25),
    [sym_boolean] = STATE(25),
    [sym_comment] = ACTIONS(3),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(9),
    [anon_sym_COMMA] = ACTIONS(73),
    [anon_sym_LBRACK] = ACTIONS(11),
    [anon_sym_BANG] = ACTIONS(77),
    [anon_sym_RPAREN] = ACTIONS(73),
    [anon_sym_DQUOTE] = ACTIONS(15),
    [anon_sym_SQUOTE] = ACTIONS(17),
    [anon_sym_PIPE] = ACTIONS(79),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(21),
    [anon_sym_DOT_LBRACK] = ACTIONS(23),
    [sym_literal] = ACTIONS(81),
    [aux_sym_number_token1] = ACTIONS(27),
    [aux_sym_number_token2] = ACTIONS(27),
    [aux_sym_number_token3] = ACTIONS(27),
    [aux_sym_number_token4] = ACTIONS(27),
    [anon_sym_true] = ACTIONS(29),
    [anon_sym_false] = ACTIONS(29),
    [sym_null] = ACTIONS(81),
  },
  [6] = {
    [sym__value] = STATE(71),
    [sym_bracketed_map] = STATE(71),
    [sym_bracketed_array] = STATE(71),
    [sym_tag] = STATE(11),
    [sym_tagged_value] = STATE(71),
    [sym_string] = STATE(71),
    [sym_block_literal] = STATE(71),
    [sym_interpolation] = STATE(71),
    [sym_node_replacement] = STATE(71),
    [sym_number] = STATE(71),
    [sym_boolean] = STATE(71),
    [sym_comment] = ACTIONS(3),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(9),
    [anon_sym_COMMA] = ACTIONS(83),
    [anon_sym_LBRACK] = ACTIONS(11),
    [anon_sym_RBRACK] = ACTIONS(83),
    [anon_sym_BANG] = ACTIONS(77),
    [anon_sym_DQUOTE] = ACTIONS(15),
    [anon_sym_SQUOTE] = ACTIONS(17),
    [anon_sym_PIPE] = ACTIONS(85),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(21),
    [anon_sym_DOT_LBRACK] = ACTIONS(23),
    [sym_literal] = ACTIONS(87),
    [aux_sym_number_token1] = ACTIONS(27),
    [aux_sym_number_token2] = ACTIONS(27),
    [aux_sym_number_token3] = ACTIONS(27),
    [aux_sym_number_token4] = ACTIONS(27),
    [anon_sym_true] = ACTIONS(29),
    [anon_sym_false] = ACTIONS(29),
    [sym_null] = ACTIONS(87),
  },
  [7] = {
    [sym__value] = STATE(74),
    [sym_bracketed_map] = STATE(74),
    [sym_bracketed_array] = STATE(74),
    [sym_tag] = STATE(5),
    [sym__tag_arguments] = STATE(86),
    [sym_tagged_value] = STATE(74),
    [sym_string] = STATE(74),
    [sym_block_literal] = STATE(74),
    [sym_interpolation] = STATE(74),
    [sym_node_replacement] = STATE(74),
    [sym_number] = STATE(74),
    [sym_boolean] = STATE(74),
    [sym_comment] = ACTIONS(3),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(9),
    [anon_sym_LBRACK] = ACTIONS(11),
    [anon_sym_BANG] = ACTIONS(77),
    [anon_sym_RPAREN] = ACTIONS(89),
    [anon_sym_DQUOTE] = ACTIONS(15),
    [anon_sym_SQUOTE] = ACTIONS(17),
    [anon_sym_PIPE] = ACTIONS(79),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(21),
    [anon_sym_DOT_LBRACK] = ACTIONS(23),
    [sym_literal] = ACTIONS(91),
    [aux_sym_number_token1] = ACTIONS(27),
    [aux_sym_number_token2] = ACTIONS(27),
    [aux_sym_number_token3] = ACTIONS(27),
    [aux_sym_number_token4] = ACTIONS(27),
    [anon_sym_true] = ACTIONS(29),
    [anon_sym_false] = ACTIONS(29),
    [sym_null] = ACTIONS(91),
  },
  [8] = {
    [sym__value] = STATE(77),
    [sym_bracketed_map] = STATE(77),
    [sym_bracketed_array] = STATE(77),
    [sym_tag] = STATE(9),
    [sym_tagged_value] = STATE(77),
    [sym_string] = STATE(77),
    [sym_block_literal] = STATE(77),
    [sym_interpolation] = STATE(77),
    [sym_node_replacement] = STATE(77),
    [sym_number] = STATE(77),
    [sym_boolean] = STATE(77),
    [sym_comment] = ACTIONS(3),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(9),
    [anon_sym_RBRACE] = ACTIONS(93),
    [anon_sym_COMMA] = ACTIONS(93),
    [anon_sym_LBRACK] = ACTIONS(11),
    [anon_sym_BANG] = ACTIONS(77),
    [anon_sym_DQUOTE] = ACTIONS(15),
    [anon_sym_SQUOTE] = ACTIONS(17),
    [anon_sym_PIPE] = ACTIONS(95),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(21),
    [anon_sym_DOT_LBRACK] = ACTIONS(23),
    [sym_literal] = ACTIONS(97),
    [aux_sym_number_token1] = ACTIONS(27),
    [aux_sym_number_token2] = ACTIONS(27),
    [aux_sym_number_token3] = ACTIONS(27),
    [aux_sym_number_token4] = ACTIONS(27),
    [anon_sym_true] = ACTIONS(29),
    [anon_sym_false] = ACTIONS(29),
    [sym_null] = ACTIONS(97),
  },
  [9] = {
    [sym__value] = STATE(25),
    [sym_bracketed_map] = STATE(25),
    [sym_bracketed_array] = STATE(25),
    [sym_tag] = STATE(9),
    [sym_tagged_value] = STATE(25),
    [sym_string] = STATE(25),
    [sym_block_literal] = STATE(25),
    [sym_interpolation] = STATE(25),
    [sym_node_replacement] = STATE(25),
    [sym_number] = STATE(25),
    [sym_boolean] = STATE(25),
    [sym_comment] = ACTIONS(3),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(9),
    [anon_sym_RBRACE] = ACTIONS(73),
    [anon_sym_COMMA] = ACTIONS(73),
    [anon_sym_LBRACK] = ACTIONS(11),
    [anon_sym_BANG] = ACTIONS(77),
    [anon_sym_DQUOTE] = ACTIONS(15),
    [anon_sym_SQUOTE] = ACTIONS(17),
    [anon_sym_PIPE] = ACTIONS(95),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(21),
    [anon_sym_DOT_LBRACK] = ACTIONS(23),
    [sym_literal] = ACTIONS(81),
    [aux_sym_number_token1] = ACTIONS(27),
    [aux_sym_number_token2] = ACTIONS(27),
    [aux_sym_number_token3] = ACTIONS(27),
    [aux_sym_number_token4] = ACTIONS(27),
    [anon_sym_true] = ACTIONS(29),
    [anon_sym_false] = ACTIONS(29),
    [sym_null] = ACTIONS(81),
  },
  [10] = {
    [sym__value] = STATE(74),
    [sym_bracketed_map] = STATE(74),
    [sym_bracketed_array] = STATE(74),
    [sym_tag] = STATE(5),
    [sym__tag_arguments] = STATE(98),
    [sym_tagged_value] = STATE(74),
    [sym_string] = STATE(74),
    [sym_block_literal] = STATE(74),
    [sym_interpolation] = STATE(74),
    [sym_node_replacement] = STATE(74),
    [sym_number] = STATE(74),
    [sym_boolean] = STATE(74),
    [sym_comment] = ACTIONS(3),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(9),
    [anon_sym_LBRACK] = ACTIONS(11),
    [anon_sym_BANG] = ACTIONS(77),
    [anon_sym_RPAREN] = ACTIONS(99),
    [anon_sym_DQUOTE] = ACTIONS(15),
    [anon_sym_SQUOTE] = ACTIONS(17),
    [anon_sym_PIPE] = ACTIONS(79),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(21),
    [anon_sym_DOT_LBRACK] = ACTIONS(23),
    [sym_literal] = ACTIONS(91),
    [aux_sym_number_token1] = ACTIONS(27),
    [aux_sym_number_token2] = ACTIONS(27),
    [aux_sym_number_token3] = ACTIONS(27),
    [aux_sym_number_token4] = ACTIONS(27),
    [anon_sym_true] = ACTIONS(29),
    [anon_sym_false] = ACTIONS(29),
    [sym_null] = ACTIONS(91),
  },
  [11] = {
    [sym__value] = STATE(25),
    [sym_bracketed_map] = STATE(25),
    [sym_bracketed_array] = STATE(25),
    [sym_tag] = STATE(11),
    [sym_tagged_value] = STATE(25),
    [sym_string] = STATE(25),
    [sym_block_literal] = STATE(25),
    [sym_interpolation] = STATE(25),
    [sym_node_replacement] = STATE(25),
    [sym_number] = STATE(25),
    [sym_boolean] = STATE(25),
    [sym_comment] = ACTIONS(3),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(9),
    [anon_sym_COMMA] = ACTIONS(73),
    [anon_sym_LBRACK] = ACTIONS(11),
    [anon_sym_RBRACK] = ACTIONS(73),
    [anon_sym_BANG] = ACTIONS(77),
    [anon_sym_DQUOTE] = ACTIONS(15),
    [anon_sym_SQUOTE] = ACTIONS(17),
    [anon_sym_PIPE] = ACTIONS(85),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(21),
    [anon_sym_DOT_LBRACK] = ACTIONS(23),
    [sym_literal] = ACTIONS(81),
    [aux_sym_number_token1] = ACTIONS(27),
    [aux_sym_number_token2] = ACTIONS(27),
    [aux_sym_number_token3] = ACTIONS(27),
    [aux_sym_number_token4] = ACTIONS(27),
    [anon_sym_true] = ACTIONS(29),
    [anon_sym_false] = ACTIONS(29),
    [sym_null] = ACTIONS(81),
  },
  [12] = {
    [sym__value] = STATE(71),
    [sym_bracketed_map] = STATE(71),
    [sym_bracketed_array] = STATE(71),
    [sym_tag] = STATE(5),
    [sym_tagged_value] = STATE(71),
    [sym_string] = STATE(71),
    [sym_block_literal] = STATE(71),
    [sym_interpolation] = STATE(71),
    [sym_node_replacement] = STATE(71),
    [sym_number] = STATE(71),
    [sym_boolean] = STATE(71),
    [sym_comment] = ACTIONS(3),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(9),
    [anon_sym_COMMA] = ACTIONS(83),
    [anon_sym_LBRACK] = ACTIONS(11),
    [anon_sym_BANG] = ACTIONS(77),
    [anon_sym_RPAREN] = ACTIONS(83),
    [anon_sym_DQUOTE] = ACTIONS(15),
    [anon_sym_SQUOTE] = ACTIONS(17),
    [anon_sym_PIPE] = ACTIONS(79),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(21),
    [anon_sym_DOT_LBRACK] = ACTIONS(23),
    [sym_literal] = ACTIONS(87),
    [aux_sym_number_token1] = ACTIONS(27),
    [aux_sym_number_token2] = ACTIONS(27),
    [aux_sym_number_token3] = ACTIONS(27),
    [aux_sym_number_token4] = ACTIONS(27),
    [anon_sym_true] = ACTIONS(29),
    [anon_sym_false] = ACTIONS(29),
    [sym_null] = ACTIONS(87),
  },
  [13] = {
    [sym__value] = STATE(74),
    [sym_bracketed_map] = STATE(74),
    [sym_bracketed_array] = STATE(74),
    [sym_tag] = STATE(5),
    [sym__tag_arguments] = STATE(83),
    [sym_tagged_value] = STATE(74),
    [sym_string] = STATE(74),
    [sym_block_literal] = STATE(74),
    [sym_interpolation] = STATE(74),
    [sym_node_replacement] = STATE(74),
    [sym_number] = STATE(74),
    [sym_boolean] = STATE(74),
    [sym_comment] = ACTIONS(3),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(9),
    [anon_sym_LBRACK] = ACTIONS(11),
    [anon_sym_BANG] = ACTIONS(77),
    [anon_sym_RPAREN] = ACTIONS(101),
    [anon_sym_DQUOTE] = ACTIONS(15),
    [anon_sym_SQUOTE] = ACTIONS(17),
    [anon_sym_PIPE] = ACTIONS(79),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(21),
    [anon_sym_DOT_LBRACK] = ACTIONS(23),
    [sym_literal] = ACTIONS(91),
    [aux_sym_number_token1] = ACTIONS(27),
    [aux_sym_number_token2] = ACTIONS(27),
    [aux_sym_number_token3] = ACTIONS(27),
    [aux_sym_number_token4] = ACTIONS(27),
    [anon_sym_true] = ACTIONS(29),
    [anon_sym_false] = ACTIONS(29),
    [sym_null] = ACTIONS(91),
  },
  [14] = {
    [sym__value] = STATE(68),
    [sym_bracketed_map] = STATE(68),
    [sym_bracketed_array] = STATE(68),
    [sym__array_content] = STATE(92),
    [sym_tag] = STATE(11),
    [sym_tagged_value] = STATE(68),
    [sym_string] = STATE(68),
    [sym_block_literal] = STATE(68),
    [sym_interpolation] = STATE(68),
    [sym_node_replacement] = STATE(68),
    [sym_number] = STATE(68),
    [sym_boolean] = STATE(68),
    [sym_comment] = ACTIONS(3),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(9),
    [anon_sym_LBRACK] = ACTIONS(11),
    [anon_sym_RBRACK] = ACTIONS(103),
    [anon_sym_BANG] = ACTIONS(77),
    [anon_sym_DQUOTE] = ACTIONS(15),
    [anon_sym_SQUOTE] = ACTIONS(17),
    [anon_sym_PIPE] = ACTIONS(85),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(21),
    [anon_sym_DOT_LBRACK] = ACTIONS(23),
    [sym_literal] = ACTIONS(105),
    [aux_sym_number_token1] = ACTIONS(27),
    [aux_sym_number_token2] = ACTIONS(27),
    [aux_sym_number_token3] = ACTIONS(27),
    [aux_sym_number_token4] = ACTIONS(27),
    [anon_sym_true] = ACTIONS(29),
    [anon_sym_false] = ACTIONS(29),
    [sym_null] = ACTIONS(105),
  },
  [15] = {
    [sym__value] = STATE(74),
    [sym_bracketed_map] = STATE(74),
    [sym_bracketed_array] = STATE(74),
    [sym_tag] = STATE(5),
    [sym__tag_arguments] = STATE(80),
    [sym_tagged_value] = STATE(74),
    [sym_string] = STATE(74),
    [sym_block_literal] = STATE(74),
    [sym_interpolation] = STATE(74),
    [sym_node_replacement] = STATE(74),
    [sym_number] = STATE(74),
    [sym_boolean] = STATE(74),
    [sym_comment] = ACTIONS(3),
    [aux_sym_document_separator_token1] = ACTIONS(3),
    [anon_sym_LBRACE] = ACTIONS(9),
    [anon_sym_LBRACK] = ACTIONS(11),
    [anon_sym_BANG] = ACTIONS(77),
    [anon_sym_RPAREN] = ACTIONS(107),
    [anon_sym_DQUOTE] = ACTIONS(15),
    [anon_sym_SQUOTE] = ACTIONS(17),
    [anon_sym_PIPE] = ACTIONS(79),
    [anon_sym_DOLLAR_LBRACK] = ACTIONS(21),
    [anon_sym_DOT_LBRACK] = ACTIONS(23),
    [sym_literal] = ACTIONS(91),
    [aux_sym_number_token1] = ACTIONS(27),
    [aux_sym_number_token2] = ACTIONS(27),
    [aux_sym_number_token3] = ACTIONS(27),
    [aux_sym_number_token4] = ACTIONS(27),
    [anon_sym_true] = ACTIONS(29),
    [anon_sym_false] = ACTIONS(29),
    [sym_null] = ACTIONS(91),
  },
};

static const uint16_t ts_small_parse_table[] = {
  [0] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(111), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
    ACTIONS(109), 12,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_COLON,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
  [32] = 5,
    ACTIONS(115), 1,
      anon_sym_DOT,
    STATE(17), 1,
      aux_sym_tag_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(118), 10,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
    ACTIONS(113), 11,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_LPAREN,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
  [68] = 6,
    ACTIONS(122), 1,
      anon_sym_DOT,
    ACTIONS(124), 1,
      anon_sym_LPAREN,
    STATE(17), 1,
      aux_sym_tag_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(120), 10,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(126), 10,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [106] = 6,
    ACTIONS(122), 1,
      anon_sym_DOT,
    ACTIONS(130), 1,
      anon_sym_LPAREN,
    STATE(18), 1,
      aux_sym_tag_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(128), 10,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(132), 10,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [144] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(136), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
    ACTIONS(134), 12,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_COLON,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
  [176] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(140), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
    ACTIONS(138), 12,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_COLON,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
  [208] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(144), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
    ACTIONS(142), 12,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_COLON,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
  [240] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(146), 11,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(148), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [271] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(150), 11,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(152), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [302] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(154), 11,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(156), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [333] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(158), 11,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(160), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [364] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(162), 11,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(164), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [395] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(113), 11,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_LPAREN,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(118), 11,
      anon_sym_DOT,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [426] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(166), 11,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(168), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [457] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(170), 11,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(172), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [488] = 6,
    ACTIONS(174), 1,
      anon_sym_DOT,
    ACTIONS(176), 1,
      anon_sym_LPAREN,
    STATE(33), 1,
      aux_sym_tag_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(128), 7,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_LBRACK,
      anon_sym_BANG,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(132), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [524] = 5,
    ACTIONS(178), 1,
      anon_sym_DOT,
    STATE(32), 1,
      aux_sym_tag_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(113), 8,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_LBRACK,
      anon_sym_BANG,
      anon_sym_LPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(118), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [558] = 6,
    ACTIONS(174), 1,
      anon_sym_DOT,
    ACTIONS(181), 1,
      anon_sym_LPAREN,
    STATE(32), 1,
      aux_sym_tag_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(120), 7,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_LBRACK,
      anon_sym_BANG,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(126), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [594] = 5,
    ACTIONS(187), 1,
      aux_sym_block_literal_token1,
    STATE(35), 1,
      aux_sym_block_literal_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(183), 7,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_LBRACK,
      anon_sym_BANG,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(185), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [627] = 5,
    ACTIONS(193), 1,
      aux_sym_block_literal_token1,
    STATE(40), 1,
      aux_sym_block_literal_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(189), 7,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_LBRACK,
      anon_sym_BANG,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(191), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [660] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(195), 10,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(197), 10,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [689] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(199), 10,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(201), 10,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [718] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(203), 10,
      anon_sym_LBRACE,
      anon_sym_RBRACE,
      anon_sym_COMMA,
      anon_sym_LBRACK,
      anon_sym_RBRACK,
      anon_sym_BANG,
      anon_sym_RPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(205), 10,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [747] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(113), 8,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_LBRACK,
      anon_sym_BANG,
      anon_sym_LPAREN,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(118), 12,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOT,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [776] = 5,
    ACTIONS(211), 1,
      aux_sym_block_literal_token1,
    STATE(40), 1,
      aux_sym_block_literal_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(207), 7,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_LBRACK,
      anon_sym_BANG,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(209), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [809] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(199), 8,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_COLON,
      anon_sym_LBRACK,
      anon_sym_BANG,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(201), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [837] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(195), 8,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_COLON,
      anon_sym_LBRACK,
      anon_sym_BANG,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(197), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [865] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(203), 8,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_COLON,
      anon_sym_LBRACK,
      anon_sym_BANG,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(205), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [893] = 3,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(214), 7,
      ts_builtin_sym_end,
      anon_sym_LBRACE,
      anon_sym_LBRACK,
      anon_sym_BANG,
      anon_sym_DQUOTE,
      anon_sym_SQUOTE,
      anon_sym_PIPE,
    ACTIONS(216), 11,
      anon_sym_DASH_DASH_DASH,
      anon_sym_DOLLAR_LBRACK,
      anon_sym_DOT_LBRACK,
      sym_literal,
      aux_sym_number_token1,
      aux_sym_number_token2,
      aux_sym_number_token3,
      aux_sym_number_token4,
      anon_sym_true,
      anon_sym_false,
      sym_null,
  [920] = 12,
    ACTIONS(15), 1,
      anon_sym_DQUOTE,
    ACTIONS(17), 1,
      anon_sym_SQUOTE,
    ACTIONS(21), 1,
      anon_sym_DOLLAR_LBRACK,
    ACTIONS(23), 1,
      anon_sym_DOT_LBRACK,
    ACTIONS(218), 1,
      anon_sym_RBRACE,
    ACTIONS(220), 1,
      anon_sym_BANG,
    ACTIONS(222), 1,
      sym_literal,
    STATE(75), 1,
      sym__map_entry,
    STATE(87), 1,
      sym__map_content,
    STATE(97), 1,
      sym_tag,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    STATE(79), 5,
      sym__key,
      sym_tagged_key,
      sym_string,
      sym_interpolation,
      sym_node_replacement,
  [962] = 11,
    ACTIONS(15), 1,
      anon_sym_DQUOTE,
    ACTIONS(17), 1,
      anon_sym_SQUOTE,
    ACTIONS(21), 1,
      anon_sym_DOLLAR_LBRACK,
    ACTIONS(23), 1,
      anon_sym_DOT_LBRACK,
    ACTIONS(220), 1,
      anon_sym_BANG,
    ACTIONS(222), 1,
      sym_literal,
    STATE(78), 1,
      sym__map_entry,
    STATE(97), 1,
      sym_tag,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(224), 2,
      anon_sym_RBRACE,
      anon_sym_COMMA,
    STATE(79), 5,
      sym__key,
      sym_tagged_key,
      sym_string,
      sym_interpolation,
      sym_node_replacement,
  [1002] = 6,
    ACTIONS(228), 1,
      anon_sym_DQUOTE,
    ACTIONS(230), 1,
      aux_sym_string_token1,
    ACTIONS(232), 1,
      sym_string_escape,
    ACTIONS(234), 1,
      anon_sym_DOLLAR_LBRACK,
    ACTIONS(226), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    STATE(48), 2,
      sym_interpolation,
      aux_sym_string_repeat1,
  [1023] = 6,
    ACTIONS(234), 1,
      anon_sym_DOLLAR_LBRACK,
    ACTIONS(236), 1,
      anon_sym_DQUOTE,
    ACTIONS(238), 1,
      aux_sym_string_token1,
    ACTIONS(240), 1,
      sym_string_escape,
    ACTIONS(226), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    STATE(49), 2,
      sym_interpolation,
      aux_sym_string_repeat1,
  [1044] = 6,
    ACTIONS(242), 1,
      anon_sym_DQUOTE,
    ACTIONS(244), 1,
      aux_sym_string_token1,
    ACTIONS(247), 1,
      sym_string_escape,
    ACTIONS(250), 1,
      anon_sym_DOLLAR_LBRACK,
    ACTIONS(226), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    STATE(49), 2,
      sym_interpolation,
      aux_sym_string_repeat1,
  [1065] = 4,
    ACTIONS(253), 1,
      aux_sym_block_literal_token1,
    STATE(51), 1,
      aux_sym_block_literal_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(183), 2,
      anon_sym_COMMA,
      anon_sym_RBRACK,
  [1080] = 4,
    ACTIONS(255), 1,
      aux_sym_block_literal_token1,
    STATE(63), 1,
      aux_sym_block_literal_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(189), 2,
      anon_sym_COMMA,
      anon_sym_RBRACK,
  [1095] = 4,
    ACTIONS(257), 1,
      aux_sym_block_literal_token1,
    STATE(52), 1,
      aux_sym_block_literal_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(207), 2,
      anon_sym_COMMA,
      anon_sym_RPAREN,
  [1110] = 5,
    ACTIONS(260), 1,
      anon_sym_SQUOTE,
    ACTIONS(262), 1,
      aux_sym_string_token2,
    ACTIONS(265), 1,
      sym_string_escape,
    STATE(53), 1,
      aux_sym_string_repeat2,
    ACTIONS(226), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1127] = 4,
    ACTIONS(268), 1,
      aux_sym_block_literal_token1,
    STATE(52), 1,
      aux_sym_block_literal_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(189), 2,
      anon_sym_COMMA,
      anon_sym_RPAREN,
  [1142] = 5,
    ACTIONS(228), 1,
      anon_sym_SQUOTE,
    ACTIONS(270), 1,
      aux_sym_string_token2,
    ACTIONS(272), 1,
      sym_string_escape,
    STATE(57), 1,
      aux_sym_string_repeat2,
    ACTIONS(226), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1159] = 4,
    ACTIONS(274), 1,
      aux_sym_block_literal_token1,
    STATE(54), 1,
      aux_sym_block_literal_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(183), 2,
      anon_sym_COMMA,
      anon_sym_RPAREN,
  [1174] = 5,
    ACTIONS(236), 1,
      anon_sym_SQUOTE,
    ACTIONS(276), 1,
      aux_sym_string_token2,
    ACTIONS(278), 1,
      sym_string_escape,
    STATE(53), 1,
      aux_sym_string_repeat2,
    ACTIONS(226), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1191] = 4,
    ACTIONS(280), 1,
      aux_sym_block_literal_token1,
    STATE(58), 1,
      aux_sym_block_literal_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(207), 2,
      anon_sym_RBRACE,
      anon_sym_COMMA,
  [1206] = 4,
    ACTIONS(283), 1,
      aux_sym_block_literal_token1,
    STATE(58), 1,
      aux_sym_block_literal_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(189), 2,
      anon_sym_RBRACE,
      anon_sym_COMMA,
  [1221] = 4,
    ACTIONS(285), 1,
      aux_sym_block_literal_token1,
    STATE(59), 1,
      aux_sym_block_literal_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(183), 2,
      anon_sym_RBRACE,
      anon_sym_COMMA,
  [1236] = 4,
    ACTIONS(287), 1,
      anon_sym_DOT,
    STATE(61), 1,
      aux_sym_tag_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(113), 2,
      anon_sym_COLON,
      anon_sym_LPAREN,
  [1251] = 3,
    ACTIONS(136), 1,
      aux_sym_string_token1,
    ACTIONS(226), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(134), 3,
      anon_sym_DQUOTE,
      sym_string_escape,
      anon_sym_DOLLAR_LBRACK,
  [1264] = 4,
    ACTIONS(290), 1,
      aux_sym_block_literal_token1,
    STATE(63), 1,
      aux_sym_block_literal_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(207), 2,
      anon_sym_COMMA,
      anon_sym_RBRACK,
  [1279] = 5,
    ACTIONS(120), 1,
      anon_sym_COLON,
    ACTIONS(181), 1,
      anon_sym_LPAREN,
    ACTIONS(293), 1,
      anon_sym_DOT,
    STATE(61), 1,
      aux_sym_tag_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1296] = 5,
    ACTIONS(128), 1,
      anon_sym_COLON,
    ACTIONS(176), 1,
      anon_sym_LPAREN,
    ACTIONS(293), 1,
      anon_sym_DOT,
    STATE(64), 1,
      aux_sym_tag_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1313] = 4,
    ACTIONS(295), 1,
      anon_sym_COMMA,
    ACTIONS(297), 1,
      anon_sym_RPAREN,
    STATE(69), 1,
      aux_sym__array_content_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1327] = 4,
    ACTIONS(299), 1,
      anon_sym_COMMA,
    ACTIONS(302), 1,
      anon_sym_RBRACK,
    STATE(67), 1,
      aux_sym__array_content_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1341] = 4,
    ACTIONS(304), 1,
      anon_sym_COMMA,
    ACTIONS(306), 1,
      anon_sym_RBRACK,
    STATE(72), 1,
      aux_sym__array_content_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1355] = 4,
    ACTIONS(302), 1,
      anon_sym_RPAREN,
    ACTIONS(308), 1,
      anon_sym_COMMA,
    STATE(69), 1,
      aux_sym__array_content_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1369] = 4,
    ACTIONS(311), 1,
      anon_sym_RBRACE,
    ACTIONS(313), 1,
      anon_sym_COMMA,
    STATE(70), 1,
      aux_sym__map_content_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1383] = 2,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(302), 3,
      anon_sym_COMMA,
      anon_sym_RBRACK,
      anon_sym_RPAREN,
  [1393] = 4,
    ACTIONS(304), 1,
      anon_sym_COMMA,
    ACTIONS(316), 1,
      anon_sym_RBRACK,
    STATE(67), 1,
      aux_sym__array_content_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1407] = 2,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(113), 3,
      anon_sym_COLON,
      anon_sym_DOT,
      anon_sym_LPAREN,
  [1417] = 4,
    ACTIONS(295), 1,
      anon_sym_COMMA,
    ACTIONS(318), 1,
      anon_sym_RPAREN,
    STATE(66), 1,
      aux_sym__array_content_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1431] = 4,
    ACTIONS(320), 1,
      anon_sym_RBRACE,
    ACTIONS(322), 1,
      anon_sym_COMMA,
    STATE(76), 1,
      aux_sym__map_content_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1445] = 4,
    ACTIONS(322), 1,
      anon_sym_COMMA,
    ACTIONS(324), 1,
      anon_sym_RBRACE,
    STATE(70), 1,
      aux_sym__map_content_repeat1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1459] = 2,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(326), 2,
      anon_sym_RBRACE,
      anon_sym_COMMA,
  [1468] = 2,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
    ACTIONS(311), 2,
      anon_sym_RBRACE,
      anon_sym_COMMA,
  [1477] = 2,
    ACTIONS(328), 1,
      anon_sym_COLON,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1485] = 2,
    ACTIONS(99), 1,
      anon_sym_RPAREN,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1493] = 2,
    ACTIONS(330), 1,
      anon_sym_COLON,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1501] = 2,
    ACTIONS(332), 1,
      aux_sym_interpolation_token1,
    ACTIONS(226), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1509] = 2,
    ACTIONS(334), 1,
      anon_sym_RPAREN,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1517] = 2,
    ACTIONS(336), 1,
      aux_sym_interpolation_token1,
    ACTIONS(226), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1525] = 2,
    ACTIONS(338), 1,
      aux_sym_tag_token1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1533] = 2,
    ACTIONS(101), 1,
      anon_sym_RPAREN,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1541] = 2,
    ACTIONS(340), 1,
      anon_sym_RBRACE,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1549] = 2,
    ACTIONS(342), 1,
      anon_sym_RBRACK,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1557] = 2,
    ACTIONS(344), 1,
      aux_sym_interpolation_token1,
    ACTIONS(226), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1565] = 2,
    ACTIONS(346), 1,
      anon_sym_RBRACK,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1573] = 2,
    ACTIONS(348), 1,
      ts_builtin_sym_end,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1581] = 2,
    ACTIONS(350), 1,
      anon_sym_RBRACK,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1589] = 2,
    ACTIONS(352), 1,
      aux_sym_tag_token1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1597] = 2,
    ACTIONS(354), 1,
      aux_sym_tag_token1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1605] = 2,
    ACTIONS(356), 1,
      anon_sym_RBRACK,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1613] = 2,
    ACTIONS(358), 1,
      aux_sym_tag_token1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1621] = 2,
    ACTIONS(360), 1,
      anon_sym_COLON,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1629] = 2,
    ACTIONS(362), 1,
      anon_sym_RPAREN,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1637] = 2,
    ACTIONS(364), 1,
      aux_sym_tag_token1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1645] = 2,
    ACTIONS(366), 1,
      aux_sym_tag_token1,
    ACTIONS(3), 2,
      sym_comment,
      aux_sym_document_separator_token1,
  [1653] = 2,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(368), 1,
      aux_sym_document_separator_token1,
};

static const uint32_t ts_small_parse_table_map[] = {
  [SMALL_STATE(16)] = 0,
  [SMALL_STATE(17)] = 32,
  [SMALL_STATE(18)] = 68,
  [SMALL_STATE(19)] = 106,
  [SMALL_STATE(20)] = 144,
  [SMALL_STATE(21)] = 176,
  [SMALL_STATE(22)] = 208,
  [SMALL_STATE(23)] = 240,
  [SMALL_STATE(24)] = 271,
  [SMALL_STATE(25)] = 302,
  [SMALL_STATE(26)] = 333,
  [SMALL_STATE(27)] = 364,
  [SMALL_STATE(28)] = 395,
  [SMALL_STATE(29)] = 426,
  [SMALL_STATE(30)] = 457,
  [SMALL_STATE(31)] = 488,
  [SMALL_STATE(32)] = 524,
  [SMALL_STATE(33)] = 558,
  [SMALL_STATE(34)] = 594,
  [SMALL_STATE(35)] = 627,
  [SMALL_STATE(36)] = 660,
  [SMALL_STATE(37)] = 689,
  [SMALL_STATE(38)] = 718,
  [SMALL_STATE(39)] = 747,
  [SMALL_STATE(40)] = 776,
  [SMALL_STATE(41)] = 809,
  [SMALL_STATE(42)] = 837,
  [SMALL_STATE(43)] = 865,
  [SMALL_STATE(44)] = 893,
  [SMALL_STATE(45)] = 920,
  [SMALL_STATE(46)] = 962,
  [SMALL_STATE(47)] = 1002,
  [SMALL_STATE(48)] = 1023,
  [SMALL_STATE(49)] = 1044,
  [SMALL_STATE(50)] = 1065,
  [SMALL_STATE(51)] = 1080,
  [SMALL_STATE(52)] = 1095,
  [SMALL_STATE(53)] = 1110,
  [SMALL_STATE(54)] = 1127,
  [SMALL_STATE(55)] = 1142,
  [SMALL_STATE(56)] = 1159,
  [SMALL_STATE(57)] = 1174,
  [SMALL_STATE(58)] = 1191,
  [SMALL_STATE(59)] = 1206,
  [SMALL_STATE(60)] = 1221,
  [SMALL_STATE(61)] = 1236,
  [SMALL_STATE(62)] = 1251,
  [SMALL_STATE(63)] = 1264,
  [SMALL_STATE(64)] = 1279,
  [SMALL_STATE(65)] = 1296,
  [SMALL_STATE(66)] = 1313,
  [SMALL_STATE(67)] = 1327,
  [SMALL_STATE(68)] = 1341,
  [SMALL_STATE(69)] = 1355,
  [SMALL_STATE(70)] = 1369,
  [SMALL_STATE(71)] = 1383,
  [SMALL_STATE(72)] = 1393,
  [SMALL_STATE(73)] = 1407,
  [SMALL_STATE(74)] = 1417,
  [SMALL_STATE(75)] = 1431,
  [SMALL_STATE(76)] = 1445,
  [SMALL_STATE(77)] = 1459,
  [SMALL_STATE(78)] = 1468,
  [SMALL_STATE(79)] = 1477,
  [SMALL_STATE(80)] = 1485,
  [SMALL_STATE(81)] = 1493,
  [SMALL_STATE(82)] = 1501,
  [SMALL_STATE(83)] = 1509,
  [SMALL_STATE(84)] = 1517,
  [SMALL_STATE(85)] = 1525,
  [SMALL_STATE(86)] = 1533,
  [SMALL_STATE(87)] = 1541,
  [SMALL_STATE(88)] = 1549,
  [SMALL_STATE(89)] = 1557,
  [SMALL_STATE(90)] = 1565,
  [SMALL_STATE(91)] = 1573,
  [SMALL_STATE(92)] = 1581,
  [SMALL_STATE(93)] = 1589,
  [SMALL_STATE(94)] = 1597,
  [SMALL_STATE(95)] = 1605,
  [SMALL_STATE(96)] = 1613,
  [SMALL_STATE(97)] = 1621,
  [SMALL_STATE(98)] = 1629,
  [SMALL_STATE(99)] = 1637,
  [SMALL_STATE(100)] = 1645,
  [SMALL_STATE(101)] = 1653,
};

static const TSParseActionEntry ts_parse_actions[] = {
  [0] = {.entry = {.count = 0, .reusable = false}},
  [1] = {.entry = {.count = 1, .reusable = false}}, RECOVER(),
  [3] = {.entry = {.count = 1, .reusable = true}}, SHIFT_EXTRA(),
  [5] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_source_file, 0),
  [7] = {.entry = {.count = 1, .reusable = false}}, SHIFT(101),
  [9] = {.entry = {.count = 1, .reusable = true}}, SHIFT(45),
  [11] = {.entry = {.count = 1, .reusable = true}}, SHIFT(14),
  [13] = {.entry = {.count = 1, .reusable = true}}, SHIFT(94),
  [15] = {.entry = {.count = 1, .reusable = true}}, SHIFT(47),
  [17] = {.entry = {.count = 1, .reusable = true}}, SHIFT(55),
  [19] = {.entry = {.count = 1, .reusable = true}}, SHIFT(34),
  [21] = {.entry = {.count = 1, .reusable = false}}, SHIFT(84),
  [23] = {.entry = {.count = 1, .reusable = false}}, SHIFT(82),
  [25] = {.entry = {.count = 1, .reusable = false}}, SHIFT(2),
  [27] = {.entry = {.count = 1, .reusable = false}}, SHIFT(30),
  [29] = {.entry = {.count = 1, .reusable = false}}, SHIFT(29),
  [31] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_source_file, 1),
  [33] = {.entry = {.count = 1, .reusable = false}}, SHIFT(3),
  [35] = {.entry = {.count = 1, .reusable = true}}, REDUCE(aux_sym_source_file_repeat1, 2),
  [37] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_source_file_repeat1, 2), SHIFT_REPEAT(101),
  [40] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_source_file_repeat1, 2), SHIFT_REPEAT(45),
  [43] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_source_file_repeat1, 2), SHIFT_REPEAT(14),
  [46] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_source_file_repeat1, 2), SHIFT_REPEAT(94),
  [49] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_source_file_repeat1, 2), SHIFT_REPEAT(47),
  [52] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_source_file_repeat1, 2), SHIFT_REPEAT(55),
  [55] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_source_file_repeat1, 2), SHIFT_REPEAT(34),
  [58] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_source_file_repeat1, 2), SHIFT_REPEAT(84),
  [61] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_source_file_repeat1, 2), SHIFT_REPEAT(82),
  [64] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_source_file_repeat1, 2), SHIFT_REPEAT(3),
  [67] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_source_file_repeat1, 2), SHIFT_REPEAT(30),
  [70] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_source_file_repeat1, 2), SHIFT_REPEAT(29),
  [73] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_tagged_value, 1),
  [75] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_tagged_value, 1),
  [77] = {.entry = {.count = 1, .reusable = true}}, SHIFT(99),
  [79] = {.entry = {.count = 1, .reusable = true}}, SHIFT(56),
  [81] = {.entry = {.count = 1, .reusable = false}}, SHIFT(25),
  [83] = {.entry = {.count = 1, .reusable = true}}, REDUCE(aux_sym__array_content_repeat1, 1),
  [85] = {.entry = {.count = 1, .reusable = true}}, SHIFT(50),
  [87] = {.entry = {.count = 1, .reusable = false}}, SHIFT(71),
  [89] = {.entry = {.count = 1, .reusable = true}}, SHIFT(43),
  [91] = {.entry = {.count = 1, .reusable = false}}, SHIFT(74),
  [93] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym__map_entry, 2),
  [95] = {.entry = {.count = 1, .reusable = true}}, SHIFT(60),
  [97] = {.entry = {.count = 1, .reusable = false}}, SHIFT(77),
  [99] = {.entry = {.count = 1, .reusable = true}}, SHIFT(37),
  [101] = {.entry = {.count = 1, .reusable = true}}, SHIFT(41),
  [103] = {.entry = {.count = 1, .reusable = true}}, SHIFT(26),
  [105] = {.entry = {.count = 1, .reusable = false}}, SHIFT(68),
  [107] = {.entry = {.count = 1, .reusable = true}}, SHIFT(38),
  [109] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_string, 3),
  [111] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_string, 3),
  [113] = {.entry = {.count = 1, .reusable = true}}, REDUCE(aux_sym_tag_repeat1, 2),
  [115] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_tag_repeat1, 2), SHIFT_REPEAT(100),
  [118] = {.entry = {.count = 1, .reusable = false}}, REDUCE(aux_sym_tag_repeat1, 2),
  [120] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_tag, 3),
  [122] = {.entry = {.count = 1, .reusable = false}}, SHIFT(100),
  [124] = {.entry = {.count = 1, .reusable = true}}, SHIFT(10),
  [126] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_tag, 3),
  [128] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_tag, 2),
  [130] = {.entry = {.count = 1, .reusable = true}}, SHIFT(15),
  [132] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_tag, 2),
  [134] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_interpolation, 3),
  [136] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_interpolation, 3),
  [138] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_node_replacement, 3),
  [140] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_node_replacement, 3),
  [142] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_string, 2),
  [144] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_string, 2),
  [146] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_bracketed_map, 2),
  [148] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_bracketed_map, 2),
  [150] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_bracketed_array, 3),
  [152] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_bracketed_array, 3),
  [154] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_tagged_value, 2),
  [156] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_tagged_value, 2),
  [158] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_bracketed_array, 2),
  [160] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_bracketed_array, 2),
  [162] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_bracketed_map, 3),
  [164] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_bracketed_map, 3),
  [166] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_boolean, 1),
  [168] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_boolean, 1),
  [170] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_number, 1),
  [172] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_number, 1),
  [174] = {.entry = {.count = 1, .reusable = false}}, SHIFT(85),
  [176] = {.entry = {.count = 1, .reusable = true}}, SHIFT(7),
  [178] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_tag_repeat1, 2), SHIFT_REPEAT(85),
  [181] = {.entry = {.count = 1, .reusable = true}}, SHIFT(13),
  [183] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_block_literal, 1),
  [185] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_block_literal, 1),
  [187] = {.entry = {.count = 1, .reusable = false}}, SHIFT(35),
  [189] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_block_literal, 2),
  [191] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_block_literal, 2),
  [193] = {.entry = {.count = 1, .reusable = false}}, SHIFT(40),
  [195] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_tag, 6),
  [197] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_tag, 6),
  [199] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_tag, 5),
  [201] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_tag, 5),
  [203] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_tag, 4),
  [205] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_tag, 4),
  [207] = {.entry = {.count = 1, .reusable = true}}, REDUCE(aux_sym_block_literal_repeat1, 2),
  [209] = {.entry = {.count = 1, .reusable = false}}, REDUCE(aux_sym_block_literal_repeat1, 2),
  [211] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_block_literal_repeat1, 2), SHIFT_REPEAT(40),
  [214] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_document_separator, 2),
  [216] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_document_separator, 2),
  [218] = {.entry = {.count = 1, .reusable = true}}, SHIFT(23),
  [220] = {.entry = {.count = 1, .reusable = true}}, SHIFT(96),
  [222] = {.entry = {.count = 1, .reusable = false}}, SHIFT(79),
  [224] = {.entry = {.count = 1, .reusable = true}}, REDUCE(aux_sym__map_content_repeat1, 1),
  [226] = {.entry = {.count = 1, .reusable = false}}, SHIFT_EXTRA(),
  [228] = {.entry = {.count = 1, .reusable = true}}, SHIFT(22),
  [230] = {.entry = {.count = 1, .reusable = false}}, SHIFT(48),
  [232] = {.entry = {.count = 1, .reusable = true}}, SHIFT(48),
  [234] = {.entry = {.count = 1, .reusable = true}}, SHIFT(89),
  [236] = {.entry = {.count = 1, .reusable = true}}, SHIFT(16),
  [238] = {.entry = {.count = 1, .reusable = false}}, SHIFT(49),
  [240] = {.entry = {.count = 1, .reusable = true}}, SHIFT(49),
  [242] = {.entry = {.count = 1, .reusable = true}}, REDUCE(aux_sym_string_repeat1, 2),
  [244] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_string_repeat1, 2), SHIFT_REPEAT(49),
  [247] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_string_repeat1, 2), SHIFT_REPEAT(49),
  [250] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_string_repeat1, 2), SHIFT_REPEAT(89),
  [253] = {.entry = {.count = 1, .reusable = false}}, SHIFT(51),
  [255] = {.entry = {.count = 1, .reusable = false}}, SHIFT(63),
  [257] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_block_literal_repeat1, 2), SHIFT_REPEAT(52),
  [260] = {.entry = {.count = 1, .reusable = true}}, REDUCE(aux_sym_string_repeat2, 2),
  [262] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_string_repeat2, 2), SHIFT_REPEAT(53),
  [265] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_string_repeat2, 2), SHIFT_REPEAT(53),
  [268] = {.entry = {.count = 1, .reusable = false}}, SHIFT(52),
  [270] = {.entry = {.count = 1, .reusable = false}}, SHIFT(57),
  [272] = {.entry = {.count = 1, .reusable = true}}, SHIFT(57),
  [274] = {.entry = {.count = 1, .reusable = false}}, SHIFT(54),
  [276] = {.entry = {.count = 1, .reusable = false}}, SHIFT(53),
  [278] = {.entry = {.count = 1, .reusable = true}}, SHIFT(53),
  [280] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_block_literal_repeat1, 2), SHIFT_REPEAT(58),
  [283] = {.entry = {.count = 1, .reusable = false}}, SHIFT(58),
  [285] = {.entry = {.count = 1, .reusable = false}}, SHIFT(59),
  [287] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_tag_repeat1, 2), SHIFT_REPEAT(93),
  [290] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_block_literal_repeat1, 2), SHIFT_REPEAT(63),
  [293] = {.entry = {.count = 1, .reusable = true}}, SHIFT(93),
  [295] = {.entry = {.count = 1, .reusable = true}}, SHIFT(12),
  [297] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym__tag_arguments, 2),
  [299] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym__array_content_repeat1, 2), SHIFT_REPEAT(6),
  [302] = {.entry = {.count = 1, .reusable = true}}, REDUCE(aux_sym__array_content_repeat1, 2),
  [304] = {.entry = {.count = 1, .reusable = true}}, SHIFT(6),
  [306] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym__array_content, 1),
  [308] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym__array_content_repeat1, 2), SHIFT_REPEAT(12),
  [311] = {.entry = {.count = 1, .reusable = true}}, REDUCE(aux_sym__map_content_repeat1, 2),
  [313] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym__map_content_repeat1, 2), SHIFT_REPEAT(46),
  [316] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym__array_content, 2),
  [318] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym__tag_arguments, 1),
  [320] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym__map_content, 1),
  [322] = {.entry = {.count = 1, .reusable = true}}, SHIFT(46),
  [324] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym__map_content, 2),
  [326] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym__map_entry, 3),
  [328] = {.entry = {.count = 1, .reusable = true}}, SHIFT(8),
  [330] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_tagged_key, 2),
  [332] = {.entry = {.count = 1, .reusable = false}}, SHIFT(95),
  [334] = {.entry = {.count = 1, .reusable = true}}, SHIFT(42),
  [336] = {.entry = {.count = 1, .reusable = false}}, SHIFT(88),
  [338] = {.entry = {.count = 1, .reusable = true}}, SHIFT(39),
  [340] = {.entry = {.count = 1, .reusable = true}}, SHIFT(27),
  [342] = {.entry = {.count = 1, .reusable = true}}, SHIFT(20),
  [344] = {.entry = {.count = 1, .reusable = false}}, SHIFT(90),
  [346] = {.entry = {.count = 1, .reusable = true}}, SHIFT(62),
  [348] = {.entry = {.count = 1, .reusable = true}},  ACCEPT_INPUT(),
  [350] = {.entry = {.count = 1, .reusable = true}}, SHIFT(24),
  [352] = {.entry = {.count = 1, .reusable = true}}, SHIFT(73),
  [354] = {.entry = {.count = 1, .reusable = true}}, SHIFT(31),
  [356] = {.entry = {.count = 1, .reusable = true}}, SHIFT(21),
  [358] = {.entry = {.count = 1, .reusable = true}}, SHIFT(65),
  [360] = {.entry = {.count = 1, .reusable = true}}, SHIFT(81),
  [362] = {.entry = {.count = 1, .reusable = true}}, SHIFT(36),
  [364] = {.entry = {.count = 1, .reusable = true}}, SHIFT(19),
  [366] = {.entry = {.count = 1, .reusable = true}}, SHIFT(28),
  [368] = {.entry = {.count = 1, .reusable = true}}, SHIFT(44),
};

#ifdef __cplusplus
extern "C" {
#endif
#ifdef _WIN32
#define extern __declspec(dllexport)
#endif

extern const TSLanguage *tree_sitter_tony(void) {
  static const TSLanguage language = {
    .version = LANGUAGE_VERSION,
    .symbol_count = SYMBOL_COUNT,
    .alias_count = ALIAS_COUNT,
    .token_count = TOKEN_COUNT,
    .external_token_count = EXTERNAL_TOKEN_COUNT,
    .state_count = STATE_COUNT,
    .large_state_count = LARGE_STATE_COUNT,
    .production_id_count = PRODUCTION_ID_COUNT,
    .field_count = FIELD_COUNT,
    .max_alias_sequence_length = MAX_ALIAS_SEQUENCE_LENGTH,
    .parse_table = &ts_parse_table[0][0],
    .small_parse_table = ts_small_parse_table,
    .small_parse_table_map = ts_small_parse_table_map,
    .parse_actions = ts_parse_actions,
    .symbol_names = ts_symbol_names,
    .symbol_metadata = ts_symbol_metadata,
    .public_symbol_map = ts_symbol_map,
    .alias_map = ts_non_terminal_alias_map,
    .alias_sequences = &ts_alias_sequences[0][0],
    .lex_modes = ts_lex_modes,
    .lex_fn = ts_lex,
    .primary_state_ids = ts_primary_state_ids,
  };
  return &language;
}
#ifdef __cplusplus
}
#endif
