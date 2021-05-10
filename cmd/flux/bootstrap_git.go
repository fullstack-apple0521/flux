/*
Copyright 2021 The Flux authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	cryptossh "golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"

	"github.com/fluxcd/flux2/internal/bootstrap"
	"github.com/fluxcd/flux2/internal/bootstrap/git/gogit"
	"github.com/fluxcd/flux2/internal/flags"
	"github.com/fluxcd/flux2/internal/utils"
	"github.com/fluxcd/flux2/pkg/manifestgen/install"
	"github.com/fluxcd/flux2/pkg/manifestgen/sourcesecret"
	"github.com/fluxcd/flux2/pkg/manifestgen/sync"
)

var bootstrapGitCmd = &cobra.Command{
	Use:   "git",
	Short: "Bootstrap toolkit components in a Git repository",
	Long: `The bootstrap git command commits the toolkit components manifests to the
branch of a Git repository. It then configures the target cluster to synchronize with
the repository. If the toolkit components are present on the cluster, the bootstrap
command will perform an upgrade if needed.`,
	Example: `  # Run bootstrap for a Git repository and authenticate with your SSH agent
  flux bootstrap git --url=ssh://git@example.com/repository.git

  # Run bootstrap for a Git repository and authenticate using a password
  flux bootstrap git --url=https://example.com/repository.git --password=<password>

  # Run bootstrap for a Git repository with a passwordless private key
  flux bootstrap git --url=ssh://git@example.com/repository.git --private-key-file=<path/to/private.key>

  # Run bootstrap for a Git repository with a private key and password
  flux bootstrap git --url=ssh://git@example.com/repository.git --private-key-file=<path/to/private.key> --password=<password>
`,
	RunE: bootstrapGitCmdRun,
}

type gitFlags struct {
	url      string
	interval time.Duration
	path     flags.SafeRelativePath
	username string
	password string
}

var gitArgs gitFlags

func init() {
	bootstrapGitCmd.Flags().StringVar(&gitArgs.url, "url", "", "Git repository URL")
	bootstrapGitCmd.Flags().DurationVar(&gitArgs.interval, "interval", time.Minute, "sync interval")
	bootstrapGitCmd.Flags().Var(&gitArgs.path, "path", "path relative to the repository root, when specified the cluster sync will be scoped to this path")
	bootstrapGitCmd.Flags().StringVarP(&gitArgs.username, "username", "u", "git", "basic authentication username")
	bootstrapGitCmd.Flags().StringVarP(&gitArgs.password, "password", "p", "", "basic authentication password")

	bootstrapCmd.AddCommand(bootstrapGitCmd)
}

func bootstrapGitCmdRun(cmd *cobra.Command, args []string) error {
	if err := bootstrapValidate(); err != nil {
		return err
	}

	repositoryURL, err := url.Parse(gitArgs.url)
	if err != nil {
		return err
	}
	gitAuth, err := transportForURL(repositoryURL)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), rootArgs.timeout)
	defer cancel()

	kubeClient, err := utils.KubeClient(rootArgs.kubeconfig, rootArgs.kubecontext)
	if err != nil {
		return err
	}

	// Manifest base
	if ver, err := getVersion(bootstrapArgs.version); err == nil {
		bootstrapArgs.version = ver
	}
	manifestsBase, err := buildEmbeddedManifestBase()
	if err != nil {
		return err
	}
	defer os.RemoveAll(manifestsBase)

	// Lazy go-git repository
	tmpDir, err := ioutil.TempDir("", "flux-bootstrap-")
	if err != nil {
		return fmt.Errorf("failed to create temporary working dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	gitClient := gogit.New(tmpDir, gitAuth)

	// Install manifest config
	installOptions := install.Options{
		BaseURL:                rootArgs.defaults.BaseURL,
		Version:                bootstrapArgs.version,
		Namespace:              rootArgs.namespace,
		Components:             bootstrapComponents(),
		Registry:               bootstrapArgs.registry,
		ImagePullSecret:        bootstrapArgs.imagePullSecret,
		WatchAllNamespaces:     bootstrapArgs.watchAllNamespaces,
		NetworkPolicy:          bootstrapArgs.networkPolicy,
		LogLevel:               bootstrapArgs.logLevel.String(),
		NotificationController: rootArgs.defaults.NotificationController,
		ManifestFile:           rootArgs.defaults.ManifestFile,
		Timeout:                rootArgs.timeout,
		TargetPath:             gitArgs.path.ToSlash(),
		ClusterDomain:          bootstrapArgs.clusterDomain,
		TolerationKeys:         bootstrapArgs.tolerationKeys,
	}
	if customBaseURL := bootstrapArgs.manifestsPath; customBaseURL != "" {
		installOptions.BaseURL = customBaseURL
	}

	// Source generation and secret config
	secretOpts := sourcesecret.Options{
		Name:         bootstrapArgs.secretName,
		Namespace:    rootArgs.namespace,
		TargetPath:   gitArgs.path.String(),
		ManifestFile: sourcesecret.MakeDefaultOptions().ManifestFile,
	}
	if bootstrapArgs.tokenAuth {
		secretOpts.Username = gitArgs.username
		secretOpts.Password = gitArgs.password

		if bootstrapArgs.caFile != "" {
			secretOpts.CAFilePath = bootstrapArgs.caFile
		}

		// Configure repository URL to match auth config for sync.
		repositoryURL.User = nil
		repositoryURL.Scheme = "https"
		repositoryURL.Host = repositoryURL.Hostname()
	} else {
		secretOpts.PrivateKeyAlgorithm = sourcesecret.PrivateKeyAlgorithm(bootstrapArgs.keyAlgorithm)
		secretOpts.Password = gitArgs.password
		secretOpts.RSAKeyBits = int(bootstrapArgs.keyRSABits)
		secretOpts.ECDSACurve = bootstrapArgs.keyECDSACurve.Curve

		// Configure repository URL to match auth config for sync.
		repositoryURL.User = url.User(gitArgs.username)
		repositoryURL.Scheme = "ssh"
		if bootstrapArgs.sshHostname != "" {
			repositoryURL.Host = bootstrapArgs.sshHostname
		}
		if bootstrapArgs.privateKeyFile != "" {
			secretOpts.PrivateKeyPath = bootstrapArgs.privateKeyFile
		}

		// Configure last as it depends on the config above.
		secretOpts.SSHHostname = repositoryURL.Host
	}

	// Sync manifest config
	syncOpts := sync.Options{
		Interval:          gitArgs.interval,
		Name:              rootArgs.namespace,
		Namespace:         rootArgs.namespace,
		URL:               repositoryURL.String(),
		Branch:            bootstrapArgs.branch,
		Secret:            bootstrapArgs.secretName,
		TargetPath:        gitArgs.path.ToSlash(),
		ManifestFile:      sync.MakeDefaultOptions().ManifestFile,
		GitImplementation: sourceGitArgs.gitImplementation.String(),
		RecurseSubmodules: bootstrapArgs.recurseSubmodules,
	}

	// Bootstrap config
	bootstrapOpts := []bootstrap.GitOption{
		bootstrap.WithRepositoryURL(gitArgs.url),
		bootstrap.WithBranch(bootstrapArgs.branch),
		bootstrap.WithAuthor(bootstrapArgs.authorName, bootstrapArgs.authorEmail),
		bootstrap.WithCommitMessageAppendix(bootstrapArgs.commitMessageAppendix),
		bootstrap.WithKubeconfig(rootArgs.kubeconfig, rootArgs.kubecontext),
		bootstrap.WithPostGenerateSecretFunc(promptPublicKey),
		bootstrap.WithLogger(logger),
	}

	// Setup bootstrapper with constructed configs
	b, err := bootstrap.NewPlainGitProvider(gitClient, kubeClient, bootstrapOpts...)
	if err != nil {
		return err
	}

	// Run
	return bootstrap.Run(ctx, b, manifestsBase, installOptions, secretOpts, syncOpts, rootArgs.pollInterval, rootArgs.timeout)
}

// transportForURL constructs a transport.AuthMethod based on the scheme
// of the given URL and the configured flags. If the protocol equals
// "ssh" but no private key is configured, authentication using the local
// SSH-agent is attempted.
func transportForURL(u *url.URL) (transport.AuthMethod, error) {
	switch u.Scheme {
	case "https":
		return &http.BasicAuth{
			Username: gitArgs.username,
			Password: gitArgs.password,
		}, nil
	case "ssh":
		if bootstrapArgs.privateKeyFile != "" {
			// TODO(hidde): replace custom logic with https://github.com/go-git/go-git/pull/298
			//  once made available in go-git release.
			bytes, err := ioutil.ReadFile(bootstrapArgs.privateKeyFile)
			if err != nil {
				return nil, err
			}
			signer, err := cryptossh.ParsePrivateKey(bytes)
			if _, ok := err.(*cryptossh.PassphraseMissingError); ok {
				signer, err = cryptossh.ParsePrivateKeyWithPassphrase(bytes, []byte(gitArgs.password))
			}
			if err != nil {
				return nil, err
			}
			return &ssh.PublicKeys{Signer: signer, User: u.User.Username()}, nil
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("scheme %q is not supported", u.Scheme)
	}
}

func promptPublicKey(ctx context.Context, secret corev1.Secret, _ sourcesecret.Options) error {
	ppk, ok := secret.StringData[sourcesecret.PublicKeySecretKey]
	if !ok {
		return nil
	}

	logger.Successf("public key: %s", strings.TrimSpace(ppk))
	prompt := promptui.Prompt{
		Label:     "Please give the key access to your repository",
		IsConfirm: true,
	}
	_, err := prompt.Run()
	if err != nil {
		return fmt.Errorf("aborting")
	}
	return nil
}
