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

package utils

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	helmv2 "github.com/fluxcd/helm-controller/api/v2beta1"
	imageautov1 "github.com/fluxcd/image-automation-controller/api/v1alpha1"
	imagereflectv1 "github.com/fluxcd/image-reflector-controller/api/v1alpha1"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1beta1"
	notificationv1 "github.com/fluxcd/notification-controller/api/v1beta1"
	"github.com/fluxcd/pkg/runtime/dependency"
	"github.com/fluxcd/pkg/version"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta1"
	"github.com/olekukonko/tablewriter"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/fluxcd/flux2/pkg/manifestgen/install"
)

type Utils struct {
}

type ExecMode string

const (
	ModeOS       ExecMode = "os.stderr|stdout"
	ModeStderrOS ExecMode = "os.stderr"
	ModeCapture  ExecMode = "capture.stderr|stdout"
)

func ExecKubectlCommand(ctx context.Context, mode ExecMode, kubeConfigPath string, kubeContext string, args ...string) (string, error) {
	var stdoutBuf, stderrBuf bytes.Buffer

	if kubeConfigPath != "" && len(filepath.SplitList(kubeConfigPath)) == 1 {
		args = append(args, "--kubeconfig="+kubeConfigPath)
	}

	if kubeContext != "" {
		args = append(args, "--context="+kubeContext)
	}

	c := exec.CommandContext(ctx, "kubectl", args...)

	if mode == ModeStderrOS {
		c.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	}
	if mode == ModeOS {
		c.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
		c.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	}

	if mode == ModeStderrOS || mode == ModeOS {
		if err := c.Run(); err != nil {
			return "", err
		} else {
			return "", nil
		}
	}

	if mode == ModeCapture {
		c.Stdout = &stdoutBuf
		c.Stderr = &stderrBuf
		if err := c.Run(); err != nil {
			return stderrBuf.String(), err
		} else {
			return stdoutBuf.String(), nil
		}
	}

	return "", nil
}

func ExecTemplate(obj interface{}, tmpl, filename string) error {
	t, err := template.New("tmpl").Parse(tmpl)
	if err != nil {
		return err
	}

	var data bytes.Buffer
	writer := bufio.NewWriter(&data)
	if err := t.Execute(writer, obj); err != nil {
		return err
	}

	if err := writer.Flush(); err != nil {
		return err
	}

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.WriteString(file, data.String())
	if err != nil {
		return err
	}

	return file.Sync()
}

func KubeConfig(kubeConfigPath string, kubeContext string) (*rest.Config, error) {
	configFiles := SplitKubeConfigPath(kubeConfigPath)
	configOverrides := clientcmd.ConfigOverrides{}

	if len(kubeContext) > 0 {
		configOverrides.CurrentContext = kubeContext
	}

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{Precedence: configFiles},
		&configOverrides,
	).ClientConfig()

	if err != nil {
		return nil, fmt.Errorf("kubernetes configuration load failed: %w", err)
	}

	return cfg, nil
}

func KubeClient(kubeConfigPath string, kubeContext string) (client.Client, error) {
	cfg, err := KubeConfig(kubeConfigPath, kubeContext)
	if err != nil {
		return nil, fmt.Errorf("kubernetes client initialization failed: %w", err)
	}

	scheme := apiruntime.NewScheme()
	_ = apiextensionsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = sourcev1.AddToScheme(scheme)
	_ = kustomizev1.AddToScheme(scheme)
	_ = helmv2.AddToScheme(scheme)
	_ = notificationv1.AddToScheme(scheme)
	_ = imagereflectv1.AddToScheme(scheme)
	_ = imageautov1.AddToScheme(scheme)

	kubeClient, err := client.New(cfg, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("kubernetes client initialization failed: %w", err)
	}

	return kubeClient, nil
}

// SplitKubeConfigPath splits the given KUBECONFIG path based on the runtime OS
// target.
//
// Ref: https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/#the-kubeconfig-environment-variable
func SplitKubeConfigPath(path string) []string {
	var sep string
	switch runtime.GOOS {
	case "windows":
		sep = ";"
	default:
		sep = ":"
	}
	return strings.Split(path, sep)
}

func ContainsItemString(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func ContainsEqualFoldItemString(s []string, e string) (string, bool) {
	for _, a := range s {
		if strings.EqualFold(a, e) {
			return a, true
		}
	}
	return "", false
}

// ParseObjectKindName extracts the kind and name of a resource
// based on the '<kind>/<name>' format
func ParseObjectKindName(input string) (kind, name string) {
	name = input
	parts := strings.Split(input, "/")
	if len(parts) == 2 {
		kind, name = parts[0], parts[1]
	}
	return kind, name
}

// ParseObjectKindNameNamespace extracts the kind, name and namespace of a resource
// based on the '<kind>/<name>.<namespace>' format
func ParseObjectKindNameNamespace(input string) (kind, name, namespace string) {
	name = input
	parts := strings.Split(input, "/")
	if len(parts) == 2 {
		kind, name = parts[0], parts[1]
	}

	if nn := strings.Split(name, "."); len(nn) > 1 {
		name = strings.Join(nn[:len(nn)-1], ".")
		namespace = nn[len(nn)-1]
	}

	return kind, name, namespace
}

func MakeDependsOn(deps []string) []dependency.CrossNamespaceDependencyReference {
	refs := []dependency.CrossNamespaceDependencyReference{}
	for _, dep := range deps {
		parts := strings.Split(dep, "/")
		depNamespace := ""
		depName := ""
		if len(parts) > 1 {
			depNamespace = parts[0]
			depName = parts[1]
		} else {
			depName = parts[0]
		}
		refs = append(refs, dependency.CrossNamespaceDependencyReference{
			Namespace: depNamespace,
			Name:      depName,
		})
	}
	return refs
}

func PrintTable(writer io.Writer, header []string, rows [][]string) {
	table := tablewriter.NewWriter(writer)
	table.SetHeader(header)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetTablePadding("\t")
	table.SetNoWhiteSpace(true)
	table.AppendBulk(rows)
	table.Render()
}

func ValidateComponents(components []string) error {
	defaults := install.MakeDefaultOptions()
	bootstrapAllComponents := append(defaults.Components, defaults.ComponentsExtra...)
	for _, component := range components {
		if !ContainsItemString(bootstrapAllComponents, component) {
			return fmt.Errorf("component %s is not available", component)
		}
	}

	return nil
}

// CompatibleVersion returns if the provided binary version is compatible
// with the given target version. At present, this is true if the target
// version is equal to the MINOR range of the binary, or if the binary
// version is a prerelease.
func CompatibleVersion(binary, target string) bool {
	binSv, err := version.ParseVersion(binary)
	if err != nil {
		return false
	}
	// Assume prerelease builds are compatible.
	if binSv.Prerelease() != "" {
		return true
	}
	targetSv, err := version.ParseVersion(target)
	if err != nil {
		return false
	}
	return binSv.Major() == targetSv.Major() && binSv.Minor() == targetSv.Minor()
}
