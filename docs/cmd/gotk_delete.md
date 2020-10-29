## gotk delete

Delete sources and resources

### Synopsis

The delete sub-commands delete sources and resources.

### Options

```
  -h, --help     help for delete
  -s, --silent   delete resource without asking for confirmation
```

### Options inherited from parent commands

```
      --kubeconfig string   path to the kubeconfig file (default "~/.kube/config")
  -n, --namespace string    the namespace scope for this operation (default "flux-system")
      --timeout duration    timeout for this operation (default 5m0s)
      --verbose             print generated objects
```

### SEE ALSO

* [gotk](gotk.md)	 - Command line utility for assembling Kubernetes CD pipelines
* [gotk delete alert](gotk_delete_alert.md)	 - Delete a Alert resource
* [gotk delete alert-provider](gotk_delete_alert-provider.md)	 - Delete a Provider resource
* [gotk delete helmrelease](gotk_delete_helmrelease.md)	 - Delete a HelmRelease resource
* [gotk delete kustomization](gotk_delete_kustomization.md)	 - Delete a Kustomization resource
* [gotk delete receiver](gotk_delete_receiver.md)	 - Delete a Receiver resource
* [gotk delete source](gotk_delete_source.md)	 - Delete sources

