/*
Copyright 2020 The Flux authors

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
	"crypto/elliptic"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/fluxcd/flux2/internal/flags"
	"github.com/fluxcd/flux2/internal/utils"
	"github.com/fluxcd/flux2/pkg/manifestgen/sourcesecret"
)

var createSecretGitCmd = &cobra.Command{
	Use:   "git [name]",
	Short: "Create or update a Kubernetes secret for Git authentication",
	Long: `The create secret git command generates a Kubernetes secret with Git credentials.
For Git over SSH, the host and SSH keys are automatically generated and stored in the secret.
For Git over HTTP/S, the provided basic authentication credentials are stored in the secret.`,
	Example: `  # Create a Git SSH authentication secret using an ECDSA P-521 curve public key

  flux create secret git podinfo-auth \
    --url=ssh://git@github.com/stefanprodan/podinfo \
    --ssh-key-algorithm=ecdsa \
    --ssh-ecdsa-curve=p521

  # Create a Git SSH authentication secret with a passwordless private key from file
  # The public SSH host key will still be gathered from the host
  flux create secret git podinfo-auth \
    --url=ssh://git@github.com/stefanprodan/podinfo \
    --private-key-file=./private.key

  # Create a Git SSH authentication secret with a passworded private key from file
  # The public SSH host key will still be gathered from the host
  flux create secret git podinfo-auth \
    --url=ssh://git@github.com/stefanprodan/podinfo \
    --private-key-file=./private.key \
    --password=<password>

  # Create a secret for a Git repository using basic authentication
  flux create secret git podinfo-auth \
    --url=https://github.com/stefanprodan/podinfo \
    --username=username \
    --password=password

  # Create a Git SSH secret on disk and print the deploy key
  flux create secret git podinfo-auth \
    --url=ssh://git@github.com/stefanprodan/podinfo \
    --export > podinfo-auth.yaml

  yq read podinfo-auth.yaml 'data."identity.pub"' | base64 --decode

  # Create a Git SSH secret on disk and encrypt it with Mozilla SOPS
  flux create secret git podinfo-auth \
    --namespace=apps \
    --url=ssh://git@github.com/stefanprodan/podinfo \
    --export > podinfo-auth.yaml

  sops --encrypt --encrypted-regex '^(data|stringData)$' \
    --in-place podinfo-auth.yaml`,
	RunE: createSecretGitCmdRun,
}

type secretGitFlags struct {
	url            string
	username       string
	password       string
	keyAlgorithm   flags.PublicKeyAlgorithm
	rsaBits        flags.RSAKeyBits
	ecdsaCurve     flags.ECDSACurve
	caFile         string
	privateKeyFile string
}

var secretGitArgs = NewSecretGitFlags()

func init() {
	createSecretGitCmd.Flags().StringVar(&secretGitArgs.url, "url", "", "git address, e.g. ssh://git@host/org/repository")
	createSecretGitCmd.Flags().StringVarP(&secretGitArgs.username, "username", "u", "", "basic authentication username")
	createSecretGitCmd.Flags().StringVarP(&secretGitArgs.password, "password", "p", "", "basic authentication password")
	createSecretGitCmd.Flags().Var(&secretGitArgs.keyAlgorithm, "ssh-key-algorithm", secretGitArgs.keyAlgorithm.Description())
	createSecretGitCmd.Flags().Var(&secretGitArgs.rsaBits, "ssh-rsa-bits", secretGitArgs.rsaBits.Description())
	createSecretGitCmd.Flags().Var(&secretGitArgs.ecdsaCurve, "ssh-ecdsa-curve", secretGitArgs.ecdsaCurve.Description())
	createSecretGitCmd.Flags().StringVar(&secretGitArgs.caFile, "ca-file", "", "path to TLS CA file used for validating self-signed certificates")
	createSecretGitCmd.Flags().StringVar(&secretGitArgs.privateKeyFile, "private-key-file", "", "path to a passwordless private key file used for authenticating to the Git SSH server")

	createSecretCmd.AddCommand(createSecretGitCmd)
}

func NewSecretGitFlags() secretGitFlags {
	return secretGitFlags{
		keyAlgorithm: flags.PublicKeyAlgorithm(sourcesecret.RSAPrivateKeyAlgorithm),
		rsaBits:      2048,
		ecdsaCurve:   flags.ECDSACurve{Curve: elliptic.P384()},
	}
}

func createSecretGitCmdRun(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("secret name is required")
	}
	name := args[0]
	if secretGitArgs.url == "" {
		return fmt.Errorf("url is required")
	}

	u, err := url.Parse(secretGitArgs.url)
	if err != nil {
		return fmt.Errorf("git URL parse failed: %w", err)
	}

	labels, err := parseLabels()
	if err != nil {
		return err
	}

	opts := sourcesecret.Options{
		Name:         name,
		Namespace:    rootArgs.namespace,
		Labels:       labels,
		ManifestFile: sourcesecret.MakeDefaultOptions().ManifestFile,
	}
	switch u.Scheme {
	case "ssh":
		opts.SSHHostname = u.Host
		opts.PrivateKeyPath = secretGitArgs.privateKeyFile
		opts.PrivateKeyAlgorithm = sourcesecret.PrivateKeyAlgorithm(secretGitArgs.keyAlgorithm)
		opts.RSAKeyBits = int(secretGitArgs.rsaBits)
		opts.ECDSACurve = secretGitArgs.ecdsaCurve.Curve
		opts.Password = secretGitArgs.password
	case "http", "https":
		if secretGitArgs.username == "" || secretGitArgs.password == "" {
			return fmt.Errorf("for Git over HTTP/S the username and password are required")
		}
		opts.Username = secretGitArgs.username
		opts.Password = secretGitArgs.password
		opts.CAFilePath = secretGitArgs.caFile
	default:
		return fmt.Errorf("git URL scheme '%s' not supported, can be: ssh, http and https", u.Scheme)
	}

	secret, err := sourcesecret.Generate(opts)
	if err != nil {
		return err
	}

	if createArgs.export {
		fmt.Println(secret.Content)
		return nil
	}

	var s corev1.Secret
	if err := yaml.Unmarshal([]byte(secret.Content), &s); err != nil {
		return err
	}

	if ppk, ok := s.StringData[sourcesecret.PublicKeySecretKey]; ok {
		logger.Generatef("deploy key: %s", ppk)
	}

	ctx, cancel := context.WithTimeout(context.Background(), rootArgs.timeout)
	defer cancel()
	kubeClient, err := utils.KubeClient(rootArgs.kubeconfig, rootArgs.kubecontext)
	if err != nil {
		return err
	}
	if err := upsertSecret(ctx, kubeClient, s); err != nil {
		return err
	}
	logger.Actionf("secret '%s' created in '%s' namespace", name, rootArgs.namespace)

	return nil
}
