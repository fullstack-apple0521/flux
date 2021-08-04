package main

import (
	"testing"
)

func TestTraceNoArgs(t *testing.T) {
	cmd := cmdTestCase{
		args:        "trace",
		wantError:   true,
		goldenValue: "object name is required",
	}
	cmd.runTestCmd(t)
}

func TestTraceDeployment(t *testing.T) {
	cmd := cmdTestCase{
		args:       "trace podinfo -n podinfo --kind deployment --api-version=apps/v1",
		wantError:  false,
		goldenFile: "testdata/trace/deployment.txt",
		objectFile: "testdata/trace/deployment.yaml",
	}
	cmd.runTestCmd(t)
}

func TestTraceHelmRelease(t *testing.T) {
	cmd := cmdTestCase{
		args:       "trace podinfo -n podinfo --kind HelmRelease --api-version=helm.toolkit.fluxcd.io/v2beta1",
		wantError:  false,
		goldenFile: "testdata/trace/helmrelease.txt",
		objectFile: "testdata/trace/helmrelease.yaml",
	}
	cmd.runTestCmd(t)
}

func TestTraceHelmReleaseMissingGitRef(t *testing.T) {
	cmd := cmdTestCase{
		args:       "trace podinfo -n podinfo --kind HelmRelease --api-version=helm.toolkit.fluxcd.io/v2beta1",
		wantError:  false,
		goldenFile: "testdata/trace/helmrelease-missing-git-ref.txt",
		objectFile: "testdata/trace/helmrelease-missing-git-ref.yaml",
	}
	cmd.runTestCmd(t)
}
