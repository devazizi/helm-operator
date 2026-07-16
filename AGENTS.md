# AGENTS.md

## Project overview

This repository contains a Go-based Kubernetes operator. It currently manages
three custom resources:

- `Repository` (`helm.k8s.ir/v1alpha1`): configures a Helm chart repository.
- `DeployChart` (`helm.k8s.ir/v1alpha1`): installs, upgrades, and removes a Helm
  release.
- `Reflector` (`others.helm.k8s.ir/v1alpha1`): synchronizes a Secret or ConfigMap
  into namespaces selected by shell-style patterns.

The operator uses `sigs.k8s.io/controller-runtime` and Helm's Go SDK. A mutating
admission webhook records the authenticated `DeployChart` creator so Helm
actions can use Kubernetes user/group impersonation.

## Repository layout

- `cmd/manager/main.go`: scheme registration, manager configuration, and
  controller setup.
- `api/helm/v1alpha1/`: Helm repository and release API types.
- `api/others/v1alpha1/`: Reflector API types.
- `internal/controller/`: reconciliation logic and controller tests.
- `internal/helm/`: Helm SDK integration and impersonated action settings.
- `internal/webhook/`: trusted creator-identity admission handling.
- `internal/values/`: experimental values helpers.
- `charts/helm-operator/`: the sole installation path, including canonical
  CRDs, StatefulSet, persistent cache, RBAC, webhook TLS, and Services.
- `Dockerfile`: operator container build.

## Development commands

Use Go 1.26 or newer.

```bash
go mod download
go test ./...
go build -o operator ./cmd/manager
```

Format changed Go files before finishing:

```bash
gofmt -w <changed-go-files>
```

Validate chart YAML by rendering it:

```bash
helm template helm-operator charts/helm-operator --namespace helm-operator
```

When the environment restricts writes to the default Go cache, use a temporary
cache without changing project behavior:

```bash
GOCACHE=/tmp/helm-operator-gocache go test ./...
```

## Implementation conventions

- Keep reconciliation idempotent. Reprocessing an unchanged resource must not
  produce unnecessary Kubernetes updates or Helm operations.
- Always propagate command and API errors. Do not mark a resource successful
  after an operation has failed.
- Use the status subresource for observed state.
- Prefer standard `metav1.Condition` conditions with clear `Reason` and
  `Message` values.
- Set `status.observedGeneration` when a generation has been handled.
- Use finalizers when deletion requires cleanup outside the custom resource.
- Treat cleanup of an already absent object as successful.
- Never derive an impersonated identity from mutable spec fields or untrusted
  user annotations. Creator identity must come from admission
  `request.userInfo`, and updates must preserve the previously trusted value.
- Never log Secret contents, passwords, tokens, private keys, or Helm repository
  credentials.
- Do not overwrite Kubernetes objects that the operator does not clearly own.
- Deep-copy maps and byte slices before assigning data between cached objects.
- Add focused tests for new matching, reconciliation, ownership, and cleanup
  behavior.

## API and CRD changes

The canonical CRDs are under `charts/helm-operator/crds/` and are maintained
alongside the Go API types. Do not reintroduce a separate raw-manifest tree.
When an API field changes, update all of the following in the same change:

1. The matching type under `api/helm/v1alpha1/` or `api/others/v1alpha1/`.
2. The canonical OpenAPI schema under `charts/helm-operator/crds/`.
3. The custom-resource reference in `README.md`.
4. Tests covering validation or behavior changes.

New fields should include appropriate validation, such as required fields,
enums, minimum lengths, and array limits. Avoid silently accepting unsupported
configuration.

## Reflector behavior

The Reflector contract is intentional and should remain stable unless a change
explicitly redesigns the API:

- A Reflector's `metadata.name` and `metadata.namespace` identify its source.
- `spec.kind` supports only `Secret` and `ConfigMap`.
- `spec.reflectTo` uses shell-style patterns through Go's `path.Match`; it is not
  regular-expression syntax.
- Exact namespace names work without wildcard characters.
- The source namespace is skipped.
- Source updates and namespace changes must trigger reconciliation.
- Managed target deletion or modification must trigger source reconciliation.
- Unmanaged target objects must never be overwritten or deleted.
- Managed objects are identified using the Reflector UID label and tracking
  annotations defined in `internal/controller/reflector_controller.go`.
- Pattern changes, kind changes, source deletion, and Reflector deletion must
  remove stale managed copies.
- Cross-namespace owner references must not be used; Kubernetes does not permit
  them as an ownership mechanism.
- Source labels and annotations are not copied to targets. Only data, Secret
  type, and the operator's tracking metadata are synchronized.

Secret reflection expands access to sensitive information. Changes must
preserve collision protection and must not expose data through status, events,
or logs.

## Helm controller considerations

- The `DeployChart` resource name is the Helm release name.
- Its namespace is the Helm release namespace.
- A changed specification hash triggers an SDK install or upgrade action.
- Install, upgrade, release lookup, and uninstall must use the authenticated
  creator's username and groups through Kubernetes impersonation.
- The original creator remains fixed for the resource lifetime. Updates by a
  different user must not transfer the execution identity.
- A finalizer must protect impersonated uninstall-on-delete.
- Repository URLs are used directly for isolated SDK actions; do not introduce
  correctness dependencies on pod-local Helm configuration.
- Persistent chart archives use `HELM_CHART_CACHE` and must remain valid `.tgz`
  files keyed by repository URL, chart name, and version.
- Standard HTTP proxy environment variables must continue to reach all Helm SDK
  repository and chart HTTP clients.
- The existing values-template helper in `internal/values/template.go` is experimental
  and is not part of active reconciliation.

When changing Helm execution, preserve timeouts and ensure errors and SDK logs
do not expose credentials or sensitive values.

## Deployment and security

The deployment uses a custom operator ClusterRole plus `impersonate` permission
for users and groups. It does not bind `cluster-admin`, but broad impersonation
is itself highly privileged. Kubernetes authorizes chart operations as the
recorded creator rather than as the operator ServiceAccount.

For security-related changes:

- Prefer least-privilege RBAC where the supported chart model permits it.
- Preserve the webhook's default fail-closed behavior and TLS trust chain.
- Treat changes to creator annotations, webhook matching rules, impersonation
  RBAC, or finalizers as security-boundary changes requiring focused tests.
- Reference Kubernetes Secrets instead of storing credentials directly in a
  custom resource.
- Avoid adding credentials to command-line arguments where possible.
- Keep the runtime image minimal and pin important dependency versions.
- Do not contact or mutate a live Kubernetes cluster unless the user explicitly
  asks for deployment or live verification.

## Testing expectations

At minimum, run:

```bash
go test ./...
```

For chart or deployment changes, also run:

```bash
helm lint charts/helm-operator
helm template helm-operator charts/helm-operator --namespace helm-operator
```

The chart is the only supported deployment mechanism. Do not add Kustomize or
standalone namespace, RBAC, controller, webhook, sample, or CRD manifests.

For controller changes, tests should cover the success path and important
safety behavior. Examples include:

- Resource creation and update.
- Idempotent reconciliation.
- Invalid configuration.
- Missing source objects.
- Ownership collisions.
- Pattern changes and stale-object cleanup.
- Finalizer cleanup.

Do not claim live-cluster verification when only unit tests, fake-client tests,
or client-side manifest checks were run.

## Working-tree safety

- Preserve unrelated modified and untracked files; they belong to the user.
- Inspect `git status --short` before and after changes.
- Do not use destructive Git commands to discard changes.
- Keep changes scoped to the requested task.
- Do not edit dependency files unless the implementation actually requires a
  dependency change.
- Update `README.md` when user-facing installation, API, or behavior changes.
