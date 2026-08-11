# Evaluation in Tony Format

A [companion article](https://dev.to/scott_cotton_dc9ce3e7e632/structured-matching-patching-and-diffing-done-right-22ci) describes how [Tony format](https://github.com/signadot/tony-format)'s matching, patching, and diffing operations all share the same IR and tag system. This article covers a fourth operation---evaluation---which uses the same tag mechanism to expand expressions, load files, execute commands, and thread an environment through a document tree.

The evaluation mechanism isn't fundamentally different from expansion systems in other tools. YAML's tag syntax (`!` prefix) is already used this way across the industry: CloudFormation's `!Ref`, `!Sub`, and `!GetAtt` wire resources together at deploy time; Home Assistant's `!include` and `!secret` split configuration across files; SpeechBrain's HyperPyYAML uses `!ref` for cross-references and `!new` to instantiate Python objects at parse time. Outside YAML-specific tools, the same pattern appears in Jinja2's template rendering, Helm's Go templates, and jsonnet's late binding---walk a tree or string, find markers, substitute values.

What makes Tony's version worth writing about is not novelty in the expansion mechanism itself, but how it fits into the same IR and tag system that drives match, patch, and diff. CloudFormation tags only wire resources. HyperPyYAML tags only construct Python objects. Tony's eval tags are symbols in the same registry as `!or`, `!key(name)`, and `!dive`, operating on the same `ir.Node` tree, composing with everything else.

## How it works

Evaluation is a post-order tree walk. The engine traverses the `ir.Node` tree depth-first, processing children before parents. At each node, if the node carries an eval tag (`!eval`, `!file`, `!exec`, `!script`, etc.), the engine looks up the corresponding symbol in the eval registry, instantiates an operation, and calls its `Eval` method:

```go
type Op interface {
    Eval(doc *ir.Node, env Env, f EvalFunc) (*ir.Node, error)
}
```

The `Env` is a `map[string]any` threaded through every call. The `EvalFunc` callback enables recursive delegation back to the engine, same pattern as `MatchFunc` and `PatchFunc` in the merge operations. New eval operations are added by registering a symbol---no changes to the engine.

## Expression syntax

Tony supports two expression forms in string values:

- **`.[expression]`** --- replaces the entire node. If the string is exactly `.[x]`, the node is replaced with whatever `x` evaluates to---a number, an object, an array, null. The result is typed, not stringified.

- **`$[expression]`** --- string interpolation. The expression result is converted to a string and spliced into the surrounding text. Multiple `$[...]` expressions can appear in a single string.

Expressions are evaluated with [expr-lang](https://expr-lang.org/), a Go expression evaluator. Variables come from the threaded environment:

```yaml
!eval
metadata:
  name: .[name]
  namespace: .[namespace]
  labels:
    version: v$[version]
```

With `env = {name: "frontend", namespace: "prod", version: "1.2.3"}`, this produces:

```yaml
metadata:
  name: frontend
  namespace: prod
  labels:
    version: v1.2.3
```

The distinction between `.[...]` and `$[...]` matters. `.[replicas]` where `replicas` is the integer 3 produces the number 3, not the string `"3"`. `$[replicas]` produces the string `"3"`. CloudFormation has a similar two-form split---`!Ref` for references, `!Sub` for string interpolation---but both produce strings in the end. Tony's `.[...]` produces typed IR nodes: objects, arrays, numbers, not just strings. This is what keeps evaluation type-safe---markers expand to structured data, not text.

## Script functions

When evaluating expressions, several built-in functions are available that operate on the document tree itself:

- **`whereami()`** --- returns the path of the current node (e.g. `$.metadata.name`)
- **`getpath(path)`** --- navigates to any node in the document by path
- **`listpath(path)`** --- returns multiple nodes matching a path pattern
- **`getenv(name)`** --- reads an OS environment variable

These functions give expressions access to the document's own structure. A value can reference another value elsewhere in the same tree:

```yaml
!eval
config:
  appName: my-app
  database:
    host: postgres.example.com
    port: 5432
deployment:
  name: '.[getpath("$.config.appName").String]'
  dsn: '.[getpath("$.config.database.host").String + ":" + string(getpath("$.config.database.port").Int64)]'
```

This is self-referential expansion: the document is both the template and the data source. HyperPyYAML's `!ref` tag does something similar---`!ref <key>` resolves a value from elsewhere in the same YAML file, with support for dotted paths and simple arithmetic. Tony's `getpath` navigates arbitrary paths through the typed IR tree, and because it returns an `ir.Node`, the result composes with further expressions.

## The eval tags

The built-in eval operations, all registered as symbols in the same registry:

| Tag | What it does |
|-----|-------------|
| `!eval` | Expand `.[...]` and `$[...]` expressions in the subtree |
| `!file` | Load content from a local file or HTTP URL |
| `!exec` | Run a shell command, capture stdout |
| `!script` | Evaluate an expr-lang program (with output mode: value, string, or any) |
| `!osenv` | Read OS environment variables |
| `!tostring` | Convert a node to its string representation |
| `!toint` | Convert to integer |
| `!tovalue` | Parse a YAML string into a typed node |
| `!b64enc` | Base64 encode |

`!file` and `!exec` are the I/O operations. `!file` loads a file path or URL that can itself contain `.[...]` references (expanded before loading). `!exec` runs a shell command and returns stdout as a string node. Both are simple---there's no caching, no retry logic, no abstraction layer. They do one thing. This is a deliberate trade-off against tools like Carvel's ytt, which is fully sandboxed (no filesystem, no network, no shell). Tony chooses to allow side effects and marks them: `!exec` and `!pipe` are flagged as unsafe operations that callers can opt out of.

`!script` provides full expr-lang evaluation with configurable output. With `!script(value)`, the result string is parsed as YAML back into IR. With `!script(string)`, it stays a string. With `!script(any)`, the raw expr-lang result is converted directly to an IR node.

## Tag composition

Tags compose via dot-separation. `!tovalue.file` chains two operations on a single node: `!file` loads the file, `!tovalue` parses the result as YAML. `!not.or` negates a disjunction. `!subtree.type` searches depth-first for nodes of a given type. The engine peels off the first tag, executes it, and passes the rest of the chain to the child---a pipeline that reads right to left. Tags also carry arguments inline: `!key(name)`, `!script(value)`, `!delete(!mytag)`. Other tools that use YAML tags---CloudFormation, HyperPyYAML, authentik blueprints---achieve composition by nesting child nodes: variable maps, extra arrays, separate mapping entries. The YAML spec assigns one tag per node, so stacking tags means adding structure. Tony's dot-separated tags put multiple operations on a *single* node's tag string. The result: document structure preservation. The operations ride on the tag; they don't reshape the data to accommodate themselves.

```yaml
# Load a YAML file and merge it into the current object
<<: !tovalue.file config/cluster.yaml

# Run a command, parse its stdout as a typed value
from: !tovalue.exec "echo 42"
```

## Composition with `o build`

The build system is where evaluation becomes practical. A `build.tony` file declares sources, patches, an environment, and output configuration. The environment is where `!eval`, `!exec`, and `!file` do their work:

```tony
build:
  env:
    namespace: default
    version: !exec ./version.sh
    debug: true

  sources:
  - dir: ./manifests

  patches:
  - match: { kind: Deployment }
    patch: { metadata: { namespace: .[namespace] } }
  - file: extra-patches.tony
    if: .[debug]
```

The environment is itself evaluated---`!exec ./version.sh` runs at build time and its stdout becomes the value of `version`. Then every `.[namespace]` and `.[debug]` reference in patches expands against that resolved environment. Patches are conditional: `if: .[debug]` means the patch only applies when `debug` is truthy.

Environment values are set with increasing precedence:

1. `env:` field in the build file
2. `-e key=value` flags
3. `-- key1=val1 key2=val2` arguments
4. `$TONY_DIRBUILD_ENV` OS environment variable

Profiles---override files in a `profiles/` subdirectory---patch the base environment before the build runs. `o build -p staging` applies `profiles/staging.{tony,yaml,json}` on top of the base env.

## Use case: keeping configuration DRY

The typical use case is a user organising their configuration to avoid repetition. A Kubernetes deployment directory might have dozens of manifests that share the same namespace, image registry, and version string. Without evaluation, you either duplicate these values everywhere or reach for a templating engine.

With Tony evaluation, you declare the shared values once in the environment and reference them:

```tony
build:
  env:
    registry: us-docker.pkg.dev/myproject/myrepo
    version: !exec "git describe --tags --always"
    namespace: production

  sources:
  - dir: ./k8s

  patches:
  - match: { kind: !or [Deployment, StatefulSet, DaemonSet] }
    patch:
      metadata:
        namespace: .[namespace]
      spec:
        template:
          spec:
            containers: !key(name)
            - name: app
              image: $[registry]/app:$[version]
```

The version comes from git at build time. The namespace is a plain string in the environment. The image reference is string-interpolated from multiple variables. Switch to staging by running `o build -p staging` or `o build -- namespace=staging`.

This is DRY without a template language. The patches are still valid Tony/YAML documents. You can diff them, match against them, or compose them with other patches. A Helm chart is text that happens to produce YAML. A Tony build file is structured data that stays structured data throughout.

## Use case: generating config test fixtures

The other direction is a software provider who needs to generate large volumes of configuration examples for testing. Instead of maintaining hundreds of hand-written YAML files, you define a base document and use evaluation to produce variants:

```tony
build:
  env:
    test_name: basic
    replicas: 1
    image_tag: latest
    enable_monitoring: false

  sources:
  - dir: ./templates

  patches:
  - match: { kind: Deployment }
    patch:
      metadata:
        name: test-$[test_name]
        labels:
          test-case: $[test_name]
      spec:
        replicas: .[replicas]
  - match: { kind: Deployment }
    patch:
      spec:
        template:
          spec:
            containers: !key(name)
            - name: app
              image: myapp:$[image_tag]
  - match: { kind: ServiceMonitor }
    if: .[enable_monitoring]
    patch:
      metadata:
        name: test-$[test_name]-monitor
```

Run `o build -- test_name=ha replicas=3 enable_monitoring=true` to produce an HA variant. Run it in a loop with different parameters to generate a test matrix. Each generated output is valid, diffable, matchable YAML. You can diff two test variants structurally to verify exactly what changed. You can match the output against validation patterns to catch regressions.

Because evaluation composes with the other operations, the test infrastructure is the same tool as the production build infrastructure. Generate a fixture, diff it against a baseline, match it against a schema---all with `o`.

## Not a template engine

It's worth being explicit about what this is not. Tony evaluation is not string interpolation bolted onto YAML. It operates on the IR tree. An expression that evaluates to a number produces a number node, not a string that looks like a number. An expression that evaluates to an object produces an object node with fields and values, not a string containing YAML.

Helm templates YAML as text---a Go template emits characters that happen to form valid YAML, and if your indentation is wrong, you get a parse error at a distance. Jinja2 has the same problem. Carvel's ytt avoids this by operating on YAML structure, but uses comment annotations rather than YAML's own tag mechanism, and its Starlark-based logic lives in a separate layer from the data. jsonnet solves the typing problem---its expressions produce typed values---but it's a separate language, not YAML.

Tony's eval tags are none of these things. They're processed the same way as `!or` or `!key(name)`---a symbol in a registry, receiving an IR node, returning an IR node. The engine doesn't know or care whether a tag means "match this pattern" or "expand this expression." Everything stays in the typed IR. Evaluation doesn't break the invariant that makes match, patch, and diff work. It's all tree operations on the same tree.
