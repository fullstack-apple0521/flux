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
	"time"

	"github.com/spf13/cobra"
	apimeta "k8s.io/apimachinery/pkg/api/meta"

	autov1 "github.com/fluxcd/image-automation-controller/api/v1alpha2"
	meta "github.com/fluxcd/pkg/apis/meta"
)

var reconcileImageUpdateCmd = &cobra.Command{
	Use:   "update [name]",
	Short: "Reconcile an ImageUpdateAutomation",
	Long:  `The reconcile image update command triggers a reconciliation of an ImageUpdateAutomation resource and waits for it to finish.`,
	Example: `  # Trigger an automation run for an existing image update automation
  flux reconcile image update latest-images`,
	RunE: reconcileCommand{
		apiType: imageUpdateAutomationType,
		object:  imageUpdateAutomationAdapter{&autov1.ImageUpdateAutomation{}},
	}.run,
}

func init() {
	reconcileImageCmd.AddCommand(reconcileImageUpdateCmd)
}

func (obj imageUpdateAutomationAdapter) suspended() bool {
	return obj.ImageUpdateAutomation.Spec.Suspend
}

func (obj imageUpdateAutomationAdapter) lastHandledReconcileRequest() string {
	return obj.Status.GetLastHandledReconcileRequest()
}

func (obj imageUpdateAutomationAdapter) successMessage() string {
	if rc := apimeta.FindStatusCondition(obj.Status.Conditions, meta.ReadyCondition); rc != nil {
		return rc.Message
	}
	if obj.Status.LastAutomationRunTime != nil {
		return "last run " + obj.Status.LastAutomationRunTime.Time.Format(time.RFC3339)
	}
	return "automation not yet run"
}
