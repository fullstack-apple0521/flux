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

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	notificationv1 "github.com/fluxcd/notification-controller/api/v1beta1"
	"github.com/fluxcd/pkg/apis/meta"

	"github.com/fluxcd/flux2/internal/utils"
)

var createReceiverCmd = &cobra.Command{
	Use:   "receiver [name]",
	Short: "Create or update a Receiver resource",
	Long:  "The create receiver command generates a Receiver resource.",
	Example: `  # Create a Receiver
  flux create receiver github-receiver \
	--type github \
	--event ping \
	--event push \
	--secret-ref webhook-token \
	--resource GitRepository/webapp \
	--resource HelmRepository/webapp`,
	RunE: createReceiverCmdRun,
}

type receiverFlags struct {
	receiverType string
	secretRef    string
	events       []string
	resources    []string
}

var receiverArgs receiverFlags

func init() {
	createReceiverCmd.Flags().StringVar(&receiverArgs.receiverType, "type", "", "")
	createReceiverCmd.Flags().StringVar(&receiverArgs.secretRef, "secret-ref", "", "")
	createReceiverCmd.Flags().StringSliceVar(&receiverArgs.events, "event", []string{}, "also accepts comma-separated values")
	createReceiverCmd.Flags().StringSliceVar(&receiverArgs.resources, "resource", []string{}, "also accepts comma-separated values")
	createCmd.AddCommand(createReceiverCmd)
}

func createReceiverCmdRun(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("Receiver name is required")
	}
	name := args[0]

	if receiverArgs.receiverType == "" {
		return fmt.Errorf("Receiver type is required")
	}

	if receiverArgs.secretRef == "" {
		return fmt.Errorf("secret ref is required")
	}

	resources := []notificationv1.CrossNamespaceObjectReference{}
	for _, resource := range receiverArgs.resources {
		kind, name := utils.ParseObjectKindName(resource)
		if kind == "" {
			return fmt.Errorf("invalid event source '%s', must be in format <kind>/<name>", resource)
		}

		resources = append(resources, notificationv1.CrossNamespaceObjectReference{
			Kind: kind,
			Name: name,
		})
	}

	if len(resources) == 0 {
		return fmt.Errorf("atleast one resource is required")
	}

	sourceLabels, err := parseLabels()
	if err != nil {
		return err
	}

	if !createArgs.export {
		logger.Generatef("generating Receiver")
	}

	receiver := notificationv1.Receiver{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: rootArgs.namespace,
			Labels:    sourceLabels,
		},
		Spec: notificationv1.ReceiverSpec{
			Type:      receiverArgs.receiverType,
			Events:    receiverArgs.events,
			Resources: resources,
			SecretRef: meta.LocalObjectReference{
				Name: receiverArgs.secretRef,
			},
			Suspend: false,
		},
	}

	if createArgs.export {
		return printExport(exportReceiver(&receiver))
	}

	ctx, cancel := context.WithTimeout(context.Background(), rootArgs.timeout)
	defer cancel()

	kubeClient, err := utils.KubeClient(rootArgs.kubeconfig, rootArgs.kubecontext)
	if err != nil {
		return err
	}

	logger.Actionf("applying Receiver")
	namespacedName, err := upsertReceiver(ctx, kubeClient, &receiver)
	if err != nil {
		return err
	}

	logger.Waitingf("waiting for Receiver reconciliation")
	if err := wait.PollImmediate(rootArgs.pollInterval, rootArgs.timeout,
		isReceiverReady(ctx, kubeClient, namespacedName, &receiver)); err != nil {
		return err
	}
	logger.Successf("Receiver %s is ready", name)

	logger.Successf("generated webhook URL %s", receiver.Status.URL)
	return nil
}

func upsertReceiver(ctx context.Context, kubeClient client.Client,
	receiver *notificationv1.Receiver) (types.NamespacedName, error) {
	namespacedName := types.NamespacedName{
		Namespace: receiver.GetNamespace(),
		Name:      receiver.GetName(),
	}

	var existing notificationv1.Receiver
	err := kubeClient.Get(ctx, namespacedName, &existing)
	if err != nil {
		if errors.IsNotFound(err) {
			if err := kubeClient.Create(ctx, receiver); err != nil {
				return namespacedName, err
			} else {
				logger.Successf("Receiver created")
				return namespacedName, nil
			}
		}
		return namespacedName, err
	}

	existing.Labels = receiver.Labels
	existing.Spec = receiver.Spec
	if err := kubeClient.Update(ctx, &existing); err != nil {
		return namespacedName, err
	}
	receiver = &existing
	logger.Successf("Receiver updated")
	return namespacedName, nil
}

func isReceiverReady(ctx context.Context, kubeClient client.Client,
	namespacedName types.NamespacedName, receiver *notificationv1.Receiver) wait.ConditionFunc {
	return func() (bool, error) {
		err := kubeClient.Get(ctx, namespacedName, receiver)
		if err != nil {
			return false, err
		}

		if c := apimeta.FindStatusCondition(receiver.Status.Conditions, meta.ReadyCondition); c != nil {
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
