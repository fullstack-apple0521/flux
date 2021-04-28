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

	imagev1 "github.com/fluxcd/image-reflector-controller/api/v1alpha2"
)

var resumeImageRepositoryCmd = &cobra.Command{
	Use:   "repository [name]",
	Short: "Resume a suspended ImageRepository",
	Long:  `The resume command marks a previously suspended ImageRepository resource for reconciliation and waits for it to finish.`,
	Example: `  # Resume reconciliation for an existing ImageRepository
  flux resume image repository alpine`,
	RunE: resumeCommand{
		apiType: imageRepositoryType,
		object:  imageRepositoryAdapter{&imagev1.ImageRepository{}},
		list:    imageRepositoryListAdapter{&imagev1.ImageRepositoryList{}},
	}.run,
}

func init() {
	resumeImageCmd.AddCommand(resumeImageRepositoryCmd)
}

func (obj imageRepositoryAdapter) getObservedGeneration() int64 {
	return obj.ImageRepository.Status.ObservedGeneration
}

func (obj imageRepositoryAdapter) setUnsuspended() {
	obj.ImageRepository.Spec.Suspend = false
}

func (a imageRepositoryListAdapter) resumeItem(i int) resumable {
	return &imageRepositoryAdapter{&a.ImageRepositoryList.Items[i]}
}
