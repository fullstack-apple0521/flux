/*
Copyright 2020, 2021 The Flux authors

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

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/fluxcd/flux2/internal/utils"
	"github.com/fluxcd/flux2/pkg/manifestgen/sourcesecret"
)

var createSecretTLSCmd = &cobra.Command{
	Use:   "tls [name]",
	Short: "Create or update a Kubernetes secret with TLS certificates",
	Long:  `The create secret tls command generates a Kubernetes secret with certificates for use with TLS.`,
	Example: ` # Create a TLS secret on disk and encrypt it with Mozilla SOPS.
  # Files are expected to be PEM-encoded.
  flux create secret tls certs \
    --namespace=my-namespace \
    --cert-file=./client.crt \
    --key-file=./client.key \
    --export > certs.yaml

  sops --encrypt --encrypted-regex '^(data|stringData)$' \
    --in-place certs.yaml`,
	RunE: createSecretTLSCmdRun,
}

type secretTLSFlags struct {
	certFile string
	keyFile  string
	caFile   string
}

var secretTLSArgs secretTLSFlags

func initSecretTLSFlags(flags *pflag.FlagSet, args *secretTLSFlags) {
	flags.StringVar(&args.certFile, "cert-file", "", "TLS authentication cert file path")
	flags.StringVar(&args.keyFile, "key-file", "", "TLS authentication key file path")
	flags.StringVar(&args.caFile, "ca-file", "", "TLS authentication CA file path")
}

func init() {
	flags := createSecretTLSCmd.Flags()
	initSecretTLSFlags(flags, &secretTLSArgs)
	createSecretCmd.AddCommand(createSecretTLSCmd)
}

func createSecretTLSCmdRun(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("secret name is required")
	}
	name := args[0]

	labels, err := parseLabels()
	if err != nil {
		return err
	}

	opts := sourcesecret.Options{
		Name:         name,
		Namespace:    rootArgs.namespace,
		Labels:       labels,
		CAFilePath:   secretTLSArgs.caFile,
		CertFilePath: secretTLSArgs.certFile,
		KeyFilePath:  secretTLSArgs.keyFile,
	}
	secret, err := sourcesecret.Generate(opts)
	if err != nil {
		return err
	}

	if createArgs.export {
		fmt.Println(secret.Content)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), rootArgs.timeout)
	defer cancel()
	kubeClient, err := utils.KubeClient(rootArgs.kubeconfig, rootArgs.kubecontext)
	if err != nil {
		return err
	}
	var s corev1.Secret
	if err := yaml.Unmarshal([]byte(secret.Content), &s); err != nil {
		return err
	}
	if err := upsertSecret(ctx, kubeClient, s); err != nil {
		return err
	}

	return nil
}
