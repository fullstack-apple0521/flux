/*
Copyright 2021 The Flux authors

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
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/fluxcd/flux2/internal/flags"
	"github.com/fluxcd/flux2/internal/utils"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Display formatted logs for Flux components",
	Long:  "The logs command displays formatted logs from various Flux components.",
	Example: `  # Print the reconciliation logs of all Flux custom resources in your cluster
	 flux logs --all-namespaces

	# Stream logs for a particular log level
	flux logs --follow --level=error --all-namespaces

	# Filter logs by kind, name and namespace
	flux logs --kind=Kustomization --name=podinfo --namespace=default

	# Print logs when Flux is installed in a different namespace than flux-system
	flux logs --flux-namespace=my-namespace
    `,
	RunE: logsCmdRun,
}

type logsFlags struct {
	logLevel      flags.LogLevel
	follow        bool
	tail          int64
	kind          string
	name          string
	fluxNamespace string
	allNamespaces bool
}

var logsArgs = &logsFlags{
	tail: -1,
}

func init() {
	logsCmd.Flags().Var(&logsArgs.logLevel, "level", logsArgs.logLevel.Description())
	logsCmd.Flags().StringVarP(&logsArgs.kind, "kind", "", logsArgs.kind, "displays errors of a particular toolkit kind e.g GitRepository")
	logsCmd.Flags().StringVarP(&logsArgs.name, "name", "", logsArgs.name, "specifies the name of the object logs to be displayed")
	logsCmd.Flags().BoolVarP(&logsArgs.follow, "follow", "f", logsArgs.follow, "specifies if the logs should be streamed")
	logsCmd.Flags().Int64VarP(&logsArgs.tail, "tail", "", logsArgs.tail, "lines of recent log file to display")
	logsCmd.Flags().StringVarP(&logsArgs.fluxNamespace, "flux-namespace", "", rootArgs.defaults.Namespace, "the namespace where the Flux components are running")
	logsCmd.Flags().BoolVarP(&logsArgs.allNamespaces, "all-namespaces", "A", false, "displays logs for objects across all namespaces")
	rootCmd.AddCommand(logsCmd)
}

func logsCmdRun(cmd *cobra.Command, args []string) error {
	fluxSelector := fmt.Sprintf("app.kubernetes.io/instance=%s", logsArgs.fluxNamespace)

	ctx, cancel := context.WithTimeout(context.Background(), rootArgs.timeout)
	defer cancel()

	var pods []corev1.Pod
	cfg, err := utils.KubeConfig(rootArgs.kubeconfig, rootArgs.kubecontext)
	if err != nil {
		return err
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}

	if len(args) > 0 {
		return fmt.Errorf("no argument required")
	}

	pods, err = getPods(ctx, clientset, fluxSelector)
	if err != nil {
		return err
	}

	logOpts := &corev1.PodLogOptions{
		Follow: logsArgs.follow,
	}

	if logsArgs.tail > -1 {
		logOpts.TailLines = &logsArgs.tail
	}

	var requests []rest.ResponseWrapper
	for _, pod := range pods {
		req := clientset.CoreV1().Pods(logsArgs.fluxNamespace).GetLogs(pod.Name, logOpts)
		requests = append(requests, req)
	}

	if logsArgs.follow && len(requests) > 1 {
		return parallelPodLogs(ctx, requests)
	}

	return podLogs(ctx, requests)
}

func getPods(ctx context.Context, c *kubernetes.Clientset, label string) ([]corev1.Pod, error) {
	var ret []corev1.Pod

	opts := metav1.ListOptions{
		LabelSelector: label,
	}
	deployList, err := c.AppsV1().Deployments(logsArgs.fluxNamespace).List(ctx, opts)
	if err != nil {
		return ret, err
	}

	for _, deploy := range deployList.Items {
		label := deploy.Spec.Template.Labels
		opts := metav1.ListOptions{
			LabelSelector: createLabelStringFromMap(label),
		}
		podList, err := c.CoreV1().Pods(logsArgs.fluxNamespace).List(ctx, opts)
		if err != nil {
			return ret, err
		}
		ret = append(ret, podList.Items...)
	}

	return ret, nil
}

func parallelPodLogs(ctx context.Context, requests []rest.ResponseWrapper) error {
	reader, writer := io.Pipe()
	wg := &sync.WaitGroup{}
	wg.Add(len(requests))

	var mutex = &sync.Mutex{}

	for _, request := range requests {
		go func(req rest.ResponseWrapper) {
			defer wg.Done()
			if err := logRequest(mutex, ctx, req, os.Stdout); err != nil {
				writer.CloseWithError(err)
				return
			}
		}(request)
	}

	go func() {
		wg.Wait()
		writer.Close()
	}()

	_, err := io.Copy(os.Stdout, reader)
	return err
}

func podLogs(ctx context.Context, requests []rest.ResponseWrapper) error {
	mutex := &sync.Mutex{}
	for _, req := range requests {
		if err := logRequest(mutex, ctx, req, os.Stdout); err != nil {
			return err
		}
	}

	return nil
}

func createLabelStringFromMap(m map[string]string) string {
	var strArr []string
	for key, val := range m {
		pair := fmt.Sprintf("%v=%v", key, val)
		strArr = append(strArr, pair)
	}

	return strings.Join(strArr, ",")
}

func logRequest(mu *sync.Mutex, ctx context.Context, request rest.ResponseWrapper, w io.Writer) error {
	stream, err := request.Stream(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)

	const logTmpl = "{{.Timestamp}} {{.Level}} {{.Kind}}{{if .Name}}/{{.Name}}.{{.Namespace}}{{end}} - {{.Message}} {{.Error}}\n"
	t, err := template.New("log").Parse(logTmpl)
	if err != nil {
		return fmt.Errorf("unable to create template, err: %s", err)
	}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var l ControllerLogEntry
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			logger.Failuref("parse error: %s", err)
			break
		}

		mu.Lock()
		filterPrintLog(t, &l)
		mu.Unlock()
	}

	return nil
}

func filterPrintLog(t *template.Template, l *ControllerLogEntry) {
	if logsArgs.logLevel != "" && logsArgs.logLevel != l.Level ||
		logsArgs.kind != "" && strings.ToLower(logsArgs.kind) != strings.ToLower(l.Kind) ||
		logsArgs.name != "" && strings.ToLower(logsArgs.name) != strings.ToLower(l.Name) ||
		!logsArgs.allNamespaces && strings.ToLower(rootArgs.namespace) != strings.ToLower(l.Namespace) {
		return
	}

	err := t.Execute(os.Stdout, l)
	if err != nil {
		logger.Failuref("log template error: %s", err)
	}
}

type ControllerLogEntry struct {
	Timestamp string         `json:"ts"`
	Level     flags.LogLevel `json:"level"`
	Message   string         `json:"msg"`
	Error     string         `json:"error,omitempty"`
	Logger    string         `json:"logger"`
	Kind      string         `json:"reconciler kind,omitempty"`
	Name      string         `json:"name,omitempty"`
	Namespace string         `json:"namespace,omitempty"`
}
