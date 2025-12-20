# Tony Format: Design Rationale and Comparisons

## What Problem Does Tony Solve?

Most "better JSON" projects focus on syntax improvements — making data more pleasant to write. Tony takes a different approach: it makes **operations on data** (patch, match, diff) first-class citizens that use the same format as the data itself.

This is similar to Lisp's homoiconicity, where code and data share the same structure. In Tony, a patch is just a document with merge operation tags. A match query is just a document describing the shape you're looking for.

## Comparison with Other Formats

| Project | Innovation | Scope |
|---------|-----------|-------|
| JSON5, JSONC | Comments, trailing commas | Syntax |
| HJSON | Relaxed syntax, multi-line strings | Syntax |
| TOML | Flat structure, datetime literals | Syntax |
| StrictYAML | YAML without the footguns | Syntax |
| CUE | Validation + schemas + templating | Language |
| **Tony** | Operations as documents | Ecosystem |

Tony is closer to CUE or Dhall (formats with computational ambitions) than to JSON5 or HJSON (syntax polish). But unlike CUE, Tony isn't trying to be a logic language — it's a minimal substrate where match/diff/patch operations compose naturally.

## Key Differentiators

### 1. Operations as Documents

Patches, matches, and schemas are expressed in Tony format itself:

```yaml
# A patch is just a document with merge operation tags
patch:
  metadata:
    labels:
      environment: production
  spec:
    replicas: 3
    template:
      spec:
        containers:
          - resources: !delete  # Delete this field
```

No separate patch language to learn. `!delete`, `!insert`, `!replace` are just tags on regular document nodes.

### 2. Author-Controlled Merge Semantics

In Kubernetes, merge behavior is baked into Go struct tags that document authors cannot control:

```go
// Kubernetes source code decides how YOUR lists merge
type PodSpec struct {
    Containers []Container `json:"containers" patchStrategy:"merge" patchMergeKey:"name"`
}
```

In Tony, the document author controls merge semantics:

```yaml
# Author declares: merge this list by the "name" field
spec:
  containers: !key(name)
    - name: app
      image: myapp:v2
```

The `!key(name)` tag tells Tony to treat this array as a map keyed by `name`. The same mechanism works for any list, on any document — not just the fields Kubernetes decided to annotate.

### 3. Kinded Paths

The path syntax `.field[0]{sparse}` distinguishes between object access (`.`), array access (`[]`), and sparse map access (`{}`). This enables precise addressing that most JSON path implementations can't express.

### 4. Preservation of Metadata

Comments, formatting choices, and tag annotations survive round-trips through the IR. Edit a config programmatically without destroying human context.

### 5. Helm Chart Generation

Tony can generate Helm charts using merge key syntax for raw text injection:

```yaml
# Tony input
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  <<: '{{- if .Values.environment }}'
data:
  ENV: production
<<: '{{- end }}'
---
# Output with -x flag
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
{{- if .Values.environment }}
data:
  ENV: production
{{- end }}
```

This uses YAML 1.1's merge key syntax (`<<:`) repurposed for raw text output. Because Kustomize uses strategic merge patches (not templates), **Kustomize cannot produce Helm charts**. Tony can.

---

## `o build` vs Kustomize

Kustomize is the standard tool for Kubernetes manifest customization. Here's how Tony's `o build` compares:

### The Fundamental Difference

| Aspect | Kustomize | `o build` |
|--------|-----------|-----------|
| **Patch format** | Strategic merge patch OR JSON Patch | Tony document with `!delete`, `!key(name)` tags |
| **Targeting** | By resource name/kind/namespace | By structural `match:` (any field) |
| **Merge semantics** | Embedded in upstream Go structs | Author-controlled via tags |
| **Conditionals** | Separate overlay directories | `if:` field with expressions |
| **Substitution** | Rejected; use `replacements` | Native with `$[var]` syntax |
| **Helm generation** | Not possible | Supported via merge keys |
| **Domain** | Kubernetes-specific | General-purpose |

### Substitution: Kustomize's Intentional Gap

Kustomize maintainers explicitly reject templating. Their answer is `replacements`:

```yaml
# Kustomize: replacements (verbose, positional)
replacements:
  - source:
      kind: ConfigMap
      name: env-config
      fieldPath: data.DATABASE_URL
    targets:
      - select:
          kind: Deployment
        fieldPaths:
          - spec.template.spec.containers.[name=app].env.[name=DATABASE_URL].value
```

Tony uses simple variable expansion:

```yaml
# Tony build.tony
env:
  DATABASE_URL: !env DATABASE_URL

patches:
  - match: {kind: Deployment}
    patch:
      spec:
        template:
          spec:
            containers:
              - env:
                  - name: DATABASE_URL
                    value: $[DATABASE_URL]
```

### Conditional Patches: Directory Explosion vs Single File

Kustomize requires separate directories for each environment variant:

```
overlays/
  dev/
    kustomization.yaml
  staging/
    kustomization.yaml
  prod/
    kustomization.yaml
# 90% identical, diverging in subtle ways over time
```

Tony uses conditional patches in a single file:

```yaml
# Tony: conditionals with expressions
patches:
  - if: '.[ Env == "prod" ]'
    match: {kind: Deployment}
    patch: {spec: {replicas: 3}}

  - if: '.[ Env != "prod" ]'
    match: {kind: Deployment}
    patch: {spec: {replicas: 1}}
```

### ConfigMap Data Merging: The Killer Example

Given this ConfigMap:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  labels:
    app: myapp
data:
  LOG_LEVEL: info
  OLD_FEATURE: enabled
  DATABASE: postgres
```

**Kustomize** — needs TWO different patch mechanisms:
```yaml
# kustomization.yaml
patchesStrategicMerge:
  - configmap-merge.yaml   # Can add/change, can't delete

patchesJson6902:
  - target:
      version: v1
      kind: ConfigMap
      name: app-config     # Must know exact name!
    patch: |
      - op: remove
        path: /data/OLD_FEATURE

# configmap-merge.yaml (separate file)
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config         # Must repeat the name
data:
  LOG_LEVEL: debug         # Change
  NEW_KEY: new-value       # Add
```

**Tony** — one patch, structural matching:
```yaml
patches:
  - match:
      kind: ConfigMap
      metadata: {labels: {app: myapp}}  # Match by label, not name
    patch:
      data:
        LOG_LEVEL: debug      # Change
        NEW_KEY: new-value    # Add
        OLD_FEATURE: !delete  # Delete
```

### Who Controls Merge Behavior?

This is the fundamental philosophical difference:

**Kubernetes strategic merge:** The Go struct author (upstream Kubernetes developers) decides how lists merge. If they didn't add `patchMergeKey` to a field, you can't merge by key. If they chose the wrong key, you're stuck.

```go
// You cannot change this. Ever.
Volumes []Volume `patchStrategy:"merge" patchMergeKey:"name"`
```

**Tony:** The document author decides how lists merge, per-patch:

```yaml
# You control this. Always.
spec:
  volumes: !key(name)
    - name: config-volume
      configMap:
        name: my-config
```

The same flexibility applies to any list field, including ConfigMap `data` — which Kubernetes strategic merge treats as a map (merge by key) but won't let you delete keys without switching to JSON Patch.

### Real-World Example: Conditional Workload Configuration

From an actual Tony build file:

```yaml
# build.tony
build:
  env:
    cluster: minikube
    workloads:
      frontend: fork
      backend: local

  patches:
    - file: patches/workloads.tony

# patches/workloads.tony
- if: '.[ workloads.frontend == "fork" ]'
  match: null  # Apply to all documents
  patch:
    spec:
      forks: !key(name)
        - name: frontend
          forkOf:
            kind: Deployment
            namespace: hotrod-$[cluster]
            name: frontend

- if: '.[ workloads.frontend == "local" ]'
  match: null
  patch:
    spec:
      local: !key(name)
        - name: frontend
          from:
            kind: Deployment
            namespace: hotrod-$[cluster]
            name: frontend
          mappings:
            - port: 8080
              toLocal: ":9998"
```

Run with different configurations:
```bash
o build -e workloads.frontend=local -e workloads.backend=fork
```

Try doing this in Kustomize without external tooling.

---

## Honest Tradeoffs

**Use Kustomize if:**
- You're in a Kubernetes-only environment
- Your team already knows it
- Your customization needs are simple (name-based patches)
- You don't need to merge-patch ConfigMap data, delete list items, or use conditional logic
- You value ecosystem support over expressiveness

**Use Tony if:**
- You need conditional patches based on environment
- You want to match documents by structure, not just name
- You need author-controlled merge semantics (not upstream Go struct tags)
- You want to generate Helm charts programmatically
- You're tired of learning multiple patch formats
- You work with configuration beyond Kubernetes

**Use JSON5/HJSON if:**
- You just want comments in JSON
- Syntax polish is sufficient
