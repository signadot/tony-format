# Eval Operations

This page documents all eval operations.

## `!eval`

**Evaluate environment variables in a document**

The !eval operation expands environment variables in the document. Variables are referenced using $varName syntax and are replaced with values from the evaluation environment.

**Child:** Document with $variable references

**Examples:**

1. ```tony
name: !eval "$USER"
```

2. ```tony
path: !eval "/home/$USER/config"
```

**See also:** [`!os_env`](./eval.md#os_env), [`!file`](./eval.md#file)

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

The !file operation loads content from a local file path or HTTP/HTTPS URL. The child must be a string containing the path or URL. The loaded content is parsed as Tony format.

**Child:** String containing file path or URL

**Examples:**

1. ```tony
config: !file "/etc/config.tony"
```

2. ```tony
data: !file "https://example.com/data.tony"
```

**See also:** [`!exec`](./eval.md#exec), [`!eval`](./eval.md#eval)

---

## `!os_env`

**Get value from OS environment variable**

The !os_env operation retrieves the value of an OS environment variable. The child must be a string containing the environment variable name.

**Child:** String containing environment variable name

**Examples:**

1. ```tony
home: !os_env "HOME"
```

2. ```tony
path: !os_env "PATH"
```

**See also:** [`!eval`](./eval.md#eval)

---

## `!to_int`

**Convert a value to an integer**

The !to_int operation converts its child value to an integer. The child must be a string representation of a number.

**Child:** String value to convert

**Examples:**

1. ```tony
version: !to_int "123"
```

**See also:** [`!to_string`](./eval.md#to_string), [`!to_value`](./eval.md#to_value)

---

## `!to_string`

**Convert a value to a string**

The !to_string operation converts its child value to a string representation.

**Child:** Value to convert

**Examples:**

1. ```tony
version_str: !to_string 123
```

**See also:** [`!to_int`](./eval.md#to_int), [`!to_value`](./eval.md#to_value)

---

