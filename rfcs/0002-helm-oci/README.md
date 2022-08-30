# RFC-0002 Flux OCI support for Helm

**Status:** implemented (partially)

**Creation date:** 2022-03-30

**Last update:** 2022-08-24

## Summary

Given that Helm v3.8 supports [OCI](https://helm.sh/docs/topics/registries/) for package distribution,
we should extend the Flux Source API to allow fetching Helm charts from container registries.

## Motivation

Helm OCI support is one of the most requested feature in Flux
as seen on this [issue](https://github.com/fluxcd/source-controller/issues/124).

With OCI support, Flux users can automate chart updates to Git in the same way
they do today for container images.

### Goals

- Add support for fetching Helm charts stored as OCI artifacts with minimal API changes to Flux.
- Make it easy for users to switch from [HTTP/S Helm repositories](https://github.com/helm/helm-www/blob/416fabea6ffab8dc156b6a0c5eb5e8df5f5ef7dc/content/en/docs/topics/chart_repository.md)
  to OCI repositories.

### Non-Goals

- Introduce a new API kind for referencing charts stored as OCI artifacts.

## Proposal

Introduce an optional field called `type` to the `HelmRepository` spec.
When not specified, the `spec.type` field defaults to `default` which preserve the current `HelmRepository` API behaviour.
When the `spec.type` field is set to `oci`, the `spec.url` field must be prefixed with `oci://` (to follow the Helm conventions).
For `oci://` URLs, source-controller will use the Helm SDK and the `oras` library to connect to the OCI remote storage.

Introduce an optional field called `provider` for
[context-based authorization](https://fluxcd.io/flux/security/contextual-authorization/)
to AWS, Azure and Google Cloud. The `spec.provider` is ignored when `spec.type` is set to `default`.


### Pull charts from private repositories

#### Basic auth

For private repositories hosted on GitHub, Quay, self-hosted Docker Registry and others,
the credentials can be supplied with:

```yaml
apiVersion: source.toolkit.fluxcd.io/v1beta2
kind: HelmRepository
metadata:
  name: <repo-name>
spec:
  type: oci
  secretRef:
    name: regcred
```

The `secretRef` points to a Kubernetes secret in the same namespace as the `HelmRepository`.
The [secret type](https://kubernetes.io/docs/concepts/configuration/secret/#secret-types)
must be `kubernetes.io/dockerconfigjson`:

```shell
kubectl create secret docker-registry regcred \
  --docker-server=<your-registry-server> \
  --docker-username=<your-name> \
  --docker-password=<your-pword>
```

#### OIDC auth

When Flux runs on AKS, EKS or GKE, an IAM role (that grants read-only access to ACR, ECR or GCR)
can be used to bind the `source-controller` to the IAM role.

```yaml
apiVersion: source.toolkit.fluxcd.io/v1beta2
kind: HelmRepository
metadata:
  name: <repo-name>
spec:
  type: oci
  provider: azure
```

The provider accepts the following values: `generic`, `aws`, `azure` and `gcp`. When the provider is
not specified, it defaults to `generic`. When the provider is set to `aws`, `azure` or `gcp`, the
controller will use a specific cloud SDK for authentication purposes.

If both `spec.secretRef` and a non-generic provider are present in the definition,
the controller will use the static credentials from the referenced secret.

### User Stories

#### Story 1

> As a developer I want to use Flux `HelmReleases` that refer to Helm charts stored
> as OCI artifacts in GitHub Container Registry.

First create a secret using a GitHub token that allows access to GHCR:

```sh
kubectl create secret docker-registry ghcr-charts \
    --docker-server=ghcr.io \
    --docker-username=$GITHUB_USER \
    --docker-password=$GITHUB_TOKEN
```

Then define a `HelmRepository` of type `oci` and reference the `dockerconfig` secret:

```yaml
apiVersion: source.toolkit.fluxcd.io/v1beta2
kind: HelmRepository
metadata:
  name: ghcr-charts
  namespace: default
spec:
  type: oci
  url: oci://ghcr.io/my-org/charts/
  secretRef:
    name: ghcr-charts
```

And finally in Flux `HelmReleases`, refer to the ghcr-charts `HelmRepository`:

```yaml
apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata:
  name: my-app
  namespace: default
spec:
  interval: 60m
  chart:
    spec:
      chart: my-app
      version: '1.0.x'
      sourceRef:
        kind: HelmRepository
        name: ghcr-charts
      interval: 1m # check for new OCI artifacts every minute
```

#### Story 2

> As a platform admin I want to automate Helm chart updates based on a semver ranges.
> When a new patch version is available in the container registry, I want Flux to open a PR
> with the version set in the `HelmRelease` manifests.

Given that charts are stored in container registries, you can use Flux image automation
and patch the chart version in Git, in the same way Flux works for updating container image tags.

Define an image registry and a policy for the chart artifact:

```yaml
apiVersion: image.toolkit.fluxcd.io/v1beta1
kind: ImageRepository
metadata:
  name: my-app
  namespace: default
spec:
  image: ghcr.io/my-org/charts/my-app
  interval: 1m0s
---
apiVersion: image.toolkit.fluxcd.io/v1beta1
kind: ImagePolicy
metadata:
  name: my-app
  namespace: default
spec:
  imageRepositoryRef:
    name: my-app
  policy:
    semver:
      range: 1.0.x
```

Then add the policy marker to the `HelmRelease` manifests in Git:

```yaml
apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata:
  name: my-app
  namespace: default
spec:
  interval: 60m
  chart:
    spec:
      chart: my-app
      version: 1.0.0 # {"$imagepolicy": "default:my-app:tag"}
      sourceRef:
        kind: HelmRepository
        name: ghcr-charts
      interval: 1m
```

### Alternatives

We could introduce a new API type e.g. `HelmRegistry` to hold the reference to auth secret,
as proposed in [#2573](https://github.com/fluxcd/flux2/pull/2573).
That is considered unpractical, as there is no benefit for users in having a dedicated kind instead of
a `type` field in the current `HelmRepository` API. Adding a `type` field to the spec follows the Flux
Bucket API design, where the same Kind servers different implementations: AWS S3 vs Azure Blob vs Google Storage.

## Design Details

In source-controller we'll add a new predicate for filtering `HelmRepositories` based on the `spec.type` field.

The current `HelmRepositoryReconciler` will handle only objects with `type: default`,
it's scope remains unchanged.

We'll introduce a new reconciler named `HelmRepositoryOCIReconciler`, that will handle
objects with `type: oci`. This reconciler will set the `HelmRepository` Ready status to
`False` if:
- the URL is not prefixed with `oci://`
- the URL is malformed and can't be parsed
- the specified credentials result in an authentication error

The current `HelmChartReconciler` will be adapted to handle both types.

### Enabling the feature

The feature is enabled by default.

## Implementation History

* **2022-05-19** Partially implemented by [source-controller#690](https://github.com/fluxcd/source-controller/pull/690)
* **2022-06-06** First implementation released with [flux2 v0.31.0](https://github.com/fluxcd/flux2/releases/tag/v0.31.0)
* **2022-08-11** Resolve chart dependencies from OCI released with [flux2 v0.32.0](https://github.com/fluxcd/flux2/releases/tag/v0.32.0)
* **2022-08-29** Contextual login for AWS, Azure and GCP released with [flux2 v0.33.0](https://github.com/fluxcd/flux2/releases/tag/v0.33.0)

### TODOs

* [Add support for container registries with self-signed TLS certs](https://github.com/fluxcd/source-controller/issues/723)
