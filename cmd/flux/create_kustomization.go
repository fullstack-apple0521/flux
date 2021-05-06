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
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	helmv2 "github.com/fluxcd/helm-controller/api/v2beta1"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1beta1"
	"github.com/fluxcd/pkg/apis/meta"

	"github.com/fluxcd/flux2/internal/flags"
	"github.com/fluxcd/flux2/internal/utils"
)

var createKsCmd = &cobra.Command{
	Use:     "kustomization [name]",
	Aliases: []string{"ks"},
	Short:   "Create or update a Kustomization resource",
	Long:    "The kustomization source create command generates a Kustomize resource for a given source.",
	Example: `  # Create a Kustomization resource from a source at a given path
  flux create kustomization contour \
    --source=GitRepository/contour \
    --path="./examples/contour/" \
    --prune=true \
    --interval=10m \
    --validation=client \
    --health-check="Deployment/contour.projectcontour" \
    --health-check="DaemonSet/envoy.projectcontour" \
    --health-check-timeout=3m

  # Create a Kustomization resource that depends on the previous one
  flux create kustomization webapp \
    --depends-on=contour \
    --source=GitRepository/webapp \
    --path="./deploy/overlays/dev" \
    --prune=true \
    --interval=5m \
    --validation=client

  # Create a Kustomization using a source from a different namespace
  flux create kustomization podinfo \
    --namespace=default \
    --source=GitRepository/podinfo.flux-system \
    --path="./deploy/overlays/dev" \
    --prune=true \
    --interval=5m \
    --validation=client

  # Create a Kustomization resource that references a Bucket
  flux create kustomization secrets \
    --source=Bucket/secrets \
    --prune=true \
    --interval=5m`,
	RunE: createKsCmdRun,
}

type kustomizationFlags struct {
	source             flags.KustomizationSource
	path               flags.SafeRelativePath
	prune              bool
	dependsOn          []string
	validation         string
	healthCheck        []string
	healthTimeout      time.Duration
	saName             string
	decryptionProvider flags.DecryptionProvider
	decryptionSecret   string
	targetNamespace    string
}

var kustomizationArgs = NewKustomizationFlags()

func init() {
	createKsCmd.Flags().Var(&kustomizationArgs.source, "source", kustomizationArgs.source.Description())
	createKsCmd.Flags().Var(&kustomizationArgs.path, "path", "path to the directory containing a kustomization.yaml file")
	createKsCmd.Flags().BoolVar(&kustomizationArgs.prune, "prune", false, "enable garbage collection")
	createKsCmd.Flags().StringSliceVar(&kustomizationArgs.healthCheck, "health-check", nil, "workload to be included in the health assessment, in the format '<kind>/<name>.<namespace>'")
	createKsCmd.Flags().DurationVar(&kustomizationArgs.healthTimeout, "health-check-timeout", 2*time.Minute, "timeout of health checking operations")
	createKsCmd.Flags().StringVar(&kustomizationArgs.validation, "validation", "", "validate the manifests before applying them on the cluster, can be 'client' or 'server'")
	createKsCmd.Flags().StringSliceVar(&kustomizationArgs.dependsOn, "depends-on", nil, "Kustomization that must be ready before this Kustomization can be applied, supported formats '<name>' and '<namespace>/<name>', also accepts comma-separated values")
	createKsCmd.Flags().StringVar(&kustomizationArgs.saName, "service-account", "", "the name of the service account to impersonate when reconciling this Kustomization")
	createKsCmd.Flags().Var(&kustomizationArgs.decryptionProvider, "decryption-provider", kustomizationArgs.decryptionProvider.Description())
	createKsCmd.Flags().StringVar(&kustomizationArgs.decryptionSecret, "decryption-secret", "", "set the Kubernetes secret name that contains the OpenPGP private keys used for sops decryption")
	createKsCmd.Flags().StringVar(&kustomizationArgs.targetNamespace, "target-namespace", "", "overrides the namespace of all Kustomization objects reconciled by this Kustomization")
	createCmd.AddCommand(createKsCmd)
}

func NewKustomizationFlags() kustomizationFlags {
	return kustomizationFlags{
		path: "./",
	}
}

func createKsCmdRun(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("Kustomization name is required")
	}
	name := args[0]

	if kustomizationArgs.path == "" {
		return fmt.Errorf("path is required")
	}
	if !strings.HasPrefix(kustomizationArgs.path.String(), "./") {
		return fmt.Errorf("path must begin with ./")
	}

	if !createArgs.export {
		logger.Generatef("generating Kustomization")
	}

	kslabels, err := parseLabels()
	if err != nil {
		return err
	}

	kustomization := kustomizev1.Kustomization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: rootArgs.namespace,
			Labels:    kslabels,
		},
		Spec: kustomizev1.KustomizationSpec{
			DependsOn: utils.MakeDependsOn(kustomizationArgs.dependsOn),
			Interval: metav1.Duration{
				Duration: createArgs.interval,
			},
			Path:  kustomizationArgs.path.ToSlash(),
			Prune: kustomizationArgs.prune,
			SourceRef: kustomizev1.CrossNamespaceSourceReference{
				Kind:      kustomizationArgs.source.Kind,
				Name:      kustomizationArgs.source.Name,
				Namespace: kustomizationArgs.source.Namespace,
			},
			Suspend:         false,
			Validation:      kustomizationArgs.validation,
			TargetNamespace: kustomizationArgs.targetNamespace,
		},
	}

	if len(kustomizationArgs.healthCheck) > 0 {
		healthChecks := make([]meta.NamespacedObjectKindReference, 0)
		for _, w := range kustomizationArgs.healthCheck {
			kindObj := strings.Split(w, "/")
			if len(kindObj) != 2 {
				return fmt.Errorf("invalid health check '%s' must be in the format 'kind/name.namespace' %v", w, kindObj)
			}
			kind := kindObj[0]

			//TODO: (stefan) extend this list with all the kstatus builtin kinds
			kinds := map[string]bool{
				"Deployment":           true,
				"DaemonSet":            true,
				"StatefulSet":          true,
				helmv2.HelmReleaseKind: true,
			}
			if !kinds[kind] {
				return fmt.Errorf("invalid health check kind '%s' can be HelmRelease, Deployment, DaemonSet or StatefulSet", kind)
			}
			nameNs := strings.Split(kindObj[1], ".")
			if len(nameNs) != 2 {
				return fmt.Errorf("invalid health check '%s' must be in the format 'kind/name.namespace'", w)
			}

			check := meta.NamespacedObjectKindReference{
				Kind:      kind,
				Name:      nameNs[0],
				Namespace: nameNs[1],
			}

			if kind == helmv2.HelmReleaseKind {
				check.APIVersion = helmv2.GroupVersion.String()
			}
			healthChecks = append(healthChecks, check)
		}
		kustomization.Spec.HealthChecks = healthChecks
		kustomization.Spec.Timeout = &metav1.Duration{
			Duration: kustomizationArgs.healthTimeout,
		}
	}

	if kustomizationArgs.saName != "" {
		kustomization.Spec.ServiceAccountName = kustomizationArgs.saName
	}

	if kustomizationArgs.decryptionProvider != "" {
		kustomization.Spec.Decryption = &kustomizev1.Decryption{
			Provider: kustomizationArgs.decryptionProvider.String(),
		}

		if kustomizationArgs.decryptionSecret != "" {
			kustomization.Spec.Decryption.SecretRef = &meta.LocalObjectReference{Name: kustomizationArgs.decryptionSecret}
		}
	}

	if createArgs.export {
		return printExport(exportKs(&kustomization))
	}

	ctx, cancel := context.WithTimeout(context.Background(), rootArgs.timeout)
	defer cancel()

	kubeClient, err := utils.KubeClient(rootArgs.kubeconfig, rootArgs.kubecontext)
	if err != nil {
		return err
	}

	logger.Actionf("applying Kustomization")
	namespacedName, err := upsertKustomization(ctx, kubeClient, &kustomization)
	if err != nil {
		return err
	}

	logger.Waitingf("waiting for Kustomization reconciliation")
	if err := wait.PollImmediate(rootArgs.pollInterval, rootArgs.timeout,
		isKustomizationReady(ctx, kubeClient, namespacedName, &kustomization)); err != nil {
		return err
	}
	logger.Successf("Kustomization %s is ready", name)

	logger.Successf("applied revision %s", kustomization.Status.LastAppliedRevision)
	return nil
}

func upsertKustomization(ctx context.Context, kubeClient client.Client,
	kustomization *kustomizev1.Kustomization) (types.NamespacedName, error) {
	namespacedName := types.NamespacedName{
		Namespace: kustomization.GetNamespace(),
		Name:      kustomization.GetName(),
	}

	var existing kustomizev1.Kustomization
	err := kubeClient.Get(ctx, namespacedName, &existing)
	if err != nil {
		if errors.IsNotFound(err) {
			if err := kubeClient.Create(ctx, kustomization); err != nil {
				return namespacedName, err
			} else {
				logger.Successf("Kustomization created")
				return namespacedName, nil
			}
		}
		return namespacedName, err
	}

	existing.Labels = kustomization.Labels
	existing.Spec = kustomization.Spec
	if err := kubeClient.Update(ctx, &existing); err != nil {
		return namespacedName, err
	}
	kustomization = &existing
	logger.Successf("Kustomization updated")
	return namespacedName, nil
}

func isKustomizationReady(ctx context.Context, kubeClient client.Client,
	namespacedName types.NamespacedName, kustomization *kustomizev1.Kustomization) wait.ConditionFunc {
	return func() (bool, error) {
		err := kubeClient.Get(ctx, namespacedName, kustomization)
		if err != nil {
			return false, err
		}

		// Confirm the state we are observing is for the current generation
		if kustomization.Generation != kustomization.Status.ObservedGeneration {
			return false, nil
		}

		if c := apimeta.FindStatusCondition(kustomization.Status.Conditions, meta.ReadyCondition); c != nil {
			switch c.Status {
			case metav1.ConditionTrue:
				return true, nil
			case metav1.ConditionFalse:
				return false, fmt.Errorf(c.Message)
			}
		}
		return false, nil
	}
}
