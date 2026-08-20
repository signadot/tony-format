module.exports = grammar({
  name: 'tony',

  extras: $ => [
    /\s/,
    $.comment,
  ],

  rules: {
    source_file: $ => repeat($._top_level),

    _top_level: $ => choice(
      $.document_separator,
      $._value
    ),

    document_separator: $ => seq('---', /\s/),

    _value: $ => choice(
      $.bracketed_map,
      $.bracketed_array,
      $.string,
      $.block_literal,
      $.number,
      $.boolean,
      $.null,
      $.tagged_value,
      $.literal,
      $.interpolation,
      $.node_replacement
    ),

    bracketed_map: $ => seq(
      '{',
      optional($._map_content),
      '}'
    ),

    _map_content: $ => seq(
      $._map_entry,
      repeat(seq(',', optional($._map_entry)))
    ),

    _map_entry: $ => seq(
      $._key,
      ':',
      optional($._value)
    ),

    bracketed_array: $ => seq(
      '[',
      optional($._array_content),
      ']'
    ),

    _array_content: $ => seq(
      $._value,
      repeat(seq(',', optional($._value)))
    ),

    _key: $ => choice(
      $.tagged_key,
      $.string,
      $.key_literal,
      $.interpolation,
      $.node_replacement
    ),

    tag: $ => choice(
      seq('!', /[a-zA-Z0-9_.:\/+=~@$%^&*-]+/, repeat(seq('.', /[a-zA-Z0-9_.:\/+=~@$%^&*-]+/))),
      seq('!', /[a-zA-Z0-9_.:\/+=~@$%^&*-]+/, repeat(seq('.', /[a-zA-Z0-9_.:\/+=~@$%^&*-]+/)), '(', optional($._tag_arguments), ')')
    ),

    _tag_arguments: $ => seq(
      $._value,
      repeat(seq(',', optional($._value)))
    ),

    tagged_value: $ => prec.left(1, seq(
      $.tag,
      optional($._value)
    )),

    tagged_key: $ => seq(
      $.tag,
      ':'
    ),

    merge_key: $ => seq('<<', ':'),

    string: $ => choice(
      seq('"', repeat(choice($.string_escape, $.interpolation, /[^"\\$]+/)), '"'),
      seq("'", repeat(choice($.string_escape, /[^'\\]+/)), "'")
    ),

    string_escape: $ => seq(
      '\\',
      choice(
        /[\\"\'nrtbf]/,
        /u[0-9a-fA-F]{4}/
      )
    ),

    block_literal: $ => seq('|', repeat(/[^\n]/)),

    interpolation: $ => seq('$[', /[^\]]+/, ']'),

    node_replacement: $ => seq('.[', /[^\]]+/, ']'),

    key_literal: $ => /[a-zA-Z_$~@\/._+\-\\*%=][a-zA-Z0-9_$~@\/:.\-\\*%=!\[\]{}\(\)]*/,

    literal: $ => /[a-zA-Z_$~@\/._+\-\\*%=][a-zA-Z0-9_$~@\/:.\-\\*%=!\[\]{}\(\)]*/,

    number: $ => choice(
      /-?\d+\.\d+(?:[eE][+-]?\d+)?/,
      /-?\d+(?:[eE][+-]?\d+)?/,
      /-?0[xX][0-9a-fA-F]+/,
      /-?0[oO][0-7]+/
    ),

    boolean: $ => choice('true', 'false'),

    null: $ => 'null',

    comment: $ => seq('#', /.*/),
  }
});
