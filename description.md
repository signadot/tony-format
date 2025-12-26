# parser: tagged values broken in bracketed maps

# parser: tagged values broken in bracketed maps

Tags followed by a space and value inside bracketed maps are not parsed correctly.

## Examples

```bash
# Fails - tag treated as key
$ echo '{b: !tag val}' | o view
{
  b: \!tag
  val: null
}

# Fails - "mixed key types"
$ echo '{b: !replace 3}' | o view
error decoding document 0: parse error: mixed key types in map

# Fails - tagged object
$ echo '{b: !tag {x: 1}}' | o view
error decoding document 0: imbalanced document: "{" is not a key

# Works - no value after tag
$ echo '{b: !tag(arg)}' | o view
{b: \!tag(arg)}

# Works - indented format
$ cat <<'EOF' | o view
b: !tag
  x: 1
EOF
b: !tag
  x: 1
```

## Expected

`{b: !tag val}` should parse as a map with key `b` having a tagged value `!tag val`, not as a map with two keys `!tag` and `val`.

## Impact

This breaks `o patch` for common patch operations like `{field: !replace newval}`.