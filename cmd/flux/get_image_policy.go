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
	"github.com/spf13/cobra"

	imagev1 "github.com/fluxcd/image-reflector-controller/api/v1alpha1"
)

var getImagePolicyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Get ImagePolicy status",
	Long:  "The get image policy command prints the status of ImagePolicy objects.",
	Example: `  # List all image policies and their status
  flux get image policy

 # List image policies from all namespaces
  flux get image policy --all-namespaces`,
	RunE: getCommand{
		apiType: imagePolicyType,
		list:    &imagePolicyListAdapter{&imagev1.ImagePolicyList{}},
	}.run,
}

func init() {
	getImageCmd.AddCommand(getImagePolicyCmd)
}

func (s imagePolicyListAdapter) summariseItem(i int, includeNamespace bool, includeKind bool) []string {
	item := s.Items[i]
	status, msg := statusAndMessage(item.Status.Conditions)
	return append(nameColumns(&item, includeNamespace, includeKind), status, msg, item.Status.LatestImage)
}

func (s imagePolicyListAdapter) headers(includeNamespace bool) []string {
	headers := []string{"Name", "Ready", "Message", "Latest image"}
	if includeNamespace {
		return append(namespaceHeader, headers...)
	}
	return headers
}
