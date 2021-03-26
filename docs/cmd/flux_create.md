---
title: "flux create command"
---
## flux create

Create or update sources and resources

### Synopsis

The create sub-commands generate sources and resources.

### Options

```
      --export              export in YAML format to stdout
  -h, --help                help for create
      --interval duration   source sync interval (default 1m0s)
      --label strings       set labels on the resource (can specify multiple labels with commas: label1=value1,label2=value2)
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

* [flux](../flux/)	 - Command line utility for assembling Kubernetes CD pipelines
* [flux create alert](../flux_create_alert/)	 - Create or update a Alert resource
* [flux create alert-provider](../flux_create_alert-provider/)	 - Create or update a Provider resource
* [flux create helmrelease](../flux_create_helmrelease/)	 - Create or update a HelmRelease resource
* [flux create image](../flux_create_image/)	 - Create or update resources dealing with image automation
* [flux create kustomization](../flux_create_kustomization/)	 - Create or update a Kustomization resource
* [flux create receiver](../flux_create_receiver/)	 - Create or update a Receiver resource
* [flux create secret](../flux_create_secret/)	 - Create or update Kubernetes secrets
* [flux create source](../flux_create_source/)	 - Create or update sources
* [flux create tenant](../flux_create_tenant/)	 - Create or update a tenant

