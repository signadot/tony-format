# Eval Operations

This page documents all eval operations.

## `!eval`

**Evaluate environment variables in a document**

The !eval operation expands the expressions in the strings beneath it. Each is an [expr-lang](https://expr-lang.org/) expression evaluated against the evaluation environment (`o eval -e name=value`). `$[expr]` is replaced by the expression's value as text; a string that is exactly `.[expr]` is replaced by the value itself, keeping its type. `$USER` is not an expression and stays as it is written; `.[getenv("USER")]` reads the OS environment.

**Child:** Document whose strings hold `$[...]` or `.[...]` expressions

**Examples:**

1. ```tony
name: !eval '.[getenv("USER")]'
```

2. ```tony
path: !eval "/home/$[user]/config"
```

**See also:** [`!osenv`](./eval.md#osenv), [`!file`](./eval.md#file)

---

## `!exec`

**Execute a shell command and capture output**

The !exec operation executes a shell command and captures its stdout. The child must be a string containing the command to execute. The command is run with 'sh -c'.

**Child:** String containing shell command

**Examples:**

1. ```tony
date: !exec "date -u +%Y-%m-%d"
```

2. ```tony
hostname: !exec "hostname"
```

**See also:** [`!file`](./eval.md#file), [`!script`](./eval.md#script)

---

## `!file`

**Load content from a file or URL**

The !file operation loads content from a local file path or HTTP/HTTPS URL. The child must be a string containing the path or URL. The content is answered as a string, not parsed; `!tovalue.file` parses it.

**Child:** String containing file path or URL

**Examples:**

1. ```tony
motd: !file "/etc/motd"
```

2. ```tony
data: !tovalue.file "https://example.com/data.tony"
```

**See also:** [`!exec`](./eval.md#exec), [`!eval`](./eval.md#eval)

---

## `!osenv`

**Get value from OS environment variable**

The !osenv operation retrieves the value of an OS environment variable. The child must be a string containing the environment variable name.

**Child:** String containing environment variable name

**Examples:**

1. ```tony
home: !osenv "HOME"
```

2. ```tony
path: !osenv "PATH"
```

**See also:** [`!eval`](./eval.md#eval)

---

## `!toint`

**Convert a value to an integer**

The !toint operation converts its child value to an integer. A number converts when it is a whole number an int64 holds (`3.0` is `3`; `3.5` is refused), `true` and `false` are `1` and `0`, and a string must hold a base-10 integer.

**Child:** Number, bool, or string value to convert

**Examples:**

1. ```tony
version: !toint "123"
```

**See also:** [`!tostring`](./eval.md#tostring), [`!tovalue`](./eval.md#tovalue)

---

## `!tostring`

**Convert a value to a string**

The !tostring operation converts its child value to a string representation.

**Child:** Value to convert

**Examples:**

1. ```tony
version_str: !tostring 123
```

**See also:** [`!toint`](./eval.md#toint), [`!tovalue`](./eval.md#tovalue)

---

