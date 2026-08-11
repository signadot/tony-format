---
title: "How to Navigate ArgoCD's Manifest Friction with `o build`"
published: false
description: "ArgoCD makes it surprisingly hard to supply manifests and update images outside Helm/Kustomize. Here's how o build and ORAS cut through the friction."
tags: kubernetes, argocd, gitops, devops
---

## ArgoCD's Blind Spot

ArgoCD has one job: take Kubernetes manifests and apply them to a cluster. It
does this well. It watches a git repo, detects drift, syncs state, shows you
diffs. What it doesn't do well — surprisingly — is accept manifests.

Out of the box, ArgoCD understands three things: plain YAML directories, Helm
charts, and Kustomize overlays. Anything else requires a Config Management
Plugin (CMP), which means building a container image, writing a plugin spec,
and mounting it as a sidecar on the repo-server.

That's already more friction than you'd expect for a tool whose entire purpose
is "take YAML, apply it." But the real problem goes deeper.

### argocd-image-updater Doesn't Work with Plugins

The most common operation in a deployment pipeline is updating an image tag.
ArgoCD's answer to this is
[argocd-image-updater](https://argocd-image-updater.readthedocs.io/), a
separate controller that watches registries and writes back image tags. It
works with Helm (by updating `values.yaml` parameters) and Kustomize (by
updating `kustomization.yaml`). It does **not** work with Config Management
Plugins.

This means if you use a CMP — whether it's jsonnet, cue, dhall, or anything
else — you're on your own for image updates. ArgoCD's manifest pipeline and
its image-update pipeline are two systems that don't talk to each other unless
you happen to use one of the two blessed tools.

So a tool built to apply manifests makes it hard to supply manifests, and its
image updater only works with specific manifest generators. This is the
landscape.

## What `o build` Does

Tony format is a data format where patches, queries, and schemas are expressed
as documents themselves — combining JSON's structure with YAML's readability.
(For background on Tony's core operations, see
[Structured Matching, Patching, and Diffing Done Right](https://dev.to/scott_cotton_dc9ce3e7e632/structured-matching-patching-and-diffing-done-right-22ci).)

Its CLI tool `o` includes a build system driven by a `build.tony` file:

```tony
build:
  env:
    namespace: default
    replicas: 3

  sources:
  - dir: ./manifests

  patches:
  - match: { kind: Deployment }
    patch:
      metadata:
        namespace: .[namespace]
      spec:
        replicas: .[replicas]
```

Sources fetch documents from directories, URLs, or command output. Patches
are applied conditionally using match patterns. The `.[varname]` expressions
perform typed substitution — not string interpolation, but structural
replacement within the IR tree. (The
[evaluation system](https://dev.to/scott_cotton_dc9ce3e7e632/evaluation-in-tony-format-4cnl)
covers how these expressions compose with `!eval`, `!exec`, `!file`, and other
tags.) Profiles in a `profiles/` subdirectory override the environment per
deployment target.

The key idea: patches are documents with the same structure as the data they
operate on. No templating language. `!key(name)` lets you merge arrays by a
field — something Kustomize can only do for hardcoded Kubernetes merge keys,
but Tony lets you do for any array.

```bash
o build -y                      # output as YAML
o build -y -p staging           # with a profile
o build -y -e namespace=prod    # override a single variable
```

If you also need to produce Helm charts from the same source manifests,
`o build` handles that too — see
[One Source, Two Outputs](https://dev.to/scott_cotton_dc9ce3e7e632/one-source-two-outputs-generating-k8s-yaml-and-helm-charts-with-tony-format-30h6)
for how Tony generates both K8s YAML and Helm charts from a single build
directory.

## Solving Image Updates Without argocd-image-updater

Since argocd-image-updater doesn't work with plugins, we need another way
to get per-commit image tags into the build. The approach: store image tags
as OCI artifacts using [ORAS](https://oras.land) and pull them at sync time.

### CI Pushes Image Tags

After building images, CI publishes a small JSON artifact to the registry:

```bash
cat > image-tags.json << 'EOF'
{"env": {"images": {"tags": {"app": "sha-abc123", "worker": "sha-def456"}}}}
EOF

oras push ghcr.io/org/deploy-manifest:$COMMIT_SHA image-tags.json
```

The `env:` wrapper makes this a valid `o build` profile — it merges into the
build environment naturally.

### The Plugin Pulls at Sync Time

The CMP's generate command pulls the artifact for the current commit and
pipes it to `o build`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ConfigManagementPlugin
metadata:
  name: tony-build
spec:
  version: v1.0
  generate:
    command:
    - sh
    - -c
    - |
      if oras pull ghcr.io/org/deploy-manifest:$ARGOCD_APP_REVISION -o /tmp 2>/dev/null; then
        cat /tmp/image-tags.json | o build -y $ARGOCD_ENV_ARGS .
      else
        echo "manifest not available for $ARGOCD_APP_REVISION, scheduling retry" >&2
        ARGOCD_TOKEN=$(cat /argocd-api-token/token)
        (sleep 60 && wget -qO- \
          --header="Authorization: Bearer $ARGOCD_TOKEN" \
          "http://argocd-server.argocd.svc/api/v1/applications/$ARGOCD_APP_NAME?refresh=hard" \
          > /dev/null 2>&1) &
        exit 1
      fi
```

### The Timing Problem

When a commit is pushed, two things happen in parallel: CI starts building
images, and ArgoCD notices the new commit and tries to sync. ArgoCD is usually
faster. The `oras pull` fails because the artifact doesn't exist yet.

ArgoCD will sometimes retry manifest generation errors on its own, but this
isn't reliable — the repo-server can cache the error and stop trying. So the
plugin handles retries explicitly: on failure, it backgrounds a process that
waits 60 seconds and triggers a hard refresh via the ArgoCD API. The hard
refresh clears the cached error and ArgoCD re-invokes the plugin. This
repeats until the artifact appears.

It's a workaround for another ArgoCD gap — no built-in way for a plugin to
say "not ready yet, try again soon." But it works.

### What This Gets You

- **No git commits for image updates** — tags live in the registry alongside
  the images themselves
- **No argocd-image-updater** — the plugin handles everything, for any
  manifest generator
- **Atomic deploys** — all image tags for a commit ship as one artifact
- **Decoupled CI** — CI just pushes OCI artifacts, needs no ArgoCD access

## The Application

Here's what an ArgoCD Application looks like using `o build`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-operator
  namespace: argocd
spec:
  source:
    repoURL: https://github.com/org/repo.git
    targetRevision: HEAD
    path: deploy/operator
    plugin:
      name: tony-build-v1.0
      env:
      - name: ARGS
        value: "-p staging -p -"
  destination:
    server: https://kubernetes.default.svc
  syncPolicy:
    syncOptions:
    - ApplyOutOfSyncOnly=true
```

- **`plugin.name: tony-build-v1.0`** — tells ArgoCD to use our CMP instead of
  Helm or Kustomize
- **`env.ARGS: "-p staging -p -"`** — passes flags to `o build`: apply the
  `staging` profile, then overlay image tags from stdin via `-p -`. We place
  these args in the Application to be able to customize them without updating
  the plugin. We may want to add `-e serviceMesh=istio` for example.

The `deploy/operator` directory in that repo contains a `build.tony` file.
ArgoCD clones the repo, hands the directory to the plugin, and the plugin runs
`o build` to produce YAML on stdout. Whatever comes out is what ArgoCD applies.
Different Applications use different profiles from the same build directory.

## Installing the Plugin

A CMP runs as a sidecar on the `argocd-repo-server` deployment. You need:

1. A ConfigMap with the plugin spec
2. A sidecar container with `o` and `oras`
3. Registry credentials for pulling artifacts
4. An ArgoCD API token for the retry mechanism

The sidecar looks like this:

```yaml
containers:
- name: tony-build
  image: ghcr.io/signadot/tony-plugin:latest
  command: [/var/run/argocd/argocd-cmp-server]
  securityContext:
    runAsNonRoot: true
    runAsUser: 999
  volumeMounts:
  - name: var-files
    mountPath: /var/run/argocd
  - name: plugins
    mountPath: /home/argocd/cmp-server/plugins
  - name: tony-plugin-config
    mountPath: /home/argocd/cmp-server/config/plugin.yaml
    subPath: plugin.yaml
  - name: tmp
    mountPath: /tmp
  - name: ghcr-creds
    mountPath: /ghcr-creds
  - name: argocd-api-token
    mountPath: /argocd-api-token
```

### Patching ArgoCD with `o build`

Rather than hand-editing the repo-server Deployment, you can use `o build`
itself to do the patching. The tony-format repo includes an
[example](https://github.com/signadot/tony-format/tree/main/go-tony/examples/build/patch-argocd)
that fetches the live Deployment from the cluster and patches it:

```tony
build:
  sources:
  - exec: "kubectl get deployment argocd-repo-server -n .[namespace] -o yaml"
    format: yaml
  - dir: source

  env:
    namespace: argocd
    image: ghcr.io/signadot/tony:latest
    pluginName: tony-build

  patches:
  - file: patches/cleanup.tony
  - file: patches/namespace.tony
  - file: patches/repo-server.tony
  - file: patches/application.tony
```

The `repo-server.tony` patch uses `!key(name)` to
[merge arrays by field](https://dev.to/scott_cotton_dc9ce3e7e632/structured-matching-patching-and-diffing-done-right-22ci)
— adding the sidecar container without knowing or caring what else is in the
containers array:

```tony
- match:
    kind: Deployment
    metadata:
      name: argocd-repo-server
  patch:
    spec:
      template:
        spec:
          containers: !key(name)
          - name: tony-build
            image: .[image]
            command: [/var/run/argocd/argocd-cmp-server]
            securityContext:
              runAsNonRoot: true
              runAsUser: 999
            volumeMounts:
            - name: var-files
              mountPath: /var/run/argocd
            - name: plugins
              mountPath: /home/argocd/cmp-server/plugins
            - name: tony-plugin-config
              mountPath: /home/argocd/cmp-server/config/plugin.yaml
              subPath: plugin.yaml
            - name: tmp
              mountPath: /tmp
          volumes: !key(name)
          - name: tony-plugin-config
            configMap:
              name: tony-build-plugin
```

One command:

```bash
o build examples/build/patch-argocd -y | kubectl apply -f -
```

## Wrapping Up

ArgoCD is good at what it does — reconciling cluster state against a desired
state. But it makes assumptions about how that desired state is produced, and
those assumptions break down as soon as you step outside Helm or Kustomize.
The plugin system exists but is a second-class citizen. The image updater
ignores it entirely.

`o build` as a CMP gives you a manifest generator that works in
[structured documents](https://dev.to/scott_cotton_dc9ce3e7e632/structured-matching-patching-and-diffing-done-right-22ci)
rather than templates, with conditional patches,
[composable evaluation](https://dev.to/scott_cotton_dc9ce3e7e632/evaluation-in-tony-format-4cnl),
environment profiles, and multi-source builds. The ORAS pattern gives you
per-commit image updates without git-commit noise and without depending on
argocd-image-updater.

It shouldn't require this much machinery to give a manifest-applier some
manifests. But given ArgoCD's constraints, this is a functionally complete
way through without common drawbacks like storing non-source generated
manifests in git.

---

Install `o`:

```bash
go install github.com/signadot/tony-format/go-tony/cmd/o@latest
```

Full example: [`examples/build/patch-argocd`](https://github.com/signadot/tony-format/tree/main/go-tony/examples/build/patch-argocd)

*[Tony format](https://github.com/signadot/tony-format) is open source.*
