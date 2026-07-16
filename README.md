# Helm Operator

Helm Operator is a small Kubernetes operator that manages Helm repositories and
Helm releases through Kubernetes custom resources. Instead of running `helm repo
add` and `helm upgrade --install` manually, you create `Repository` and
`DeployChart` objects and let the operator reconcile them with the Helm Go SDK.

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
- Execute release operations with Kubernetes impersonation as the authenticated
  user who originally created the `DeployChart`.
- Report basic reconciliation state in the resource status.
- Run as a StatefulSet with a persistent `.tgz` chart cache.
- Reach internal or mirrored repositories through configurable HTTP/HTTPS
  proxies.
- Reflect Secrets and ConfigMaps into namespaces selected by wildcard patterns.
- Build and publish a container image when a semantic version Git tag is pushed.

## How it works

The operator watches Helm resources in the `helm.k8s.ir/v1alpha1` API group and
a `Reflector` resource in `others.helm.k8s.ir/v1alpha1`:

1. A cluster-scoped `Repository` is validated by downloading its index with the
   Helm Go SDK. Repository state is not persisted in an operator pod.
2. A mutating admission webhook records the authenticated creator's username
   and groups on a new namespaced `DeployChart`. User-supplied identity
   annotations are overwritten, and updates preserve the original identity.
3. The controller configures Helm's Kubernetes client to impersonate that
   creator, then installs or upgrades the chart through the Helm Go SDK.
4. The Kubernetes resource name becomes the Helm release name, and the resource
   namespace becomes the Helm release namespace.
5. The operator hashes the complete `DeployChart` specification. A changed hash
   causes another Helm upgrade.
6. A finalizer makes deletion run the Helm SDK uninstall action as the original
   creator before the `DeployChart` disappears.
7. A namespaced `Reflector` copies a Secret or ConfigMap with the same name and
   namespace into namespaces selected by shell-style patterns.

The runtime image contains the controller and CA certificates; it does not need
the `helm` or `kubectl` executables.

## Requirements

- A Kubernetes cluster
- `kubectl` configured for that cluster
- cert-manager installed, unless you provide a webhook TLS Secret and CA bundle
- Helm 3 or newer only for installing and validating this operator's chart
- A default StorageClass or a pre-created PVC for the chart cache
- Docker or another OCI builder, when building the image locally
- Go 1.26 or newer, when developing locally

## Installation

### Install with Helm

The default chart configuration uses cert-manager to issue the admission
webhook certificate. Install cert-manager first, then install the operator and
all three CRDs:

```bash
helm upgrade --install helm-operator ./charts/helm-operator \
  --namespace helm-operator \
  --create-namespace
```

Use a custom image tag when installing code that has not been released yet:

```bash
helm upgrade --install helm-operator ./charts/helm-operator \
  --namespace helm-operator \
  --create-namespace \
  --set image.repository=registry.example.com/helm-operator \
  --set image.tag=my-tag
```

Important chart values include:

| Value | Default | Description |
| --- | --- | --- |
| `replicaCount` | `1` | Number of StatefulSet replicas; one is recommended for a `ReadWriteOnce` cache. |
| `image.repository` | `ghcr.io/devazizi/helm-operator` | Operator image repository. |
| `image.tag` | `""` | Image tag; an empty value uses `Chart.appVersion`. |
| `serviceAccount.create` | `true` | Create a ServiceAccount for the operator. |
| `serviceAccount.name` | `""` | Existing ServiceAccount name when creation is disabled. |
| `rbac.create` | `true` | Create the operator ClusterRole and ClusterRoleBinding. |
| `persistence.enabled` | `true` | Create a StatefulSet claim for cached chart archives. |
| `persistence.existingClaim` | `""` | Mount a pre-created PVC instead of a claim template. |
| `persistence.size` | `5Gi` | Requested chart-cache capacity. |
| `persistence.storageClass` | `""` | StorageClass; empty uses the cluster default and `-` disables dynamic selection. |
| `proxy.httpProxy` | `""` | HTTP proxy URL exposed as `HTTP_PROXY` and `http_proxy`. |
| `proxy.httpsProxy` | `""` | HTTPS proxy URL exposed as `HTTPS_PROXY` and `https_proxy`. |
| `proxy.noProxy` | `""` | Comma-separated proxy exclusions. |
| `proxy.existingSecret` | `""` | Secret containing standard proxy environment variables. |
| `webhook.certManager.enabled` | `true` | Have cert-manager issue and inject webhook TLS data. |
| `webhook.certManager.createIssuer` | `true` | Create a self-signed namespaced Issuer. |
| `webhook.certManager.issuerRef` | built-in Issuer | Use an existing Issuer or ClusterIssuer instead. |
| `webhook.existingSecret` | `""` | TLS Secret used when cert-manager integration is disabled. |
| `webhook.caBundle` | `""` | Base64 CA bundle required with an externally managed TLS Secret. |
| `resources` | see `values.yaml` | Container requests and limits. |
| `nodeSelector`, `tolerations`, `affinity` | empty | Pod scheduling configuration. |

The supplied ClusterRole gives the controller its reconciliation permissions
and permission to impersonate users and groups. It does not bind `cluster-admin`
to the ServiceAccount. Impersonation is nevertheless a powerful cluster-wide
permission: Kubernetes authorizes each Helm request as the recorded creator,
but anyone who compromises the operator ServiceAccount could attempt to
impersonate another identity. Restrict access to that ServiceAccount and adjust
the `impersonate` rules if your identity model permits a narrower scope.

For a restricted network, mirror the operator image and required charts into
reachable registries or repositories. Configure an egress proxy when one is
available:

```bash
helm upgrade --install helm-operator ./charts/helm-operator \
  --namespace helm-operator \
  --create-namespace \
  --set image.repository=registry.internal.example/helm-operator \
  --set proxy.httpProxy=http://proxy.internal.example:3128 \
  --set proxy.httpsProxy=http://proxy.internal.example:3128 \
  --set-string proxy.noProxy='kubernetes.default.svc\,.svc\,.cluster.local\,10.0.0.0/8'
```

Proxy credentials can instead be placed in a Secret using keys such as
`HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY`, then selected with
`proxy.existingSecret`. Explicit proxy values take precedence over variables
from that Secret.

Downloaded charts are retained under `persistence.mountPath` as deterministic
`.tgz` archives keyed by repository URL, chart name, and version. A cached
combination can be reused after restart without downloading its archive again.
Repository validation still needs the repository index to be reachable when a
`Repository` resource is first created or changed.

To manage TLS without cert-manager, provide a `kubernetes.io/tls` Secret whose
certificate is valid for the chart's webhook Service and pass its CA bundle:

```bash
helm upgrade --install helm-operator ./charts/helm-operator \
  --namespace helm-operator \
  --create-namespace \
  --set webhook.certManager.enabled=false \
  --set webhook.existingSecret=helm-operator-webhook-tls \
  --set webhook.caBundle='<base64-ca-bundle>'
```

Helm installs the CRDs from the chart's `crds/` directory. There is no separate
raw-manifest installation path.

Uninstall the operator with:

```bash
helm uninstall helm-operator --namespace helm-operator
```

The CRDs, existing custom resources, and StatefulSet-generated PVCs remain after
Helm uninstall. Remove them separately only when their releases and cached
archives are no longer needed.

Confirm the installation:

```bash
kubectl get crd repositories.helm.k8s.ir deploys.helm.k8s.ir reflectors.others.helm.k8s.ir
kubectl get statefulset,pods,pvc -n helm-operator
kubectl logs -n helm-operator statefulset/helm-operator --follow
```

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
kubectl apply -f repository.yaml
kubectl get repositories
kubectl get repository haproxy-charts -o yaml
```

Once the index download succeeds, the operator sets `status.processed` to
`true` and records `status.observedGeneration`.

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
the Helm SDK's repository and chart download clients.

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

This resource has behavior similar to the following command, but it is executed
in-process with the Helm SDK and Kubernetes impersonation:

```bash
helm upgrade --install haproxy haproxy-charts/haproxy \
  --namespace default \
  --version 1.24.0 \
  --values values.yaml
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

The admission webhook stores protected creator annotations from the API
server's authenticated admission request. Do not set these annotations
yourself. After reconciliation, `status.executedAs` shows the username used for
Helm's Kubernetes requests.

The original creator remains the execution identity for later changes even if
another user edits the resource. That creator must retain enough Kubernetes
permissions for every object the chart reads or changes. Creating a
`DeployChart` does not grant any additional permissions.

### Upgrade a release

Change the chart version or any value under `spec.values`, then apply the
resource again:

```bash
kubectl apply -f deploy-chart.yaml
```

The operator compares the new specification hash with
`status.lastAppliedHash`. If they differ, it runs the Helm SDK upgrade action as
the original creator.

### Uninstall a release

Delete the `DeployChart` resource:

```bash
kubectl delete deploychart haproxy -n default
```

The finalizer runs the Helm SDK uninstall action as the original creator before
allowing deletion to finish. If that identity no longer exists or no longer has
permission, deletion remains pending until authorization is restored or an
administrator deliberately removes the finalizer.

The equivalent CLI operation is:

```bash
helm uninstall haproxy --namespace default
```

### Remove a repository

Delete its `Repository` resource:

```bash
kubectl delete repository haproxy-charts
```

Repositories are accessed directly by URL, so there is no pod-local Helm
repository entry to remove.

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
kubectl apply -f reflector.yaml
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

Change `spec.kind` to `ConfigMap` for ConfigMap sources. Save the manifest above
as `reflector.yaml` before applying it.

## Custom resource reference

### `Repository`

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `spec.url` | string | none | URL of the Helm repository. |
| `spec.hasCredentials` | boolean | `false` | Whether to pass credentials to Helm. |
| `spec.username` | string | `""` | Repository username. |
| `spec.password` | string | `""` | Repository password. |
| `status.processed` | boolean | `false` | Whether the repository was processed. |
| `status.observedGeneration` | integer | none | Latest repository generation validated by the controller. |

The resource kind is `Repository`, its plural name is `repositories`, and its
short name is `repo`.

### `DeployChart`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `spec.chart.repo` | string | yes | Name of a previously configured Helm repository. |
| `spec.chart.chart` | string | yes | Chart name inside the repository. |
| `spec.chart.version` | string | yes | Exact chart version passed to Helm. |
| `spec.values` | object | no | Arbitrary values passed directly to the Helm SDK. |
| `status.processed` | boolean | no | Whether the current specification was applied. |
| `status.state` | string | no | Intended state: `Pending`, `Deploying`, `Succeeded`, or `Failed`. |
| `status.message` | string | no | Intended human-readable status message. |
| `status.lastAppliedHash` | string | no | SHA-256 hash of the last applied specification. |
| `status.executedAs` | string | no | Authenticated creator impersonated for the last Helm attempt. |

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

The chart runs one StatefulSet replica by default. Controller-runtime leader
election uses the ID `happyhelm-controller-leader-election`, so only one replica
actively reconciles resources if `replicaCount` is increased.

Leader election protects against replicas operating on the same resources
simultaneously. With a StatefulSet claim template, every replica receives its
own cache PVC. Use a storage backend and access mode appropriate for your
replica count if configuring `persistence.existingClaim`.

## Project structure

| Path | Purpose |
| --- | --- |
| `cmd/manager/main.go` | Creates the manager, registers APIs, enables leader election, and starts the controllers. |
| `api/helm/v1alpha1/` | Go types for the `Repository` and `DeployChart` APIs. |
| `api/others/v1alpha1/` | Go types for the `Reflector` API. |
| `internal/controller/` | Helm repository, release, and reflector controllers and tests. |
| `internal/helm/` | Helm Go SDK adapter and impersonated action configuration. |
| `internal/webhook/` | Admission webhook that captures and protects creator identity. |
| `internal/values/` | Experimental values templating helpers; not connected to reconciliation. |
| `charts/helm-operator/` | The only installation path: StatefulSet, PVC, RBAC, webhook, and CRDs. |
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
go build -o operator ./cmd/manager
```

Run it against the cluster selected by your current kubeconfig:

```bash
go run ./cmd/manager
```

The controller does not require a local Helm executable. Install all three CRDs
and configure a trusted webhook certificate before starting it against a
cluster.

### Build the container image

```bash
docker build -t helm-operator:local .
```

Push the image to a registry accessible by your cluster, then set
`image.repository` and `image.tag` during Helm installation.

### Validate and package the Helm chart

```bash
helm lint charts/helm-operator
helm template helm-operator charts/helm-operator --namespace helm-operator
helm package charts/helm-operator
```

### Release images

The GitHub Actions workflow runs for tags matching `v*.*.*`. It builds the OCI
image, pushes it to GitHub Container Registry, and signs the published digest
with Cosign.

## Troubleshooting

### A repository remains unprocessed

Inspect the resource and operator logs:

```bash
kubectl describe repository <repository-name>
kubectl logs -n helm-operator statefulset/helm-operator
```

Check that the repository URL is reachable from the operator pod and that any
credentials are correct.

### A chart cannot be installed

Verify the repository name, chart name, chart version, and namespace:

```bash
kubectl describe deploychart <release-name> -n <namespace>
kubectl logs -n helm-operator statefulset/helm-operator
helm list -n <namespace>
```

The repository name in `spec.chart.repo` must match the name of a `Repository`
resource that has already been processed.

### A DeployChart is rejected or remains in Failed state

Check the webhook Service, certificate, and CA injection first. A
`DeployChart` created before the identity webhook was enabled has no trusted
creator metadata and must be recreated. If `status.executedAs` is present,
check that user's RBAC for every resource the chart manages; granting permissions
only to the operator ServiceAccount does not authorize an impersonated request.

## Limitations and security notes

- The operator ServiceAccount can impersonate users and groups. This is
  security-sensitive even though individual Helm requests are authorized as
  the impersonated creator. Narrow the allowed identities where possible and
  strictly protect the ServiceAccount credentials.
- Repository credentials are stored as plain custom-resource fields instead of
  Secret references.
- Secret reflection deliberately copies sensitive data into other namespaces.
  Anyone who can read Secrets in a target namespace can read the reflected
  value, so namespace patterns and Reflector RBAC must be tightly controlled.
- The original creator identity is fixed for the lifetime of a `DeployChart`.
  A different editor does not become the release owner, and deletion can block
  if the original creator loses authorization.
- The webhook requires correctly managed TLS. With `failurePolicy: Fail`, a
  webhook outage prevents `DeployChart` creates and updates; this protects the
  identity boundary but affects availability.
- Helm release state is stored using Helm's Kubernetes storage driver in the
  release namespace and is subject to the creator's authorization.
- The experimental template functions in `internal/values/template.go` are
  unused; values templating should not be considered supported yet.
- Repository credentials may still appear in the Kubernetes API object and
  etcd; use only appropriately protected clusters until Secret references are
  implemented.

## License

No license file is currently included. Add a license before distributing or
accepting contributions under defined terms.
