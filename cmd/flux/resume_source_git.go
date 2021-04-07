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

	sourcev1 "github.com/fluxcd/source-controller/api/v1beta1"
)

var resumeSourceGitCmd = &cobra.Command{
	Use:   "git [name]",
	Short: "Resume a suspended GitRepository",
	Long:  `The resume command marks a previously suspended GitRepository resource for reconciliation and waits for it to finish.`,
	Example: `  # Resume reconciliation for an existing GitRepository
  flux resume source git podinfo`,
	RunE: resumeCommand{
		apiType: gitRepositoryType,
		object:  gitRepositoryAdapter{&sourcev1.GitRepository{}},
	}.run,
}

func init() {
	resumeSourceCmd.AddCommand(resumeSourceGitCmd)
}

func (obj gitRepositoryAdapter) getObservedGeneration() int64 {
	return obj.GitRepository.Status.ObservedGeneration
}

func (obj gitRepositoryAdapter) setUnsuspended() {
	obj.GitRepository.Spec.Suspend = false
}
