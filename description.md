# -t build: comments in env block cause values to become null

## Description

When using comments inside the `env:` block in a `build.tony` file, the values after comments become null during evaluation.

## Reproduction

```tony
build:
  sources:
  - dir: source

  env:
    # ArgoCD namespace
    namespace: argocd

    # Tony container image  
    image: ghcr.io/signadot/tony:latest
```

Running `o build . -y` produces output with `namespace: null` and `image: null`.

## Workaround

Remove comments from the env block:

```tony
build:
  sources:
  - dir: source

  env:
    namespace: argocd
    image: ghcr.io/signadot/tony:latest
```

## Expected behavior

Comments should be preserved (visible via `o build -s`) without affecting value resolution.

## Found while

Creating the `examples/build/patch-argocd` example for ArgoCD integration.