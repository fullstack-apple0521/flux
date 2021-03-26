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
	"encoding/json"
	"os"
	"os/exec"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	v1 "k8s.io/api/apps/v1"
	apimachineryversion "k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/fluxcd/pkg/version"

	"github.com/fluxcd/flux2/internal/utils"
	"github.com/fluxcd/flux2/pkg/manifestgen/install"
	"github.com/fluxcd/flux2/pkg/status"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check requirements and installation",
	Long: `The check command will perform a series of checks to validate that
the local environment is configured correctly and if the installed components are healthy.`,
	Example: `  # Run pre-installation checks
  flux check --pre

  # Run installation checks
  flux check`,
	RunE: runCheckCmd,
}

type checkFlags struct {
	pre             bool
	components      []string
	extraComponents []string
}

type kubectlVersion struct {
	ClientVersion *apimachineryversion.Info `json:"clientVersion"`
}

var checkArgs checkFlags

func init() {
	checkCmd.Flags().BoolVarP(&checkArgs.pre, "pre", "", false,
		"only run pre-installation checks")
	checkCmd.Flags().StringSliceVar(&checkArgs.components, "components", rootArgs.defaults.Components,
		"list of components, accepts comma-separated values")
	checkCmd.Flags().StringSliceVar(&checkArgs.extraComponents, "components-extra", nil,
		"list of components in addition to those supplied or defaulted, accepts comma-separated values")
	rootCmd.AddCommand(checkCmd)
}

func runCheckCmd(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), rootArgs.timeout)
	defer cancel()

	logger.Actionf("checking prerequisites")
	checkFailed := false

	fluxCheck()

	if !kubectlCheck(ctx, ">=1.18.0-0") {
		checkFailed = true
	}

	if !kubernetesCheck(">=1.16.0-0") {
		checkFailed = true
	}

	if checkArgs.pre {
		if checkFailed {
			os.Exit(1)
		}
		logger.Successf("prerequisites checks passed")
		return nil
	}

	logger.Actionf("checking controllers")
	if !componentsCheck() {
		checkFailed = true
	}
	if checkFailed {
		os.Exit(1)
	}
	logger.Successf("all checks passed")
	return nil
}

func fluxCheck() {
	curSv, err := version.ParseVersion(VERSION)
	if err != nil {
		return
	}
	// Exclude development builds.
	if curSv.Prerelease() != "" {
		return
	}
	latest, err := install.GetLatestVersion()
	if err != nil {
		return
	}
	latestSv, err := version.ParseVersion(latest)
	if err != nil {
		return
	}
	if latestSv.GreaterThan(curSv) {
		logger.Failuref("flux %s <%s (new version is available, please upgrade)", curSv, latestSv)
	}
}

func kubectlCheck(ctx context.Context, constraint string) bool {
	_, err := exec.LookPath("kubectl")
	if err != nil {
		logger.Failuref("kubectl not found")
		return false
	}

	kubectlArgs := []string{"version", "--client", "--output", "json"}
	output, err := utils.ExecKubectlCommand(ctx, utils.ModeCapture, rootArgs.kubeconfig, rootArgs.kubecontext, kubectlArgs...)
	if err != nil {
		logger.Failuref("kubectl version can't be determined")
		return false
	}

	kv := &kubectlVersion{}
	if err = json.Unmarshal([]byte(output), kv); err != nil {
		logger.Failuref("kubectl version output can't be unmarshalled")
		return false
	}

	v, err := version.ParseVersion(kv.ClientVersion.GitVersion)
	if err != nil {
		logger.Failuref("kubectl version can't be parsed")
		return false
	}

	c, _ := semver.NewConstraint(constraint)
	if !c.Check(v) {
		logger.Failuref("kubectl version %s < %s", v.Original(), constraint)
		return false
	}

	logger.Successf("kubectl %s %s", v.String(), constraint)
	return true
}

func kubernetesCheck(constraint string) bool {
	cfg, err := utils.KubeConfig(rootArgs.kubeconfig, rootArgs.kubecontext)
	if err != nil {
		logger.Failuref("Kubernetes client initialization failed: %s", err.Error())
		return false
	}

	clientSet, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logger.Failuref("Kubernetes client initialization failed: %s", err.Error())
		return false
	}

	kv, err := clientSet.Discovery().ServerVersion()
	if err != nil {
		logger.Failuref("Kubernetes API call failed: %s", err.Error())
		return false
	}

	v, err := version.ParseVersion(kv.String())
	if err != nil {
		logger.Failuref("Kubernetes version can't be determined")
		return false
	}

	c, _ := semver.NewConstraint(constraint)
	if !c.Check(v) {
		logger.Failuref("Kubernetes version %s < %s", v.Original(), constraint)
		return false
	}

	logger.Successf("Kubernetes %s %s", v.String(), constraint)
	return true
}

func componentsCheck() bool {
	ctx, cancel := context.WithTimeout(context.Background(), rootArgs.timeout)
	defer cancel()

	kubeConfig, err := utils.KubeConfig(rootArgs.kubeconfig, rootArgs.kubecontext)
	if err != nil {
		return false
	}

	statusChecker, err := status.NewStatusChecker(kubeConfig, time.Second, rootArgs.timeout, logger)
	if err != nil {
		return false
	}

	kubeClient, err := utils.KubeClient(rootArgs.kubeconfig, rootArgs.kubecontext)
	if err != nil {
		return false
	}

	ok := true
	selector := client.MatchingLabels{"app.kubernetes.io/instance": rootArgs.namespace}
	var list v1.DeploymentList
	if err := kubeClient.List(ctx, &list, client.InNamespace(rootArgs.namespace), selector); err == nil {
		for _, d := range list.Items {
			if ref, err := buildComponentObjectRefs(d.Name); err == nil {
				if err := statusChecker.Assess(ref...); err != nil {
					ok = false
				}
			}
			for _, c := range d.Spec.Template.Spec.Containers {
				logger.Actionf(c.Image)
			}
		}
	}
	return ok
}
