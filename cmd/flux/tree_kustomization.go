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
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/fluxcd/flux2/internal/tree"
	"github.com/fluxcd/flux2/internal/utils"
	helmv2 "github.com/fluxcd/helm-controller/api/v2beta1"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1beta2"
	"github.com/fluxcd/pkg/ssa"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

var treeKsCmd = &cobra.Command{
	Use:     "kustomization [name]",
	Aliases: []string{"ks", "kustomization"},
	Short:   "Print the resource inventory of a Kustomization",
	Long:    `The tree command prints the resource list reconciled by a Kustomization.'`,
	Example: `  # Print the resources managed by the root Kustomization
  flux tree kustomization flux-system

  # Print the Flux resources managed by the root Kustomization
  flux tree kustomization flux-system --compact`,
	RunE:              treeKsCmdRun,
	ValidArgsFunction: resourceNamesCompletionFunc(kustomizev1.GroupVersion.WithKind(kustomizev1.KustomizationKind)),
}

type TreeKsFlags struct {
	compact bool
	output  string
}

var treeKsArgs TreeKsFlags

func init() {
	treeKsCmd.Flags().BoolVar(&treeKsArgs.compact, "compact", false, "list Flux resources only.")
	treeKsCmd.Flags().StringVarP(&treeKsArgs.output, "output", "o", "",
		"the format in which the tree should be printed. can be 'json' or 'yaml'")
	treeCmd.AddCommand(treeKsCmd)
}

func treeKsCmdRun(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("kustomization name is required")
	}
	name := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), rootArgs.timeout)
	defer cancel()

	kubeClient, err := utils.KubeClient(rootArgs.kubeconfig, rootArgs.kubecontext)
	if err != nil {
		return err
	}

	k := &kustomizev1.Kustomization{}
	err = kubeClient.Get(ctx, client.ObjectKey{
		Namespace: rootArgs.namespace,
		Name:      name,
	}, k)
	if err != nil {
		return err
	}

	kMeta, err := object.CreateObjMetadata(k.Namespace, k.Name,
		schema.GroupKind{Group: kustomizev1.GroupVersion.Group, Kind: kustomizev1.KustomizationKind})
	if err != nil {
		return err
	}

	kTree := tree.New(kMeta)
	err = treeKustomization(ctx, kTree, k, kubeClient, treeKsArgs.compact)
	if err != nil {
		return err
	}

	switch treeKsArgs.output {
	case "json":
		data, err := json.MarshalIndent(kTree, "", "  ")
		if err != nil {
			return err
		}
		rootCmd.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(kTree)
		if err != nil {
			return err
		}
		rootCmd.Println(string(data))
	default:
		rootCmd.Println(kTree.Print())
	}

	return nil
}

func treeKustomization(ctx context.Context, tree tree.ObjMetadataTree, item *kustomizev1.Kustomization, kubeClient client.Client, compact bool) error {
	if item.Status.Inventory == nil || len(item.Status.Inventory.Entries) == 0 {
		return nil
	}

	compactGroup := "toolkit.fluxcd.io"

	for _, entry := range item.Status.Inventory.Entries {
		objMetadata, err := object.ParseObjMetadata(entry.ID)
		if err != nil {
			return err
		}

		if compact && !strings.Contains(objMetadata.GroupKind.Group, compactGroup) {
			continue
		}

		if objMetadata.GroupKind.Group == kustomizev1.GroupVersion.Group &&
			objMetadata.GroupKind.Kind == kustomizev1.KustomizationKind &&
			objMetadata.Namespace == item.Namespace &&
			objMetadata.Name == item.Name {
			continue
		}

		ks := tree.Add(objMetadata)

		if objMetadata.GroupKind.Group == helmv2.GroupVersion.Group &&
			objMetadata.GroupKind.Kind == helmv2.HelmReleaseKind {
			objects, err := getHelmReleaseInventory(
				ctx, client.ObjectKey{
					Namespace: objMetadata.Namespace,
					Name:      objMetadata.Name,
				}, kubeClient)
			if err != nil {
				return err
			}

			for _, obj := range objects {
				if compact && !strings.Contains(obj.GroupKind.Group, compactGroup) {
					continue
				}
				ks.Add(obj)
			}
		}

		if objMetadata.GroupKind.Group == kustomizev1.GroupVersion.Group &&
			objMetadata.GroupKind.Kind == kustomizev1.KustomizationKind {
			k := &kustomizev1.Kustomization{}
			err = kubeClient.Get(ctx, client.ObjectKey{
				Namespace: objMetadata.Namespace,
				Name:      objMetadata.Name,
			}, k)
			if err != nil {
				return fmt.Errorf("failed to find object: %w", err)
			}
			err := treeKustomization(ctx, ks, k, kubeClient, compact)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

type hrStorage struct {
	Name     string `json:"name,omitempty"`
	Manifest string `json:"manifest,omitempty"`
}

func getHelmReleaseInventory(ctx context.Context, objectKey client.ObjectKey, kubeClient client.Client) ([]object.ObjMetadata, error) {
	hr := &helmv2.HelmRelease{}
	if err := kubeClient.Get(ctx, objectKey, hr); err != nil {
		return nil, err
	}

	storageNamespace := hr.GetNamespace()
	if hr.Spec.StorageNamespace != "" {
		storageNamespace = hr.Spec.StorageNamespace
	}

	storageName := hr.GetName()
	if hr.Spec.ReleaseName != "" {
		storageName = hr.Spec.ReleaseName
	} else if hr.Spec.TargetNamespace != "" {
		storageName = strings.Join([]string{hr.Spec.TargetNamespace, hr.Name}, "-")
	}

	storageVersion := hr.Status.LastReleaseRevision
	// skip release if it failed to install
	if storageVersion < 1 {
		return nil, nil
	}

	storageKey := client.ObjectKey{
		Namespace: storageNamespace,
		Name:      fmt.Sprintf("sh.helm.release.v1.%s.v%v", storageName, storageVersion),
	}

	storageSecret := &corev1.Secret{}
	if err := kubeClient.Get(ctx, storageKey, storageSecret); err != nil {
		// skip release if it has no storage
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find the Helm storage object for HelmRelease '%s': %w", objectKey.String(), err)
	}

	releaseData, releaseFound := storageSecret.Data["release"]
	if !releaseFound {
		return nil, fmt.Errorf("failed to decode the Helm storage object for HelmRelease '%s'", objectKey.String())
	}

	// adapted from https://github.com/helm/helm/blob/02685e94bd3862afcb44f6cd7716dbeb69743567/pkg/storage/driver/util.go
	var b64 = base64.StdEncoding
	b, err := b64.DecodeString(string(releaseData))
	if err != nil {
		return nil, err
	}
	var magicGzip = []byte{0x1f, 0x8b, 0x08}
	if bytes.Equal(b[0:3], magicGzip) {
		r, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		b2, err := ioutil.ReadAll(r)
		if err != nil {
			return nil, err
		}
		b = b2
	}

	var rls hrStorage
	if err := json.Unmarshal(b, &rls); err != nil {
		return nil, fmt.Errorf("failed to decode the Helm storage object for HelmRelease '%s': %w", objectKey.String(), err)
	}

	objects, err := ssa.ReadObjects(strings.NewReader(rls.Manifest))
	if err != nil {
		return nil, fmt.Errorf("failed to read the Helm storage object for HelmRelease '%s': %w", objectKey.String(), err)
	}

	return object.UnstructuredsToObjMetas(objects)
}
