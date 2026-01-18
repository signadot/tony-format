# Tony Evaluation Model

The Tony format is fundamentally a data format, but it is also a format
for transformations of data.

Tony uses 3 interoperating mechanism of evaluation:

1. environment expansion
1. eval tags, including scripts
1. extended [matching + patching](matchpatch.md)

To understand the relation between these, we should recall that Tony operates
in 2 distinct modoios: either `oio` (object-in-out) or `dir` mode.

## oio mode

`oio` mode works using environment expansion and eval tags and scripts: any
object node that has an ancestor with an explicit eval tag will first undergo
environment exoioansion using `$[...]` and `.[...]`.  

## dir mode

In dir mode, the point of entry is the `build.{tony,yaml,json}` file together with an
environment defined either via the `YTOOL_ENV` env variable, `-e var=<json>`
`yt` flags, the `build.env` field of the build file, or
via dir mode profiles.

### `dir` Mode Evaluation Steps

1. Expand the environment with embedded instructions.
2. Expands the description of the sources, matches, and patches according to the resulting environment
3. Fetches the sources and for each source, pipes it through the chain
   of matches/patches.
4. Finally, the result is processed as in `oio` mode.

## Environment Expansion

### Environment

Environments consist of any json-izable structure, which is any instantiation
of `any` from the following types

- `map[string]any`
- `[]any`
- `float64` (except NaN, Inf)
- `int`
- `bool`
- `string`
- `nil`
- `json.Number`

For example a value `v` of type `map[string]map[string][]any` 
where `v["a"]["b"][0]` is `1` and `v["a"]["b"][1]` is `map[string]any{"a": []any{1,2,false}}`
would be a valid environment value.

In environments, no user specified Go structs are available, so as to keep the
definition relatively language independent.

### `oio`

All subtrees of a YAML document with one of the following tags

- `!eval`
- `!exec`
- `!file`
- `!tovalue`
- `!tostring`
- `!toint`
- `!b64enc`
- `!script`
- `!osenv`

will be subject to environment expansion, meaning the structure will be
traversed recursively and then every string will be expanded.

#### String Expansion

String expansion works by looking for `$[...]` or `.[...]`.  Wherever any such
expression is found, it is evaluating using [`expr-lang`](https://expr-lang.org)
against the environment.

The result of that evaluation is assumed is in the same type-form as described
above for environment type forms.

If the containing expression is `.[...]` with no additional leading or trailing
characters, that value is replaced for the node containing it.

Otherwise, the containing expression is interpolated for scalars in a standard
fashion, and non-scalars are taken to be the result of marshaling them using
json.

#### Escaping

To include a literal `]` character inside an expression, escape it with a backslash:

- `\]` → literal `]`
- `\\` → literal `\`

For example, to access a map key containing `]`:

```tony
!eval
result: $[data["key[0\]"]]
```

If the expression is not properly closed (no unescaped `]`), the text is treated
as a literal string, not an expression:

| Input | Output |
|-------|--------|
| `$[x]` | evaluates `x` |
| `$[x` | literal `$[x` |
| `$["a\]b"]` | evaluates to `a]b` |
| `$[x\]` | literal `$[x\]` (no closing bracket) |

For example given the environment

```tony
foo: true
```

and a node subject to environment evaluation

```tony
!eval
f:
- 1
- .[foo]
- "hello $[foo]"
- $[foo]
```

would result in

```tony
f:
- 1
- true
- "hello true"
- "true"
```

## eval tags

Object nodes using eval tags

trigger additional evaluation in `oio` mode and at the end of `dir` mode.
Handling these tags always occurs after environment evaluation, so the contents
can also contain `$[...]` and `.[...]` expansion.

### `!exec`

The `!exec` tag executes a program and provides its output as a string to
replace the node where it is found.

```tony
- !exec |
    ls /dev/null
- true
```

evaluates to

```tony
- /dev/null
- true
```

### `!file`

The `!file` tag replaces the node where it is found with the contents of a file
as a string

```tony
- !file /dev/null
- true
```

evaluates to

```tony
- ""
- true
```

The `!file` tag can also fetch HTTP and HTTPs URLs.

### `!script`

The `!script` should be associated with a string to be interpreted as an
expr-lang script provided with

1. The environment
1. Some helper functions to access various parts of the current document
- `whereami()` gives the yaml path to the node containing the script
- `getpath()` is a function which takes a yaml path and returns the result
  of evaluating it on the root of the document containing the script.
- `listpath()` is a function which takes a yaml path and returns the result
  of evaluating it on the root of the document containing the script.  The
  result is always a list, possibly empty or nil.

## Eval chains

Eval tags may be composed using `.` to generate chains of evaluations

To interpret a file or the output of a program as structured YAML, one composes
the tags

- `!tovalue.file`
- `!tovalue.exec`

To interpret an object file or the output of a program as binary data, it can be base64
encoded (with padding) and then treated as an object string

- `!b64enc.file`
- `!b64enc.exec`

To interpret a file as a script which returns a string which represents
the yaml to embed:

- `!tovalue.script.file` file.script

## Custom Tags

Custom tags can easily be created by implementing a simple interface and
registering the tag with `eval.Register`.
