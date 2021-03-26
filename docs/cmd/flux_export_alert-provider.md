---
title: "flux export alert-provider command"
---
## flux export alert-provider

Export Provider resources in YAML format

### Synopsis

The export alert-provider command exports one or all Provider resources in YAML format.

```
flux export alert-provider [name] [flags]
```

### Examples

```
  # Export all Provider resources
  flux export alert-provider --all > alert-providers.yaml

  # Export a Provider
  flux export alert-provider slack > slack.yaml
```

### Options

```
  -h, --help   help for alert-provider
```

### Options inherited from parent commands

```
      --all                 select all resources
      --context string      kubernetes context to use
      --kubeconfig string   absolute path to the kubeconfig file
  -n, --namespace string    the namespace scope for this operation (default "flux-system")
      --timeout duration    timeout for this operation (default 5m0s)
      --verbose             print generated objects
```

### SEE ALSO

* [flux export](../flux_export/)	 - Export resources in YAML format

