---
title: "One Source, Two Outputs: Generating K8s YAML and Helm Charts with Tony Format"
published: true
date: 2026-01-08
tags: automation, kubernetes, tooling
canonical_url: https://dev.to/scott_cotton_dc9ce3e7e632/one-source-two-outputs-generating-k8s-yaml-and-helm-charts-with-tony-format-30h6
---

We use controller-gen to generate CRDs and base manifests. These YAML files are our source of truth—used in CI, deployed to dev clusters like minikube, and tested internally. But customers want Helm charts.

This creates an uncomfortable constraint: **Helm cannot be our source of truth** because controller-gen outputs YAML. We need to generate Helm charts *from* YAML, not the other way around.

## The Impedance Mismatch

Helm has a fundamental problem: you're editing YAML, but you're not.

```yaml
containers:
- name: manager
  image: {{ .Values.image.repository }}:{{ .Values.image.tag }}
  resources:
    {{- if .Values.resources }}
    {{ .Values.resources | toYaml | nindent 4 }}
    {{- end }}

```

This isn't valid YAML. Your IDE doesn't understand it. YAML validators reject it. Kustomize can't patch it. Every tool in the Kubernetes ecosystem expects YAML, but Helm templates are a hybrid format that nothing else can process.

This means you can't use kustomize to customize Helm charts. You can't use Helm as a source and generate plain YAML with structural patches. You're stuck maintaining two parallel worlds.

## Our Old Workflow: Death by a Thousand Hacks

Our previous solution was a pipeline of custom tools:

```makefile
kustomize build deploy/operator/overlays/helm \\
  | go run ./hack/fix-cm \\
  | go run ./hack/templatize.go \\
    -charts-dir $(CHARTS_DIR) \\
    -templates-dir templates

```

Each piece solved a problem that shouldn't exist.

### hack/fix-cm.go: Working Around Kustomize's ConfigMap Limitations

Kustomize can't patch individual fields inside ConfigMap `data` values. Our workaround: store structured data in ConfigMaps as actual YAML structures (invalid for a real ConfigMap), let kustomize patch them, then convert back to strings:

```go
// Input: invalid ConfigMap with structured data field
// data:
//   config:
//     x: xy
//     f: v
//
// Output: valid ConfigMap with string field
// data:
//   config: |
//     x: xy
//     f: v

```

A 100-line Go program to work around a tool limitation.

### hack/templatize.go: 450 Lines of Regex

Since kustomize rejects invalid YAML, we couldn't put Helm templates in our source files. Instead, we invented a shadow language of annotations:

```yaml
# In kustomize overlay
metadata:
  labels:
    templatize.signadot.com/range-map: .Values.commonLabels
  annotations:
    templatize.signadot.com/include-if: not .Values.disableAgent

```

Then `templatize.go` scans the output line-by-line with regex, looking for these annotations and rewriting them into Helm templates:

```go
directiveExp = regexp.MustCompile(`^(\\s*)templatize\\.signadot\\.com/([^:]+):\\s*(.+)$`)
signadotImageExp = regexp.MustCompile(`^(\\s*-?\\s*)(image|value):\\s*signadot/([a-z-]+):(.+)$`)
helmValuesExp = regexp.MustCompile(`^(.*)helm-values.signadot.com/([a-zA-Z.][a-zA-Z0-9_.]*)(\\?(.*))?$`)

```

Every image reference, every conditional include, every values injection—all handled by pattern matching on text that happens to look like YAML.

This almost worked. Every change required understanding both the kustomize overlay system *and* the templatize conventions. Adding a new configurable field meant editing multiple files and hoping the regex would match correctly.

But the real killer was what happened when regex couldn't express a transformation. The escape hatch: **git unified diffs stored as source code**, applied against generated output:

```diff
diff --git b/templates/agent-deployment.yaml a/templates/agent-deployment.yaml
@@ -31,6 +35,15 @@ spec:
         {{- range $key, $val := .Values.podAnnotations }}
         {{ $key | quote }}: {{ $val | quote }}
         {{- end }}
+        {{- if $linkerdEnabled }}
+        {{- range $key, $val := .Values.linkerd.operator.podAnnotations }}
+        {{ $key | quote }}: {{ $val | quote }}
+        {{- end }}
+        {{- end }}

```

These patches target files that don't exist—they're generated mid-pipeline. If templatize output shifts by one line (a new annotation, a reordered field), the patch context no longer matches and the build fails. You're debugging line number mismatches in diffs against fictional files.

## Tony's Solution: Valid Documents All the Way Down

Tony format solves this by letting you embed raw text in structurally valid documents using merge keys (`<<:`):

```yaml
resources:
  <<: '{{- if .Values.resources }}'
  <<: '{{ .Values.resources | toYaml | nindent 4 }}'
  <<: '{{- else }}'
  limits:
    memory: "512Mi"
  <<: '{{- end }}'

```

This is valid Tony. It parses. It patches. And when you run `o build` with the expand merge keys option `-x`, the merge keys expand to raw text at the correct indentation:

```yaml
resources:
{{- if .Values.resources }}
{{ .Values.resources | toYaml | nindent 4 }}
{{- else }}
  limits:
    memory: "512Mi"
{{- end }}

```

For simple value substitution, `!literal` preserves template syntax:

```yaml
image: !literal '{{ .Values.image.repository }}:{{ .Values.image.tag }}'

```

This guarantees that the output ends up like this

```yaml
image: |-
  {{ .Values.image.repository }}:{{ .Values.image.tag }}

```

preserving the quoting conventions inside templates which are needed for Helm.

### The New Workflow

```
deploy/
  sdctl-operator/
    build.tony              # Internal deployment build
    source/                 # Deployments, services
    crd/bases/              # controller-gen output
  charts/sdctl-operator/
    build.tony              # Helm chart build
    patches/
      images.tony           # Templatize images
      resources.tony        # Templatize resources

```

The Helm chart build sources from the internal deployment:

```yaml
build:
  destDir: ../../../dist/charts/signadot/sdctl-operator/templates

  sources:
  - exec: "o b ../../sdctl-operator"  # Reuse the same base
    format: yaml

  patches:
  - file: patches/images.tony
  - file: patches/resources.tony

```

The image patch uses `!literal` and `!key(name)` for structural merging:

```yaml
- match:
    kind: Deployment
    metadata:
      name: sdctl-controller-manager
  patch:
    spec:
      template:
        spec:
          containers: !key(name)
          - name: manager
            image: !literal '{{ include "valuesDefault" (list .Values "signadot/sdctl-controller-manager:latest" "controllerManager" "image") }}'

```

CI becomes straightforward:

```bash
# Deploy to minikube
o build deploy/sdctl-operator/ | kubectl apply -f -

# Build Helm chart for customers
o build -x deploy/charts/sdctl-operator/

```

controller-gen stays the source of truth. No regex. No annotation hacks. No ConfigMap fixups.

## Why This Actually Works

### Generic Tools Beat Domain-Specific Ones

Kustomize's philosophy is that it *understands* Kubernetes. It knows that `containers` merges by `name` because the upstream Go struct has `patchMergeKey:"name"`. This sounds helpful until you realize:

- You can't change merge behavior for fields Kubernetes got wrong
- You can't use kustomize on anything that isn't a K8s resource
- Every K8s API change potentially changes your patch semantics

Tony takes the opposite approach: it knows nothing about Kubernetes. It's a generic tool for structured data that happens to work beautifully on K8s manifests.

```yaml
containers: !key(name)  # You declare merge semantics, not upstream Go structs

```

This works on any array, in any document. Patch your Helm `values.yaml`. Patch CI configs. Patch Terraform variables. The same tool, the same syntax, the same mental model—without being locked into one ecosystem's idea of how merging should work.

### Structural Matching

Kustomize requires you to know resource names upfront. Tony matches by structure:

```yaml
- match:
    kind: Deployment
    metadata:
      labels:
        app: myapp
  patch:
    spec:
      replicas: 3

```

Match expressions compose with `!or`, `!and`, and `!not`. Apply a patch to all ConfigMaps and Secrets:

```yaml
- match:
    kind: !or [ConfigMap, Secret]
  patch:
    metadata:
      labels:
        managed-by: tony

```

### Conditionals Without Directory Explosion

Kustomize needs separate overlay directories for each variant. Tony uses expressions,
over a substitution environment similar to Helm's values.yaml:

```yaml
- if: '.[ environment == "prod" ]'
  match: { kind: Deployment }
  patch: { spec: { replicas: 5 } }

```

Moreover Tony has profiles which are patches to the substition environment.

## Looking Forward

Migrating to Tony gave us something we didn't have before: a coherent pipeline we actually understand. No more debugging regex transforms or tracing through three layers of kustomize overlays to figure out why an annotation didn't convert properly, or constructing text patches against fictional targets as source code.

But this is just the start. The `o build` model—declarative builds with sources, patches, and environment overrides—opens up possibilities we're actively exploring:

**Remote builds.** Today `o build` runs locally. We're adding support for remote sources and build execution, so your `build.tony` can reference artifacts from CI, pull base manifests from registries, or execute builds in controlled environments. The same declarative format, but with the dependency resolution happening across network boundaries.

The goal: treat Kubernetes manifests with the same rigor we treat code. Reproducible builds from declared inputs, whether those inputs are local files, controller-gen output, or artifacts from a release pipeline.

If you're fighting similar battles with Helm generation, give Tony a look: [github.com/signadot/tony-format](https://github.com/signadot/tony-format)
