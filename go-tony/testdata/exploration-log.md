# o command exploration log

## Session overview
Explored the `o` command and go-tony libraries to find bugs and understand behavior.

## Bugs found

### Issue #91: eval panic with no arguments
```bash
$ o eval
panic: runtime error: invalid memory address or nil pointer dereference
```

### Issue #92: view panic on empty input
```bash
$ echo '' | o view -
panic: runtime error: invalid memory address or nil pointer dereference
```

### Issue #90: CLOSED - false positive
Initially reported tag parsing issue `{b: !tag val}` but this was caused by shell escaping `!` to `\!`. Actual parsing works correctly.

## Commands tested

### Basic view/parse
```bash
# Works
cat << 'EOF' | o view -
{b: !tag val}
EOF

# Works - tagged object
cat << 'EOF' | o view -
{b: !tag {x: 1}}
EOF

# Works - multiline literal
cat << 'EOF' | o view -
script: |
  echo "hello"
  echo "world"
EOF

# Works - empty containers
cat << 'EOF' | o view -
empty_arr: []
empty_obj: {}
EOF

# Works - sparse array
cat << 'EOF' | o view -
{0: a, 5: b, 10: c}
EOF
# Output: !sparsearray with integer keys

# Works - key sets (bracketed shorthand)
cat << 'EOF' | o view -
{a b c d}
EOF
# Output: {a: null, b: null, c: null, d: null}
```

### Format conversion
```bash
# JSON to Tony
cat << 'EOF' | o -I json -O tony view -
{"key": "value", "num": 123, "arr": [1, 2, 3]}
EOF

# YAML mode handles multi-word unquoted strings
cat << 'EOF' | o -y view -
key: hello world
EOF
# Output: key: "hello world"
```

### Path operations
```bash
# Array indexing works
cat << 'EOF' | o get '.items[1]' -
items:
- first
- second
- third
EOF
# Output: second

# Negative index fails (expected - uses ParseUint)
cat << 'EOF' | o get '.items[-1]' -
...
EOF
# Error: strconv.ParseUint: parsing "-1": invalid syntax
```

### Diff and patch
```bash
# Diff works (exit 1 when differences found)
o diff /tmp/a.tony /tmp/b.tony
# Output shows !replace tags for changed values

# Patch with from/to format
cat << 'EOF' > /tmp/patch.tony
b: !replace
  from: 2
  to: 99
EOF
o patch -f /tmp/patch.tony /tmp/base.tony
# Output: patched result
```

### Merge keys
```bash
# Merge key with string value
cat << 'EOF' | o view -
spec:
  annotations:
    <<: |
      key1: value1
EOF

# Expand merge keys with -x
cat << 'EOF' | o -x view -
spec:
  annotations:
    <<: |
      key1: value1
EOF
# Output: merge content injected at indentation level
```

### Eval tags
```bash
# File tag works
echo "hello world" > /tmp/testfile.txt
cat << 'EOF' | o eval -
content: !file /tmp/testfile.txt
EOF
# Output: content: |
#   hello world

# List available eval tags
o eval -tags
# Output: file, exec, toint, b64enc, script, osenv, eval, tostring, tovalue
```

## Behaviors requiring documentation

### Multi-word unquoted values fail in Tony mode
```bash
cat << 'EOF' | o view -
key: hello world
EOF
# Error: imbalanced document: key not followed by :
# Fix: quote the value: key: "hello world"
```

### Date-like values fail
```bash
cat << 'EOF' | o view -
created: 2024-01-15T10:30:00Z
EOF
# Error: leading zero at ... -01 ...
# Fix: quote timestamps: created: "2024-01-15T10:30:00Z"
```

### Large integers fail
```bash
cat << 'EOF' | o view -
big: 99999999999999999999999999999999
EOF
# Error: value out of range
# Integers must fit in int64
```

### .[x] syntax requires environment
```bash
# This returns null because x isn't in scope
cat << 'EOF' | o eval -
x: 1
y: !eval .[x]
EOF
# y evaluates to null

# .[x] works within strings when env is provided programmatically
# or via $[x] interpolation in string context
```

### Reserved words as keys
```bash
# true/false as keys get quoted in output
cat << 'EOF' | o view -
true: value
EOF
# Output: "true": value
```
