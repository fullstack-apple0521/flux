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
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1beta2"
)

var getKsCmd = &cobra.Command{
	Use:     "kustomizations",
	Aliases: []string{"ks", "kustomization"},
	Short:   "Get Kustomization statuses",
	Long:    "The get kustomizations command prints the statuses of the resources.",
	Example: `  # List all kustomizations and their status
  flux get kustomizations`,
	ValidArgsFunction: resourceNamesCompletionFunc(kustomizev1.GroupVersion.WithKind(kustomizev1.KustomizationKind)),
	RunE: func(cmd *cobra.Command, args []string) error {
		get := getCommand{
			apiType: kustomizationType,
			list:    &kustomizationListAdapter{&kustomizev1.KustomizationList{}},
			funcMap: make(typeMap),
		}

		err := get.funcMap.registerCommand(get.apiType.kind, func(obj runtime.Object) (summarisable, error) {
			o, ok := obj.(*kustomizev1.Kustomization)
			if !ok {
				return nil, fmt.Errorf("Impossible to cast type %#v kustomization", obj)
			}

			sink := kustomizationListAdapter{
				&kustomizev1.KustomizationList{
					Items: []kustomizev1.Kustomization{
						*o,
					},
				},
			}
			return sink, nil
		})

		if err != nil {
			return err
		}

		if err := get.run(cmd, args); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	getCmd.AddCommand(getKsCmd)
}

func (a kustomizationListAdapter) summariseItem(i int, includeNamespace bool, includeKind bool) []string {
	item := a.Items[i]
	revision := item.Status.LastAppliedRevision
	status, msg := statusAndMessage(item.Status.Conditions)
	if status == string(metav1.ConditionTrue) {
		revision = shortenCommitSha(revision)
		msg = shortenCommitSha(msg)
	}
	return append(nameColumns(&item, includeNamespace, includeKind),
		revision, strings.Title(strconv.FormatBool(item.Spec.Suspend)), status, msg)
}

func (a kustomizationListAdapter) headers(includeNamespace bool) []string {
	headers := []string{"Name", "Revision", "Suspended", "Ready", "Message"}
	if includeNamespace {
		headers = append([]string{"Namespace"}, headers...)
	}
	return headers
}

func (a kustomizationListAdapter) statusSelectorMatches(i int, conditionType, conditionStatus string) bool {
	item := a.Items[i]
	return statusMatches(conditionType, conditionStatus, item.Status.Conditions)
}

func shortenCommitSha(msg string) string {
	r := regexp.MustCompile("/([a-f0-9]{40})$")
	sha := r.FindString(msg)
	if sha != "" {
		msg = strings.Replace(msg, sha, string([]rune(sha)[:8]), -1)
	}

	return msg
}
