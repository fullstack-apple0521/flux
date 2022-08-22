/*
Copyright 2022 The Flux authors

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

	oci "github.com/fluxcd/pkg/oci/client"

	"github.com/fluxcd/flux2/pkg/printers"
)

type listArtifactFlags struct {
	semverFilter string
	regexFilter  string
}

var listArtifactArgs listArtifactFlags

var listArtifactsCmd = &cobra.Command{
	Use:   "artifacts",
	Short: "list artifacts",
	Long: `The list command fetches the tags and their metadata from a remote OCI repository.
The command uses the credentials from '~/.docker/config.json'.`,
	Example: `  # List the artifacts stored in an OCI repository
  flux list artifact oci://ghcr.io/org/config/app
`,
	RunE: listArtifactsCmdRun,
}

func init() {
	listArtifactsCmd.Flags().StringVar(&listArtifactArgs.semverFilter, "filter-semver", "", "filter tags returned from the oci repository using semver")
	listArtifactsCmd.Flags().StringVar(&listArtifactArgs.regexFilter, "filter-regex", "", "filter tags returned from the oci repository using regex")

	listCmd.AddCommand(listArtifactsCmd)
}

func listArtifactsCmdRun(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("artifact repository URL is required")
	}
	ociURL := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), rootArgs.timeout)
	defer cancel()

	ociClient := oci.NewLocalClient()
	url, err := oci.ParseArtifactURL(ociURL)
	if err != nil {
		return err
	}

	opts := oci.ListOptions{
		RegexFilter:  listArtifactArgs.regexFilter,
		SemverFilter: listArtifactArgs.semverFilter,
	}

	metas, err := ociClient.List(ctx, url, opts)
	if err != nil {
		return err
	}

	var rows [][]string
	for _, meta := range metas {
		rows = append(rows, []string{meta.URL, meta.Digest, meta.Source, meta.Revision})
	}

	err = printers.TablePrinter([]string{"artifact", "digest", "source", "revision"}).Print(cmd.OutOrStdout(), rows)
	if err != nil {
		return err
	}

	return nil
}
