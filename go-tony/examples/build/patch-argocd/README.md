# ArgoCD Tony Build Plugin

This example patches an ArgoCD installation to add support for Tony-based
manifest generation via a Config Management Plugin (CMP).

## Quick Start

Patch your ArgoCD installation:

```bash
o build examples/build/patch-argocd -y | kubectl apply -f -
```

This will:
1. Create a ConfigMap with the plugin configuration
2. Patch the repo-server deployment to add a Tony sidecar

## Usage

### Basic Installation

```bash
o build examples/build/patch-argocd -y | kubectl apply -f -
```

### Custom Namespace

```bash
o build examples/build/patch-argocd -y -e namespace=myns | kubectl apply -f -
```

### With an ArgoCD Application

```bash
o build examples/build/patch-argocd -y \
  -e app.name=my-app \
  -e app.repoURL=https://github.com/org/repo \
  -e app.path=deploy \
  -e app.project=default \
  -e app.revision=HEAD \
  -e app.destNamespace=my-ns \
  | kubectl apply -f -
```

## Using the Plugin

Configure your ArgoCD Application to use the Tony plugin:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  source:
    repoURL: https://github.com/org/repo
    path: deploy
    plugin:
      name: tony-build-v1.0
      env:
      - name: ARGS
        value: "-p production"
```

The `ARGS` environment variable is passed to `o build`, so you can specify
profiles and other options.

## Per-Deploy Image Tags with ORAS

For CI/CD workflows where you want to deploy specific image versions without
committing to git, you can use ORAS to store image tags as OCI artifacts.

### CI/CD Pipeline (e.g., GitHub Actions)

Push image tags after building:

```bash
# Generate image tags JSON (must include env: wrapper for profile format)
cat > image-tags.json << EOF
{"env": {"images": {"tags": {"app": "sha-abc123", "worker": "sha-def456"}}}}
EOF

# Push as OCI artifact
oras push ghcr.io/org/deploy-config:$COMMIT_SHA image-tags.json:application/json
```

### Custom Plugin Configuration

Create a plugin that pulls image tags at sync time:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ConfigManagementPlugin
metadata:
  name: tony-build-with-images
spec:
  version: v1.0
  generate:
    command:
    - sh
    - -c
    - |
      set -e
      oras pull ghcr.io/org/deploy-config:$ARGOCD_APP_REVISION -o /tmp
      cat /tmp/image-tags.json | o build -y $ARGOCD_ENV_ARGS .
```

### Application Configuration

Configure the Application to read the image tags from stdin using `-p -`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  source:
    repoURL: https://github.com/org/repo
    path: deploy
    plugin:
      name: tony-build-with-images-v1.0
      env:
      - name: ARGS
        value: "-p production -p -"
```

The `-p -` tells `o build` to read an additional profile from stdin, which
is where the image tags JSON is piped.

### Benefits

- **No git commits for image updates** - Image tags are stored in the registry
- **Atomic deploys** - All image tags for a commit are deployed together
- **Automatic retries** - If the artifact isn't ready yet, ArgoCD retries
- **Minimal storage** - ORAS artifacts are just JSON blobs, not full images

### Timing Considerations

When a commit is pushed, ArgoCD may sync before CI/CD has finished pushing
the image tags artifact. With `set -e`, the plugin fails and ArgoCD retries
with backoff until the artifact exists. This ensures you never deploy with
stale or default image tags.
