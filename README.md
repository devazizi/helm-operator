# Helm Operator

Helm Operator is a small Kubernetes operator that manages Helm repositories and
Helm releases through Kubernetes custom resources. Instead of running `helm repo
add` and `helm upgrade --install` manually, you create `Repository` and
`DeployChart` objects and let the operator reconcile them.

> [!IMPORTANT]
> This project is currently an early-stage implementation. Review the
> [limitations and security notes](#limitations-and-security-notes) before using
> it in a production cluster.

## Features

- Add public Helm repositories.
- Add repositories protected by a username and password.
- Install a specific version of a Helm chart.
- Pass arbitrary Helm values through a Kubernetes resource.
- Upgrade a release when its `DeployChart` specification changes.
- Uninstall a release when its `DeployChart` resource is deleted.
- Report basic reconciliation state in the resource status.
- Run multiple operator replicas safely through controller-runtime leader
  election.
- Reflect Secrets and ConfigMaps into namespaces selected by wildcard patterns.
- Build and publish a container image when a semantic version Git tag is pushed.

## How it works

The operator watches Helm resources in the `helm.k8s.ir/v1alpha1` API group and
a `Reflector` resource in `others.helm.k8s.ir/v1alpha1`:

1. A cluster-scoped `Repository` runs `helm repo add` and `helm repo update`
   inside the operator container.
2. A namespaced `DeployChart` writes `spec.values` to a temporary YAML file and
   runs `helm upgrade --install`.
3. The Kubernetes resource name becomes the Helm release name, and the resource
   namespace becomes the Helm release namespace.
4. The operator hashes the complete `DeployChart` specification. A changed hash
   causes another Helm upgrade.
5. Deleting a `DeployChart` causes the operator to run `helm uninstall` for the
   corresponding release.
6. A namespaced `Reflector` copies a Secret or ConfigMap with the same name and
   namespace into namespaces selected by shell-style patterns.

The operator uses the Helm CLI rather than the Helm Go SDK. The runtime image
therefore includes `helm`, `kubectl`, and the compiled Go controller.

## Requirements

- A Kubernetes cluster
- `kubectl` configured for that cluster
- Helm 3, when running the operator outside its container
- Docker or another OCI builder, when building the image locally
- Go 1.24.5 or newer, when developing locally

## Installation

### 1. Install the custom resource definitions

```bash
kubectl apply -f crd/repo.crd.yaml
kubectl apply -f crd/deploy.helm.yaml
kubectl apply -f crd/reflector.crd.yaml
```

Confirm that all three CRDs exist:

```bash
kubectl get crd repositories.helm.k8s.ir deploys.helm.k8s.ir reflectors.others.helm.k8s.ir
```

### 2. Deploy the operator

```bash
kubectl apply -f deploy/first-deploy.yaml
```

This manifest creates:

- The `helm-operator` namespace
- A service account for the operator
- A `ClusterRoleBinding` granting that account `cluster-admin`
- A two-replica operator deployment

Check the deployment and logs:

```bash
kubectl get pods -n helm-operator
kubectl logs -n helm-operator deployment/helm-operator-deployment --follow
```

The included deployment manifest currently uses
`ghcr.io/devazizi/helm-operator:v1.0.3`. Update its `image` field if you want to
run a different release or a locally built image.

## Usage

### Add a public Helm repository

`Repository` is cluster-scoped, so it does not need a namespace.

```yaml
apiVersion: helm.k8s.ir/v1alpha1
kind: Repository
metadata:
  name: haproxy-charts
spec:
  url: https://haproxytech.github.io/helm-charts
```

Apply and inspect it:

```bash
kubectl apply -f examples/repo.helm.yaml
kubectl get repositories
kubectl get repository haproxy-charts -o yaml
```

Once the Helm commands finish, the operator sets `status.processed` to `true`.

### Add an authenticated Helm repository

```yaml
apiVersion: helm.k8s.ir/v1alpha1
kind: Repository
metadata:
  name: private-charts
spec:
  url: https://charts.example.com
  hasCredentials: true
  username: helm-user
  password: change-me
```

When `hasCredentials` is true, the operator passes the username and password to
`helm repo add`.

> [!WARNING]
> The current API stores these credentials directly in the custom resource.
> They are not read from a Kubernetes Secret. Do not place production
> credentials here without first changing the credential design.

### Install a chart

Create a namespaced `DeployChart` after registering its repository:

```yaml
apiVersion: helm.k8s.ir/v1alpha1
kind: DeployChart
metadata:
  name: haproxy
  namespace: default
spec:
  chart:
    repo: haproxy-charts
    chart: haproxy
    version: 1.24.0
  values:
    service:
      type: ClusterIP
    replicaCount: 2
```

This resource is equivalent to running a command similar to:

```bash
helm upgrade --install haproxy haproxy-charts/haproxy \
  --namespace default \
  --version 1.24.0 \
  --values values.yaml \
  --create-namespace
```

Apply and inspect the resource:

```bash
kubectl apply -f deploy-chart.yaml
kubectl get deploycharts -A
kubectl get deploychart haproxy -n default -o yaml
helm list -n default
```

The CRD also provides the short name `deploy`, so the following works:

```bash
kubectl get deploy -A
```

### Upgrade a release

Change the chart version or any value under `spec.values`, then apply the
resource again:

```bash
kubectl apply -f deploy-chart.yaml
```

The operator compares the new specification hash with
`status.lastAppliedHash`. If they differ, it runs `helm upgrade --install`
again.

### Uninstall a release

Delete the `DeployChart` resource:

```bash
kubectl delete deploychart haproxy -n default
```

The controller then attempts to run:

```bash
helm uninstall haproxy --namespace default
```

### Remove a repository

Delete its `Repository` resource:

```bash
kubectl delete repository haproxy-charts
```

The controller attempts to remove the matching local Helm repository from the
operator container.

### Reflect a Secret or ConfigMap

Create a `Reflector` in the same namespace and with the same name as its source
object. `reflectTo` entries are shell-style namespace patterns: `*` matches any
sequence of characters, `?` matches one character, and character classes such
as `[a-z]` are supported.

```yaml
apiVersion: others.helm.k8s.ir/v1alpha1
kind: Reflector
metadata:
  name: star-example-com
  namespace: cert-manager
spec:
  kind: Secret
  reflectTo:
    - dev-*
    - prod-*
```

In this example, the source must be a Secret named `star-example-com` in
`cert-manager`. The operator creates and maintains a Secret with that name in
every matching namespace. Use `kind: ConfigMap` to reflect a ConfigMap instead.

```bash
kubectl apply -f crd/reflector.crd.yaml
kubectl apply -f examples/reflector-secret.yaml
kubectl get reflectors -A
kubectl get reflector star-example-com -n cert-manager -o yaml
```

Reflection has the following behavior:

- Existing and newly created matching namespaces are supported.
- Source data changes are copied to all targets.
- Removing a pattern deletes copies that are no longer targeted.
- Deleting the Reflector deletes all copies managed by that Reflector.
- Deleting the source removes its managed copies and sets `Ready=False`.
- The source namespace is skipped, because it already contains the source.
- An existing target object that is not managed by this Reflector is never
  overwritten; the Reflector reports a failed condition instead.
- Source labels and annotations are not copied. Managed targets receive only
  the operator's tracking metadata.

The example files cover both supported kinds:

- `examples/reflector-secret.yaml`
- `examples/reflector-configmap.yaml`

## Custom resource reference

### `Repository`

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `spec.url` | string | none | URL of the Helm repository. |
| `spec.hasCredentials` | boolean | `false` | Whether to pass credentials to Helm. |
| `spec.username` | string | `""` | Repository username. |
| `spec.password` | string | `""` | Repository password. |
| `status.processed` | boolean | `false` | Whether the repository was processed. |

The resource kind is `Repository`, its plural name is `repositories`, and its
short name is `repo`.

### `DeployChart`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `spec.chart.repo` | string | yes | Name of a previously configured Helm repository. |
| `spec.chart.chart` | string | yes | Chart name inside the repository. |
| `spec.chart.version` | string | yes | Exact chart version passed to Helm. |
| `spec.values` | object | no | Arbitrary values written to the Helm values file. |
| `status.processed` | boolean | no | Whether the current specification was applied. |
| `status.state` | string | no | Intended state: `Pending`, `Deploying`, `Succeeded`, or `Failed`. |
| `status.message` | string | no | Intended human-readable status message. |
| `status.lastAppliedHash` | string | no | SHA-256 hash of the last applied specification. |

The resource kind is `DeployChart`, its plural name is `deploys`, and its short
name is `deploy`.

### `Reflector`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `spec.kind` | string | yes | Source kind: `Secret` or `ConfigMap`. |
| `spec.reflectTo` | string array | yes | Shell-style patterns matched against namespace names. |
| `status.observedGeneration` | integer | no | Latest resource generation handled by the controller. |
| `status.reflectedNamespaceCount` | integer | no | Number of successfully synchronized namespaces. |
| `status.reflectedNamespaces` | string array | no | Successfully synchronized namespace names. |
| `status.conditions` | condition array | no | Standard Kubernetes conditions, including `Ready`. |

The source is identified by the Reflector's own `metadata.name` and
`metadata.namespace`. The resource kind is `Reflector`, its plural name is
`reflectors`, and its short name is `reflect`.

## High availability

The supplied deployment runs two replicas. Controller-runtime leader election
uses the ID `happyhelm-controller-leader-election`, so only the elected replica
actively reconciles resources at a given time.

Leader election protects against two replicas operating on the same resources
simultaneously. It does not currently make the Helm repository configuration
highly available: `helm repo add` writes to the elected container's local
filesystem, which is not shared with another replica.

## Project structure

| Path | Purpose |
| --- | --- |
| `main.go` | Creates the controller manager, registers APIs, enables leader election, and starts both controllers. |
| `api/v1alpha1/helm_repo.go` | Go types for the `Repository` API. |
| `api/v1alpha1/helm_chart.go` | Go types for the `DeployChart` API. |
| `api/v1alpha1/reflector.go` | Go types for the `Reflector` API. |
| `controller/helm_repo_controller.go` | Adds, updates, and removes Helm repositories. |
| `controller/helm_chart_controller.go` | Installs, upgrades, and uninstalls Helm releases. |
| `controller/reflector_controller.go` | Synchronizes Secrets and ConfigMaps across matching namespaces. |
| `internal/template.go` | Experimental values templating helpers; these are not currently connected to reconciliation. |
| `crd/` | Kubernetes CRD manifests. |
| `deploy/` | Operator namespace, RBAC, and deployment manifest. |
| `examples/` | Example repository and chart resources. |
| `Dockerfile` | Multi-stage build for the operator runtime image. |
| `.github/workflows/docker-image.yml` | Builds, publishes, and signs images for version tags. |

## Development

Download dependencies and run all package tests:

```bash
go mod download
go test ./...
```

Build the binary:

```bash
go build -o operator ./main.go
```

Run it against the cluster selected by your current kubeconfig:

```bash
go run ./main.go
```

The local environment must have the `helm` executable available on `PATH`.
Install both CRDs before starting the controller.

### Build the container image

```bash
docker build -t helm-operator:local .
```

Push the image to a registry accessible by your cluster, then update the image
in `deploy/first-deploy.yaml`.

### Release images

The GitHub Actions workflow runs for tags matching `v*.*.*`. It builds the OCI
image, pushes it to GitHub Container Registry, and signs the published digest
with Cosign.

## Troubleshooting

### A repository remains unprocessed

Inspect the resource and operator logs:

```bash
kubectl describe repository <repository-name>
kubectl logs -n helm-operator deployment/helm-operator-deployment
```

Check that the repository URL is reachable from the operator pod and that any
credentials are correct.

### A chart cannot be installed

Verify the repository name, chart name, chart version, and namespace:

```bash
kubectl describe deploychart <release-name> -n <namespace>
kubectl logs -n helm-operator deployment/helm-operator-deployment
helm list -n <namespace>
```

The repository name in `spec.chart.repo` must match the name of a `Repository`
resource that has already been processed.

### The active replica changes and charts stop resolving

Helm repository configuration is currently stored in each pod's local
filesystem. Recreate or reprocess repository configuration on the new leader,
or change the operator to use shared/persistent repository configuration.

## Limitations and security notes

- The deployment grants the service account the built-in `cluster-admin` role.
  Production deployments should use a narrowly scoped custom role.
- Repository credentials are stored as plain custom-resource fields instead of
  Secret references.
- Secret reflection deliberately copies sensitive data into other namespaces.
  Anyone who can read Secrets in a target namespace can read the reflected
  value, so namespace patterns and Reflector RBAC must be tightly controlled.
- An error from `helm repo add` is currently logged but not returned by the
  helper, so a failed repository can incorrectly be marked as processed.
- Helm repository configuration is local to one pod and is not preserved across
  pod replacement or leader failover.
- `DeployChart` does not use a Kubernetes finalizer, so uninstall-on-delete is
  best effort rather than guaranteed.
- Failed Helm operations are returned as reconciliation errors, but the
  `Failed` state and status message are not currently persisted.
- The experimental template functions in `internal/template.go` are unused.
  The `vars` field shown in `examples/haproxy.helm.yaml` is not part of the CRD
  schema and values templating should not be considered supported yet.
- The project does not currently include automated tests.

## License

No license file is currently included. Add a license before distributing or
accepting contributions under defined terms.
