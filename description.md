# --title Encoder outputs empty object without space, parser rejects it

When encoding YAML, empty objects are output as `key:{}` without a space before the brace.

**Example:**
- Input: `status: {}`
- Output after encode: `status:{}`

The parser then rejects this as `unexpected TLiteral "status:{}"`.

**Reproduction:**
```
o build controllers -I y | grep "status:{}"
```

The encoder should output `status: {}` (with space) to be consistent with standard YAML and to be parseable by the tony parser.