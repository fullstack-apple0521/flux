---
title: "flux reconcile helmrelease command"
---
## flux reconcile helmrelease

Reconcile a HelmRelease resource

### Synopsis


The reconcile kustomization command triggers a reconciliation of a HelmRelease resource and waits for it to finish.

```
flux reconcile helmrelease [name] [flags]
```

### Examples

```
  # Trigger a HelmRelease apply outside of the reconciliation interval
  flux reconcile hr podinfo

  # Trigger a reconciliation of the HelmRelease's source and apply changes
  flux reconcile hr podinfo --with-source
```

### Options

```
  -h, --help          help for helmrelease
      --with-source   reconcile HelmRelease source
```

### Options inherited from parent commands

```
      --context string      kubernetes context to use
      --kubeconfig string   absolute path to the kubeconfig file
  -n, --namespace string    the namespace scope for this operation (default "flux-system")
      --timeout duration    timeout for this operation (default 5m0s)
      --verbose             print generated objects
```

### SEE ALSO

* [flux reconcile](../flux_reconcile/)	 - Reconcile sources and resources

