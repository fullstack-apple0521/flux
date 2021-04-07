---
title: "flux bootstrap git command"
---
## flux bootstrap git

Bootstrap toolkit components in a Git repository

### Synopsis

The bootstrap git command commits the toolkit components manifests to the
branch of a Git repository. It then configures the target cluster to synchronize with
the repository. If the toolkit components are present on the cluster, the bootstrap
command will perform an upgrade if needed.

```
flux bootstrap git [flags]
```

### Examples

```
  # Run bootstrap for a Git repository and authenticate with your SSH agent
  flux bootstrap git --url=ssh://git@example.com/repository.git

  # Run bootstrap for a Git repository and authenticate using a password
  flux bootstrap git --url=https://example.com/repository.git --password=<password>

  # Run bootstrap for a Git repository with a passwordless private key
  flux bootstrap git --url=ssh://git@example.com/repository.git --private-key-file=<path/to/private.key>

```

### Options

```
  -h, --help                    help for git
      --interval duration       sync interval (default 1m0s)
  -p, --password string         basic authentication password
      --path safeRelativePath   path relative to the repository root, when specified the cluster sync will be scoped to this path
      --url string              Git repository URL
  -u, --username string         basic authentication username (default "git")
```

### Options inherited from parent commands

```
      --author-email string                    author email for Git commits
      --author-name string                     author name for Git commits (default "Flux")
      --branch string                          Git branch (default "main")
      --ca-file string                         path to TLS CA file used for validating self-signed certificates
      --cluster-domain string                  internal cluster domain (default "cluster.local")
      --commit-message-appendix string         string to add to the commit messages, e.g. '[ci skip]'
      --components strings                     list of components, accepts comma-separated values (default [source-controller,kustomize-controller,helm-controller,notification-controller])
      --components-extra strings               list of components in addition to those supplied or defaulted, accepts comma-separated values
      --context string                         kubernetes context to use
      --image-pull-secret string               Kubernetes secret name used for pulling the toolkit images from a private registry
      --kubeconfig string                      absolute path to the kubeconfig file
      --log-level logLevel                     log level, available options are: (debug, info, error) (default info)
  -n, --namespace string                       the namespace scope for this operation (default "flux-system")
      --network-policy                         deny ingress access to the toolkit controllers from other namespaces using network policies (default true)
      --private-key-file string                path to a private key file used for authenticating to the Git SSH server
      --recurse-submodules                     when enabled, configures the GitRepository source to initialize and include Git submodules in the artifact it produces
      --registry string                        container registry where the toolkit images are published (default "ghcr.io/fluxcd")
      --secret-name string                     name of the secret the sync credentials can be found in or stored to (default "flux-system")
      --ssh-ecdsa-curve ecdsaCurve             SSH ECDSA public key curve (p256, p384, p521) (default p384)
      --ssh-hostname string                    SSH hostname, to be used when the SSH host differs from the HTTPS one
      --ssh-key-algorithm publicKeyAlgorithm   SSH public key algorithm (rsa, ecdsa, ed25519) (default rsa)
      --ssh-rsa-bits rsaKeyBits                SSH RSA public key bit size (multiplies of 8) (default 2048)
      --timeout duration                       timeout for this operation (default 5m0s)
      --token-auth                             when enabled, the personal access token will be used instead of SSH deploy key
      --toleration-keys strings                list of toleration keys used to schedule the components pods onto nodes with matching taints
      --verbose                                print generated objects
  -v, --version string                         toolkit version, when specified the manifests are downloaded from https://github.com/fluxcd/flux2/releases
      --watch-all-namespaces                   watch for custom resources in all namespaces, if set to false it will only watch the namespace where the toolkit is installed (default true)
```

### SEE ALSO

* [flux bootstrap](../flux_bootstrap/)	 - Bootstrap toolkit components

