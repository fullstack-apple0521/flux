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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/toolkit/internal/utils"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1beta1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta1"
)

var reconcileKsCmd = &cobra.Command{
	Use:     "kustomization [name]",
	Aliases: []string{"ks"},
	Short:   "Reconcile a Kustomization resource",
	Long: `
The reconcile kustomization command triggers a reconciliation of a Kustomization resource and waits for it to finish.`,
	Example: `  # Trigger a Kustomization apply outside of the reconciliation interval
  flux reconcile kustomization podinfo

  # Trigger a sync of the Kustomization's source and apply changes
  flux reconcile kustomization podinfo --with-source
`,
	RunE: reconcileKsCmdRun,
}

var (
	syncKsWithSource bool
)

func init() {
	reconcileKsCmd.Flags().BoolVar(&syncKsWithSource, "with-source", false, "reconcile Kustomization source")

	reconcileCmd.AddCommand(reconcileKsCmd)
}

func reconcileKsCmdRun(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("Kustomization name is required")
	}
	name := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	kubeClient, err := utils.KubeClient(kubeconfig)
	if err != nil {
		return err
	}

	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	var kustomization kustomizev1.Kustomization
	err = kubeClient.Get(ctx, namespacedName, &kustomization)
	if err != nil {
		return err
	}

	if syncKsWithSource {
		switch kustomization.Spec.SourceRef.Kind {
		case sourcev1.GitRepositoryKind:
			err = reconcileSourceGitCmdRun(nil, []string{kustomization.Spec.SourceRef.Name})
		case sourcev1.BucketKind:
			err = reconcileSourceBucketCmdRun(nil, []string{kustomization.Spec.SourceRef.Name})
		}
		if err != nil {
			return err
		}
	}

	lastHandledReconcileAt := kustomization.Status.LastHandledReconcileAt
	logger.Actionf("annotating Kustomization %s in %s namespace", name, namespace)
	if err := requestKustomizeReconciliation(ctx, kubeClient, namespacedName, &kustomization); err != nil {
		return err
	}
	logger.Successf("Kustomization annotated")

	logger.Waitingf("waiting for Kustomization reconciliation")
	if err := wait.PollImmediate(
		pollInterval, timeout,
		kustomizeReconciliationHandled(ctx, kubeClient, namespacedName, &kustomization, lastHandledReconcileAt),
	); err != nil {
		return err
	}
	logger.Successf("Kustomization reconciliation completed")

	if c := meta.GetCondition(kustomization.Status.Conditions, meta.ReadyCondition); c != nil {
		switch c.Status {
		case corev1.ConditionFalse:
			return fmt.Errorf("Kustomization reconciliation failed")
		default:
			logger.Successf("reconciled revision %s", kustomization.Status.LastAppliedRevision)
		}
	}
	return nil
}

func kustomizeReconciliationHandled(ctx context.Context, kubeClient client.Client,
	namespacedName types.NamespacedName, kustomization *kustomizev1.Kustomization, lastHandledReconcileAt string) wait.ConditionFunc {
	return func() (bool, error) {
		err := kubeClient.Get(ctx, namespacedName, kustomization)
		if err != nil {
			return false, err
		}
		return kustomization.Status.LastHandledReconcileAt != lastHandledReconcileAt, nil
	}
}

func requestKustomizeReconciliation(ctx context.Context, kubeClient client.Client,
	namespacedName types.NamespacedName, kustomization *kustomizev1.Kustomization) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		if err := kubeClient.Get(ctx, namespacedName, kustomization); err != nil {
			return err
		}
		if kustomization.Annotations == nil {
			kustomization.Annotations = map[string]string{
				meta.ReconcileAtAnnotation: time.Now().Format(time.RFC3339Nano),
			}
		} else {
			kustomization.Annotations[meta.ReconcileAtAnnotation] = time.Now().Format(time.RFC3339Nano)
		}
		return kubeClient.Update(ctx, kustomization)
	})
}
