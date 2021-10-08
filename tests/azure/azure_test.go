package test

import (
	"bytes"
	"context"
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	eventhub "github.com/Azure/azure-event-hubs-go/v3"
	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/hashicorp/terraform-exec/tfinstall"
	git2go "github.com/libgit2/git2go/v31"
	"github.com/microsoft/azure-devops-go-api/azuredevops"
	"github.com/microsoft/azure-devops-go-api/azuredevops/git"
	"github.com/stretchr/testify/require"
	giturls "github.com/whilp/git-urls"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	automationv1beta1 "github.com/fluxcd/image-automation-controller/api/v1beta1"
	reflectorv1beta1 "github.com/fluxcd/image-reflector-controller/api/v1beta1"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1beta1"
	notiv1beta1 "github.com/fluxcd/notification-controller/api/v1beta1"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/events"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta1"
)

type config struct {
	kubeconfigPath string
	kubeClient     client.Client

	azdoPat               string
	idRsa                 string
	idRsaPub              string
	knownHosts            string
	fleetInfraRepository  repoConfig
	applicationRepository repoConfig

	fluxAzureSp spConfig
	sopsId      string
	acr         acrConfig
	eventHubSas string
}

type spConfig struct {
	tenantId     string
	clientId     string
	clientSecret string
}

type repoConfig struct {
	http string
	ssh  string
}

type acrConfig struct {
	url      string
	username string
	password string
}

var cfg config

func TestMain(m *testing.M) {
	ctx := context.TODO()

	log.Println("Setting up Azure test infrastructure")
	execPath, err := tfinstall.Find(ctx, &whichTerraform{})
	if err != nil {
		log.Fatalf("terraform exec path not found: %v", err)
	}
	tf, err := tfexec.NewTerraform("./terraform/aks", execPath)
	if err != nil {
		log.Fatalf("could not create terraform instance: %v", err)
	}
	log.Println("Init Terraform")
	err = tf.Init(ctx, tfexec.Upgrade(true))
	if err != nil {
		log.Fatalf("error running init: %v", err)
	}
	log.Println("Applying Terraform")
	err = tf.Apply(ctx)
	if err != nil {
		log.Fatalf("error running apply: %v", err)
	}
	state, err := tf.Show(ctx)
	if err != nil {
		log.Fatalf("error running show: %v", err)
	}
	outputs := state.Values.Outputs
	kubeconfig := outputs["aks_kube_config"].Value.(string)
	aksHost := outputs["aks_host"].Value.(string)
	aksCert := outputs["aks_client_certificate"].Value.(string)
	aksKey := outputs["aks_client_key"].Value.(string)
	aksCa := outputs["aks_cluster_ca_certificate"].Value.(string)
	azdoPat := outputs["shared_pat"].Value.(string)
	idRsa := outputs["shared_id_rsa"].Value.(string)
	idRsaPub := outputs["shared_id_rsa_pub"].Value.(string)
	fleetInfraRepository := outputs["fleet_infra_repository"].Value.(map[string]interface{})
	applicationRepository := outputs["application_repository"].Value.(map[string]interface{})
	fluxAzureSp := outputs["flux_azure_sp"].Value.(map[string]interface{})
	sharedSopsId := outputs["sops_id"].Value.(string)
	acr := outputs["acr"].Value.(map[string]interface{})
	eventHubSas := outputs["event_hub_sas"].Value.(string)

	log.Println("Creating Kubernetes client")
	kubeconfigPath, kubeClient, err := getKubernetesCredentials(kubeconfig, aksHost, aksCert, aksKey, aksCa)
	if err != nil {
		log.Fatalf("error create Kubernetes client: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(kubeconfigPath))

	cfg = config{
		kubeconfigPath: kubeconfigPath,
		kubeClient:     kubeClient,
		azdoPat:        azdoPat,
		idRsa:          idRsa,
		idRsaPub:       idRsaPub,
		knownHosts:     "ssh.dev.azure.com ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC7Hr1oTWqNqOlzGJOfGJ4NakVyIzf1rXYd4d7wo6jBlkLvCA4odBlL0mDUyZ0/QUfTTqeu+tm22gOsv+VrVTMk6vwRU75gY/y9ut5Mb3bR5BV58dKXyq9A9UeB5Cakehn5Zgm6x1mKoVyf+FFn26iYqXJRgzIZZcZ5V6hrE0Qg39kZm4az48o0AUbf6Sp4SLdvnuMa2sVNwHBboS7EJkm57XQPVU3/QpyNLHbWDdzwtrlS+ez30S3AdYhLKEOxAG8weOnyrtLJAUen9mTkol8oII1edf7mWWbWVf0nBmly21+nZcmCTISQBtdcyPaEno7fFQMDD26/s0lfKob4Kw8H",
		fleetInfraRepository: repoConfig{
			http: fleetInfraRepository["http"].(string),
			ssh:  fleetInfraRepository["ssh"].(string),
		},
		applicationRepository: repoConfig{
			http: applicationRepository["http"].(string),
			ssh:  applicationRepository["ssh"].(string),
		},
		fluxAzureSp: spConfig{
			tenantId:     fluxAzureSp["tenant_id"].(string),
			clientId:     fluxAzureSp["client_id"].(string),
			clientSecret: fluxAzureSp["client_secret"].(string),
		},
		sopsId: sharedSopsId,
		acr: acrConfig{
			url:      acr["url"].(string),
			username: acr["username"].(string),
			password: acr["password"].(string),
		},
		eventHubSas: eventHubSas,
	}

	err = installFlux(ctx, kubeClient, kubeconfigPath, cfg.fleetInfraRepository.http, azdoPat, cfg.fluxAzureSp)
	if err != nil {
		log.Fatalf("error installing Flux: %v", err)
	}

	log.Println("Running Azure e2e tests")
	exitVal := m.Run()

	log.Println("Tearing down Azure test infrastructure")
	err = tf.Destroy(ctx)
	if err != nil {
		log.Fatalf("error running Show: %v", err)
	}

	os.Exit(exitVal)
}

func TestFluxInstallation(t *testing.T) {
	ctx := context.TODO()
	require.Eventually(t, func() bool {
		err := verifyGitAndKustomization(ctx, cfg.kubeClient, "flux-system", "flux-system")
		if err != nil {
			return false
		}
		return true
	}, 60*time.Second, 5*time.Second)
}

func TestAzureDevOpsCloning(t *testing.T) {
	ctx := context.TODO()
	branchName := "feature/branch"
	tagName := "v1"

	tests := []struct {
		name      string
		refType   string
		cloneType string
	}{
		{
			name:      "https-feature-branch",
			refType:   "branch",
			cloneType: "http",
		},
		{
			name:      "https-v1",
			refType:   "tag",
			cloneType: "http",
		},
		{
			name:      "ssh-feature-branch",
			refType:   "branch",
			cloneType: "ssh",
		},
		{
			name:      "ssh-v1",
			refType:   "tag",
			cloneType: "ssh",
		},
	}

	t.Log("Creating application sources")
	repo, repoDir, err := getRepository(cfg.applicationRepository.http, branchName, true, cfg.azdoPat)
	require.NoError(t, err)
	for _, tt := range tests {
		manifest := fmt.Sprintf(`
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: foobar
        namespace: %s
    `, tt.name)
		err = runCommand(ctx, repoDir, fmt.Sprintf("mkdir -p ./cloning-test/%s", tt.name))
		require.NoError(t, err)
		err = runCommand(ctx, repoDir, fmt.Sprintf("echo '%s' > ./cloning-test/%s/configmap.yaml", manifest, tt.name))
		require.NoError(t, err)
	}
	err = commitAndPushAll(repo, branchName, cfg.azdoPat)
	require.NoError(t, err)
	err = createTagAndPush(repo, branchName, tagName, cfg.azdoPat)
	require.NoError(t, err)

	t.Log("Verifying application-gitops namespaces")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := &sourcev1.GitRepositoryRef{
				Branch: branchName,
			}
			if tt.refType == "tag" {
				ref = &sourcev1.GitRepositoryRef{
					Tag: tagName,
				}
			}
			url := cfg.applicationRepository.http
			secretData := map[string]string{
				"username": "git",
				"password": cfg.azdoPat,
			}
			if tt.cloneType == "ssh" {
				url = cfg.applicationRepository.ssh
				secretData = map[string]string{
					"identity":     cfg.idRsa,
					"identity.pub": cfg.idRsaPub,
					"known_hosts":  cfg.knownHosts,
				}
			}

			namespace := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.name,
				},
			}
			_, err := controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, &namespace, func() error {
				return nil
			})
			gitSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "git-credentials",
					Namespace: namespace.Name,
				},
			}
			_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, gitSecret, func() error {
				gitSecret.StringData = secretData
				return nil
			})
			source := &sourcev1.GitRepository{ObjectMeta: metav1.ObjectMeta{Name: tt.name, Namespace: namespace.Name}}
			_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, source, func() error {
				source.Spec = sourcev1.GitRepositorySpec{
					GitImplementation: sourcev1.LibGit2Implementation,
					Reference:         ref,
					SecretRef: &meta.LocalObjectReference{
						Name: gitSecret.Name,
					},
					URL: url,
				}
				return nil
			})
			require.NoError(t, err)
			kustomization := &kustomizev1.Kustomization{ObjectMeta: metav1.ObjectMeta{Name: tt.name, Namespace: namespace.Name}}
			_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, kustomization, func() error {
				kustomization.Spec = kustomizev1.KustomizationSpec{
					Path: fmt.Sprintf("./cloning-test/%s", tt.name),
					SourceRef: kustomizev1.CrossNamespaceSourceReference{
						Kind:      sourcev1.GitRepositoryKind,
						Name:      tt.name,
						Namespace: namespace.Name,
					},
					Interval: metav1.Duration{Duration: 1 * time.Minute},
					Prune:    true,
				}
				return nil
			})
			require.NoError(t, err)

			// Wait for configmap to be deployed
			require.Eventually(t, func() bool {
				err := verifyGitAndKustomization(ctx, cfg.kubeClient, namespace.Name, tt.name)
				if err != nil {
					return false
				}
				nn := types.NamespacedName{Name: "foobar", Namespace: namespace.Name}
				cm := &corev1.ConfigMap{}
				err = cfg.kubeClient.Get(ctx, nn, cm)
				if err != nil {
					return false
				}
				return true
			}, 120*time.Second, 5*time.Second)
		})
	}
}

func TestImageRepositoryACR(t *testing.T) {
	ctx := context.TODO()
	name := "image-repository-acr"
	repoUrl := cfg.applicationRepository.http
	oldVersion := "1.0.0"
	newVersion := "1.0.1"
	manifest := fmt.Sprintf(`
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: podinfo
      namespace: %s
    spec:
      selector:
        matchLabels:
          app: podinfo
      template:
        metadata:
          labels:
            app: podinfo
        spec:
          containers:
          - name: podinfod
            image: %s/container/podinfo:%s # {"$imagepolicy": "%s:podinfo"}
            readinessProbe:
              exec:
                command:
                - podcli
                - check
                - http
                - localhost:9898/readyz
              initialDelaySeconds: 5
              timeoutSeconds: 5`, name, cfg.acr.url, oldVersion, name)

	repo, repoDir, err := getRepository(repoUrl, name, true, cfg.azdoPat)
	require.NoError(t, err)
	err = addFile(repoDir, "podinfo.yaml", manifest)
	require.NoError(t, err)
	err = commitAndPushAll(repo, name, cfg.azdoPat)
	require.NoError(t, err)

	err = setupNamespace(ctx, cfg.kubeClient, repoUrl, cfg.azdoPat, name)
	require.NoError(t, err)
	acrSecret := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "acr-docker", Namespace: name}}
	_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, &acrSecret, func() error {
		acrSecret.Type = corev1.SecretTypeDockerConfigJson
		acrSecret.StringData = map[string]string{
			".dockerconfigjson": fmt.Sprintf(`
        {
          "auths": {
            "%s": {
              "auth": "%s"
            }
          }
        }
        `, cfg.acr.url, b64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", cfg.acr.username, cfg.acr.password)))),
		}
		return nil
	})
	require.NoError(t, err)
	imageRepository := reflectorv1beta1.ImageRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "podinfo",
			Namespace: name,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, &imageRepository, func() error {
		imageRepository.Spec = reflectorv1beta1.ImageRepositorySpec{
			Image: fmt.Sprintf("%s/container/podinfo", cfg.acr.url),
			Interval: metav1.Duration{
				Duration: 1 * time.Minute,
			},
			SecretRef: &meta.LocalObjectReference{
				Name: acrSecret.Name,
			},
		}
		return nil
	})
	require.NoError(t, err)
	imagePolicy := reflectorv1beta1.ImagePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "podinfo",
			Namespace: name,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, &imagePolicy, func() error {
		imagePolicy.Spec = reflectorv1beta1.ImagePolicySpec{
			ImageRepositoryRef: meta.NamespacedObjectReference{
				Name: imageRepository.Name,
			},
			Policy: reflectorv1beta1.ImagePolicyChoice{
				SemVer: &reflectorv1beta1.SemVerPolicy{
					Range: "1.0.x",
				},
			},
		}
		return nil
	})
	require.NoError(t, err)
	imageAutomation := automationv1beta1.ImageUpdateAutomation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "podinfo",
			Namespace: name,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, &imageAutomation, func() error {
		imageAutomation.Spec = automationv1beta1.ImageUpdateAutomationSpec{
			Interval: metav1.Duration{
				Duration: 1 * time.Minute,
			},
			SourceRef: automationv1beta1.SourceReference{
				Kind: "GitRepository",
				Name: name,
			},
			GitSpec: &automationv1beta1.GitSpec{
				Checkout: &automationv1beta1.GitCheckoutSpec{
					Reference: sourcev1.GitRepositoryRef{
						Branch: name,
					},
				},
				Commit: automationv1beta1.CommitSpec{
					Author: automationv1beta1.CommitUser{
						Email: "imageautomation@example.com",
						Name:  "imageautomation",
					},
				},
			},
		}
		return nil
	})
	require.NoError(t, err)

	// Wait for image repository to be ready
	require.Eventually(t, func() bool {
		_, repoDir, err := getRepository(repoUrl, name, false, cfg.azdoPat)
		if err != nil {
			return false
		}
		b, err := os.ReadFile(filepath.Join(repoDir, "podinfo.yaml"))
		if err != nil {
			return false
		}
		if bytes.Contains(b, []byte(newVersion)) == false {
			return false
		}
		return true
	}, 120*time.Second, 5*time.Second)
}

func TestKeyVaultSops(t *testing.T) {
	ctx := context.TODO()
	name := "key-vault-sops"
	repoUrl := cfg.applicationRepository.http
	secretYaml := `apiVersion: v1
kind: Secret
metadata:
  name: "test"
  namespace: "key-vault-sops"
stringData:
  foo: "bar"`

	repo, tmpDir, err := getRepository(repoUrl, name, true, cfg.azdoPat)
	err = runCommand(ctx, tmpDir, "mkdir -p ./key-vault-sops")
	require.NoError(t, err)
	err = runCommand(ctx, tmpDir, fmt.Sprintf("echo \"%s\" > ./key-vault-sops/secret.enc.yaml", secretYaml))
	require.NoError(t, err)
	err = runCommand(ctx, tmpDir, fmt.Sprintf("sops --encrypt --encrypted-regex '^(data|stringData)$' --azure-kv %s --in-place ./key-vault-sops/secret.enc.yaml", cfg.sopsId))
	require.NoError(t, err)
	err = commitAndPushAll(repo, name, cfg.azdoPat)
	require.NoError(t, err)

	err = setupNamespace(ctx, cfg.kubeClient, repoUrl, cfg.azdoPat, name)
	require.NoError(t, err)

	source := &sourcev1.GitRepository{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: name}}
	require.Eventually(t, func() bool {
		_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, source, func() error {
			source.Spec = sourcev1.GitRepositorySpec{
				GitImplementation: sourcev1.LibGit2Implementation,
				Reference: &sourcev1.GitRepositoryRef{
					Branch: name,
				},
				SecretRef: &meta.LocalObjectReference{
					Name: "https-credentials",
				},
				URL: repoUrl,
			}
			return nil
		})
		if err != nil {
			return false
		}
		return true
	}, 10*time.Second, 1*time.Second)
	kustomization := &kustomizev1.Kustomization{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: name}}
	require.Eventually(t, func() bool {
		_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, kustomization, func() error {
			kustomization.Spec = kustomizev1.KustomizationSpec{
				Path: "./key-vault-sops",
				SourceRef: kustomizev1.CrossNamespaceSourceReference{
					Kind:      sourcev1.GitRepositoryKind,
					Name:      source.Name,
					Namespace: source.Namespace,
				},
				Interval: metav1.Duration{Duration: 1 * time.Minute},
				Prune:    true,
				Decryption: &kustomizev1.Decryption{
					Provider: "sops",
				},
			}
			return nil
		})
		if err != nil {
			return false
		}
		return true
	}, 10*time.Second, 1*time.Second)

	require.Eventually(t, func() bool {
		nn := types.NamespacedName{Name: "test", Namespace: name}
		secret := &corev1.Secret{}
		err = cfg.kubeClient.Get(ctx, nn, secret)
		if err != nil {
			return false
		}
		return true
	}, 120*time.Second, 5*time.Second)
}

func TestAzureDevOpsCommitStatus(t *testing.T) {
	ctx := context.TODO()
	name := "commit-status"
	repoUrl := cfg.applicationRepository.http
	manifest := fmt.Sprintf(`
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: foobar
      namespace: %s
  `, name)

	repo, repoDir, err := getRepository(repoUrl, name, true, cfg.azdoPat)
	require.NoError(t, err)
	err = addFile(repoDir, "configmap.yaml", manifest)
	require.NoError(t, err)
	err = commitAndPushAll(repo, name, cfg.azdoPat)
	require.NoError(t, err)

	err = setupNamespace(ctx, cfg.kubeClient, repoUrl, cfg.azdoPat, name)
	require.NoError(t, err)

	kustomization := &kustomizev1.Kustomization{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: name}}
	require.Eventually(t, func() bool {
		_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, kustomization, func() error {
			kustomization.Spec.HealthChecks = []meta.NamespacedObjectKindReference{
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       "foobar",
					Namespace:  name,
				},
			}
			return nil
		})
		if err != nil {
			return false
		}
		return true
	}, 10*time.Second, 1*time.Second)

	require.Eventually(t, func() bool {
		err := verifyGitAndKustomization(ctx, cfg.kubeClient, name, name)
		if err != nil {
			return false
		}
		return true
	}, 10*time.Second, 1*time.Second)

	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "azuredevops-token",
			Namespace: name,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, &secret, func() error {
		secret.StringData = map[string]string{
			"token": cfg.azdoPat,
		}
		return nil
	})
	provider := notiv1beta1.Provider{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "azuredevops",
			Namespace: name,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, &provider, func() error {
		provider.Spec = notiv1beta1.ProviderSpec{
			Type:    "azuredevops",
			Address: repoUrl,
			SecretRef: &meta.LocalObjectReference{
				Name: "azuredevops-token",
			},
		}
		return nil
	})
	require.NoError(t, err)
	alert := notiv1beta1.Alert{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "azuredevops",
			Namespace: name,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, &alert, func() error {
		alert.Spec = notiv1beta1.AlertSpec{
			ProviderRef: meta.LocalObjectReference{
				Name: provider.Name,
			},
			EventSources: []notiv1beta1.CrossNamespaceObjectReference{
				{
					Kind:      "Kustomization",
					Name:      name,
					Namespace: name,
				},
			},
		}
		return nil
	})
	require.NoError(t, err)

	u, err := giturls.Parse(repoUrl)
	require.NoError(t, err)
	id := strings.TrimLeft(u.Path, "/")
	id = strings.TrimSuffix(id, ".git")
	comp := strings.Split(id, "/")
	orgUrl := fmt.Sprintf("%s://%s/%v", u.Scheme, u.Host, comp[0])
	project := comp[1]
	repoId := comp[3]
	branch, err := repo.LookupBranch(name, git2go.BranchLocal)
	require.NoError(t, err)
	commit, err := repo.LookupCommit(branch.Target())
	rev := commit.Id().String()
	connection := azuredevops.NewPatConnection(orgUrl, cfg.azdoPat)
	client, err := git.NewClient(ctx, connection)
	require.NoError(t, err)
	getArgs := git.GetStatusesArgs{
		Project:      &project,
		RepositoryId: &repoId,
		CommitId:     &rev,
	}
	require.Eventually(t, func() bool {
		statuses, err := client.GetStatuses(ctx, getArgs)
		if err != nil {
			return false
		}
		if len(*statuses) != 1 {
			return false
		}
		return true
	}, 500*time.Second, 5*time.Second)
}

func TestEventHubNotification(t *testing.T) {
	ctx := context.TODO()
	name := "event-hub"

	// Start listening to eventhub with latest offset
	hub, err := eventhub.NewHubFromConnectionString(cfg.eventHubSas)
	require.NoError(t, err)
	c := make(chan string, 10)
	handler := func(ctx context.Context, event *eventhub.Event) error {
		c <- string(event.Data)
		return nil
	}
	runtimeInfo, err := hub.GetRuntimeInformation(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(runtimeInfo.PartitionIDs))
	listenerHandler, err := hub.Receive(ctx, runtimeInfo.PartitionIDs[0], handler, eventhub.ReceiveWithLatestOffset())
	require.NoError(t, err)

	// Setup Flux resources
	repoUrl := cfg.applicationRepository.http
	manifest := fmt.Sprintf(`
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: foobar
      namespace: %s
  `, name)

	repo, repoDir, err := getRepository(repoUrl, name, true, cfg.azdoPat)
	require.NoError(t, err)
	err = addFile(repoDir, "configmap.yaml", manifest)
	require.NoError(t, err)
	err = commitAndPushAll(repo, name, cfg.azdoPat)
	require.NoError(t, err)

	err = setupNamespace(ctx, cfg.kubeClient, repoUrl, cfg.azdoPat, name)
	require.NoError(t, err)

	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: name,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, &secret, func() error {
		secret.StringData = map[string]string{
			"address": cfg.eventHubSas,
		}
		return nil
	})
	provider := notiv1beta1.Provider{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: name,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, &provider, func() error {
		provider.Spec = notiv1beta1.ProviderSpec{
			Type:    "azureeventhub",
			Address: repoUrl,
			SecretRef: &meta.LocalObjectReference{
				Name: name,
			},
		}
		return nil
	})
	require.NoError(t, err)
	alert := notiv1beta1.Alert{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: name,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, &alert, func() error {
		alert.Spec = notiv1beta1.AlertSpec{
			ProviderRef: meta.LocalObjectReference{
				Name: provider.Name,
			},
			EventSources: []notiv1beta1.CrossNamespaceObjectReference{
				{
					Kind:      "Kustomization",
					Name:      name,
					Namespace: name,
				},
			},
		}
		return nil
	})
	require.NoError(t, err)

	kustomization := &kustomizev1.Kustomization{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: name}}
	require.Eventually(t, func() bool {
		_, err := controllerutil.CreateOrUpdate(ctx, cfg.kubeClient, kustomization, func() error {
			kustomization.Spec.HealthChecks = []meta.NamespacedObjectKindReference{
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       "foobar",
					Namespace:  name,
				},
			}
			return nil
		})
		if err != nil {
			return false
		}
		return true
	}, 10*time.Second, 1*time.Second)

	require.Eventually(t, func() bool {
		err := verifyGitAndKustomization(ctx, cfg.kubeClient, name, name)
		if err != nil {
			return false
		}
		return true
	}, 60*time.Second, 5*time.Second)

	// Wait to read even from event hub
	require.Eventually(t, func() bool {
		select {
		case eventJson := <-c:
			event := &events.Event{}
			err := json.Unmarshal([]byte(eventJson), event)
			if err != nil {
				fmt.Println(err)
				return false
			}
			if event.InvolvedObject.Kind != "Kustomization" {
				return false
			}
			if event.Message != "Health check passed" {
				return false
			}
			return true
		default:
			return false
		}
		//}, 700*time.Second, 10*time.Second)
	}, 60*time.Second, 1*time.Second)
	err = listenerHandler.Close(ctx)
	require.NoError(t, err)
	err = hub.Close(ctx)
	require.NoError(t, err)
}

// TODO: Enable when source-controller supports Helm charts from OCI sources.
/*func TestACRHelmRelease(t *testing.T) {
	ctx := context.TODO()

	// Create namespace for test
	namespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "acr-helm-release",
		},
	}
	err := kubeClient.Create(ctx, &namespace)
	require.NoError(t, err)
	defer func() {
		kubeClient.Delete(ctx, &namespace)
		require.NoError(t, err)
	}()

	// Copy ACR credentials to new namespace
	acrNn := types.NamespacedName{
		Name:      "acr-helm",
		Namespace: "flux-system",
	}
	acrSecret := corev1.Secret{}
	err = kubeClient.Get(ctx, acrNn, &acrSecret)
	require.NoError(t, err)
	acrSecret.ObjectMeta = metav1.ObjectMeta{
		Name:      acrSecret.Name,
		Namespace: namespace.Name,
	}
	err = kubeClient.Create(ctx, &acrSecret)
	require.NoError(t, err)

	// Create HelmRepository and wait for it to sync
	helmRepository := sourcev1.HelmRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "acr",
			Namespace: namespace.Name,
		},
		Spec: sourcev1.HelmRepositorySpec{
			URL: "https://acrappsoarfish.azurecr.io/helm/podinfo",
			Interval: metav1.Duration{
				Duration: 5 * time.Minute,
			},
			SecretRef: &meta.LocalObjectReference{
				Name: acrSecret.Name,
			},
			PassCredentials: true,
		},
	}
	err = kubeClient.Create(ctx, &helmRepository)
	require.NoError(t, err)
}*/
