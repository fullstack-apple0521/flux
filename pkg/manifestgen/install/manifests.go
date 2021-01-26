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

package install

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"sigs.k8s.io/kustomize/api/filesys"
	"sigs.k8s.io/kustomize/api/krusty"

	"github.com/fluxcd/pkg/untar"
)

func fetch(ctx context.Context, url, version, dir string) error {
	ghURL := fmt.Sprintf("%s/latest/download/manifests.tar.gz", url)
	if strings.HasPrefix(version, "v") {
		ghURL = fmt.Sprintf("%s/download/%s/manifests.tar.gz", url, version)
	}

	req, err := http.NewRequest("GET", ghURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request for %s, error: %w", ghURL, err)
	}

	// download
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("failed to download manifests.tar.gz from %s, error: %w", ghURL, err)
	}
	defer resp.Body.Close()

	// check response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download manifests.tar.gz from %s, status: %s", ghURL, resp.Status)
	}

	// extract
	if _, err = untar.Untar(resp.Body, dir); err != nil {
		return fmt.Errorf("failed to untar manifests.tar.gz from %s, error: %w", ghURL, err)
	}

	return nil
}

func generate(base string, options Options) error {
	if containsItemString(options.Components, options.NotificationController) {
		options.EventsAddr = fmt.Sprintf("http://%s/", options.NotificationController)
	}

	if err := execTemplate(options, namespaceTmpl, path.Join(base, "namespace.yaml")); err != nil {
		return fmt.Errorf("generate namespace failed: %w", err)
	}

	if err := execTemplate(options, labelsTmpl, path.Join(base, "labels.yaml")); err != nil {
		return fmt.Errorf("generate labels failed: %w", err)
	}

	if err := execTemplate(options, nodeSelectorTmpl, path.Join(base, "node-selector.yaml")); err != nil {
		return fmt.Errorf("generate node selector failed: %w", err)
	}

	if err := execTemplate(options, kustomizationTmpl, path.Join(base, "kustomization.yaml")); err != nil {
		return fmt.Errorf("generate kustomization failed: %w", err)
	}

	if err := os.MkdirAll(path.Join(base, "roles"), os.ModePerm); err != nil {
		return fmt.Errorf("generate roles failed: %w", err)
	}

	if err := execTemplate(options, kustomizationRolesTmpl, path.Join(base, "roles/kustomization.yaml")); err != nil {
		return fmt.Errorf("generate roles kustomization failed: %w", err)
	}

	rbacFile := filepath.Join(base, "roles/rbac.yaml")
	if err := copyFile(filepath.Join(base, "rbac.yaml"), rbacFile); err != nil {
		return fmt.Errorf("generate rbac failed: %w", err)
	}

	// workaround for kustomize not being able to patch the SA in ClusterRoleBindings
	defaultNS := MakeDefaultOptions().Namespace
	if defaultNS != options.Namespace {
		rbac, err := ioutil.ReadFile(rbacFile)
		if err != nil {
			return fmt.Errorf("reading rbac file failed: %w", err)
		}
		rbac = bytes.ReplaceAll(rbac, []byte(defaultNS), []byte(options.Namespace))
		if err := ioutil.WriteFile(rbacFile, rbac, os.ModePerm); err != nil {
			return fmt.Errorf("replacing service account namespace in rbac failed: %w", err)
		}
	}
	return nil
}

func build(base, output string) error {
	kfile := filepath.Join(base, "kustomization.yaml")

	fs := filesys.MakeFsOnDisk()
	if !fs.Exists(kfile) {
		return fmt.Errorf("%s not found", kfile)
	}

	// TODO(hidde): work around for a bug in kustomize causing it to
	//  not properly handle absolute paths on Windows.
	//  Convert the path to a relative path to the working directory
	//  as a temporary fix:
	//  https://github.com/kubernetes-sigs/kustomize/issues/2789
	if filepath.IsAbs(base) {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		base, err = filepath.Rel(wd, base)
		if err != nil {
			return err
		}
	}

	opt := krusty.MakeDefaultOptions()
	opt.DoLegacyResourceSort = true
	k := krusty.MakeKustomizer(fs, opt)
	m, err := k.Run(base)
	if err != nil {
		return err
	}

	resources, err := m.AsYaml()
	if err != nil {
		return err
	}

	if err := fs.WriteFile(output, resources); err != nil {
		return err
	}

	return nil
}
