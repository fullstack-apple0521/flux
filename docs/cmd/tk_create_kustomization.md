## tk create kustomization

Create or update a kustomization resource

### Synopsis


The kustomization source command generates a kustomization.kustomize.fluxcd.io resource for a given GitRepository source.
API spec: https://github.com/fluxcd/kustomize-controller/tree/master/docs/spec/v1alpha1

```
tk create kustomization [name] [flags]
```

### Examples

```
  # Create a kustomization from a source at a given path
  create kustomization contour \
    --source=contour \
    --path="./examples/contour/" \
    --prune=true \
    --interval=10m \
    --validate=client \
    --health-check="Deployment/contour.projectcontour" \
    --health-check="DaemonSet/envoy.projectcontour" \
    --health-check-timeout=3m

  # Create a kustomization that depends on the previous one
  create kustomization webapp \
    --depends-on=contour \
    --source=webapp \
    --path="./deploy/overlays/dev" \
    --prune=true \
    --interval=5m \
    --validate=client

  # Create a kustomization that runs under a service account
  create kustomization webapp \
    --source=webapp \
    --path="./deploy/overlays/staging" \
    --prune=true \
    --interval=5m \
    --validate=client \
    --sa-name=reconclier \
    --sa-namespace=staging

```

### Options

```
      --depends-on stringArray          kustomization that must be ready before this kustomization can be applied
      --health-check stringArray        workload to be included in the health assessment, in the format '<kind>/<name>.<namespace>'
      --health-check-timeout duration   timeout of health checking operations (default 2m0s)
  -h, --help                            help for kustomization
      --path string                     path to the directory containing the kustomization file (default "./")
      --prune                           enable garbage collection
      --sa-name string                  service account name
      --sa-namespace string             service account namespace
      --source string                   GitRepository name
      --validate string                 validate the manifests before applying them on the cluster, can be 'client' or 'server'
```

### Options inherited from parent commands

```
      --components strings   list of components, accepts comma-separated values (default [source-controller,kustomize-controller])
      --export               export in yaml format to stdout
      --interval duration    source sync interval (default 1m0s)
      --kubeconfig string    path to the kubeconfig file (default "~/.kube/config")
      --namespace string     the namespace scope for this operation (default "gitops-system")
      --timeout duration     timeout for this operation (default 5m0s)
      --verbose              print generated objects
```

### SEE ALSO

* [tk create](tk_create.md)	 - Create commands

