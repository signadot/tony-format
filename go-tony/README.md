# Tony Format

Go reference implementation of the Tony format and the `o` CLI tool for
working with structured data in Tony, YAML, and JSON.

## Go library

The module at `github.com/signadot/tony-format/go-tony` provides packages for
parsing, encoding, querying, matching, patching, and building documents in
Tony, YAML, and JSON.

| Package | Purpose |
|---------|---------|
| `ir` | Intermediate representation -- the core node tree |
| `parse` | Parse Tony/YAML/JSON into IR |
| `encode` | Encode IR back to Tony/YAML/JSON |
| `eval` | Evaluate `!eval`, `!exec`, `!file` tags |
| `mergeop` | Match and patch operations |
| `gomap` | Convert between IR and Go types |
| `dirbuild` | Programmatic access to the build system |
| `schema` | Schema definitions and validation |
| `stream` | Streaming document processing |

All document operations flow through the `ir.Node` tree. Parse any supported
format into IR, transform it with match/patch/eval, and encode it back out:

```go
node, _ := parse.Parse(data)           // Tony, YAML, or JSON
matched, _ := tony.Match(node, pattern)
patched, _ := tony.Patch(node, patch)
encode.Encode(patched, os.Stdout)       // back to any format
```

### Other tools

- **`tony-codegen`** -- generates Go marshaling code (`ToTonyIR`/`FromTonyIR`)
  and schema files from Go structs annotated with `tony:` struct tags.
- **`tony-lsp`** -- Language Server Protocol implementation for Tony format
  support in editors.

## The `o` CLI

`o` is a swiss-army knife for object notation files. It reads and writes Tony,
YAML, and JSON interchangeably and provides commands for viewing, querying,
matching, patching, diffing, evaluating, and building.

### Install

```sh
go install github.com/signadot/tony-format/go-tony/cmd/o@latest
```

### Global flags

| Flag | Description |
|------|-------------|
| `-o file` | Write output to file instead of stdout |
| `-I`, `-ifmt` | Input format: `tony/t`, `yaml/y`, `json/j` |
| `-O`, `-ofmt` | Output format: `tony/t`, `yaml/y`, `json/j` |
| `-b` | Encode with brackets |
| `-x` | Expand `<<:` merge fields during encoding |
| `-wire` | Compact output |

### view

Display files with syntax highlighting and tag rendering.

```sh
o view file.tony
o v -color file.yaml
cat file.json | o v
```

### eval

Evaluate documents containing `!eval`, `!exec`, `!file`, and other
transformation tags, with environment variable substitution.

```sh
o eval -e version=1.2.3 file.tony
o e -e debug=true file.yaml -- namespace=prod replicas=3
```

### get / list

Extract or query elements using path expressions.

```sh
o get '$.metadata.name' deployment.yaml
o list '$..containers[*].image' deployment.yaml
```

### match

Filter documents against match patterns. Supports tags like `!or`, `!and`,
`!glob`, `!not`, `!irtype`, `!has-path`, and more.

```sh
# inline pattern
o match -s '{ kind: Deployment }' manifests.yaml

# match secrets and configmaps from a pipeline
kustomize build . | o -y match -s '{ kind: !or [ConfigMap, Secret] }' -
helm template . | o -y match -s '{ kind: !or [ConfigMap, Secret] }' -

# match with trim
o match -trim -s '{ metadata: { name: null } }' manifests.yaml
```

Use `o match -tags` to list all available match tags.

### patch

Apply structured patches to documents. Supports merge-patch semantics with
tags like `!nullify`, `!insert`, `!delete`, `!replace`, `!json-patch`, and
array keying with `!key(field)`.

```sh
o patch -s '{ spec: { replicas: 5 } }' deployment.yaml
o patch -f patch.tony manifests.yaml
o patch -r -f patch.tony manifests.yaml   # reverse
```

Use `o patch -tags` to list all available patch tags.

### diff

Compute structured diffs between documents. Understands tags, strings, and
arrays.

```sh
o diff before.yaml after.yaml
o diff -r before.yaml after.yaml          # reverse
o diff -loop 'kubectl get pods -o yaml'   # watch for changes
```

### build

Build manifests from a directory containing a `build.tony` (or `.yaml`/`.json`)
configuration. Fetches documents from multiple sources, applies conditional
patches, and evaluates expressions--all driven by an environment that can be
overridden via flags, profiles, or OS environment variables.

```sh
o build                            # build from current directory
o build ./deploy                   # build from a specific directory
o build -l                         # list available profiles
o build -p staging                 # build with a profile
o build -s                         # show the resolved environment
o build -e namespace=prod          # override env vars
o build -- debug=false replicas=3  # env overrides after --
```

## Build System

A build directory contains a `build.{tony,yaml,json}` file describing sources,
patches, environment, and output configuration.

### Build file structure

```tony
build:
  env:
    namespace: default
    debug: true
    version: !exec ./version.sh

  sources:
  - dir: ./manifests
  - url: https://example.com/base.yaml
  - exec: helm template ./chart
    if: .[debug]

  patches:
  - match: { kind: Deployment }
    patch: { metadata: { namespace: .[namespace] } }
  - file: extra-patches.tony
    if: .[debug]

  output:
    destDir: ./out
    suffix: .yaml
    k8s:
      filenames: name-kind
```

### Sources

Each source entry fetches documents from one of:

- `dir:` -- recursively walk a directory for document files
- `url:` -- HTTP fetch
- `exec:` -- run a command, parse stdout

All source types accept an optional `format:` (auto-detected by default) and
`if:` conditional.

### Patches

Each patch entry has a `match:` pattern and a `patch:` to apply to matching
documents. Patches can also be loaded from external files with `file:`.
Both `match:` and `patch:` support the full set of match/patch tags. An `if:`
field makes patches conditional on the environment.

### Environment

The environment is set in four ways, in increasing precedence:

1. `env:` field in the build file
2. `-e path=value` flag
3. `-- key1=val1 key2=val2` arguments
4. `$TONY_DIRBUILD_ENV` OS environment variable (as a patch)

Use `o build -s` to inspect the resolved environment.

### Profiles

Place environment override files in a `profiles/` subdirectory. List them
with `o build -l` and apply with `o build -p <name>`. A profile patches the
base environment before the build runs.

### Output configuration

The `output` section controls where and how files are written:

```tony
output:
  destDir: ./out           # directory to write files (omit for stdout)
  suffix: .yaml            # file extension (omit to derive from format)
  filenames: auto          # filename strategy (default)
  k8s:
    filenames: name-kind   # k8s-specific pattern using tokens:
                           #   name, kind, namespace
                           # e.g. "name-kind" -> "my-app-deployment.yaml"
```

**Filename resolution priority:**

```
!filename(x) tag  >  output.k8s.filenames  >  output.filenames  >  auto
```

The `k8s.filenames` pattern is a dash-separated list of tokens. Each token
maps to a field on the Kubernetes object:

| Token | Source | Fallback |
|-------|--------|----------|
| `name` | `metadata.name` | `obj` |
| `kind` | `kind` (lowercased) | `unknown` |
| `namespace` | `metadata.namespace` | `default` |

For backward compatibility, the legacy top-level `destDir` and `suffix` fields
still work when `output` is absent.
