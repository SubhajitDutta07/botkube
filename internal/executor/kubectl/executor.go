package kubectl

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubeshop/botkube/internal/command"
	"github.com/kubeshop/botkube/internal/executor/kubectl/accessreview"
	"github.com/kubeshop/botkube/internal/executor/kubectl/builder"
	"github.com/kubeshop/botkube/internal/loggerx"
	"github.com/kubeshop/botkube/pkg/api"
	"github.com/kubeshop/botkube/pkg/api/executor"
	"github.com/kubeshop/botkube/pkg/pluginx"
)

const (
	// PluginName is the name of the Helm Botkube plugin.
	PluginName       = "kubectl"
	defaultNamespace = "default"
	description      = "Run the Kubectl CLI commands directly from your favorite communication platform."
	kubectlVersion   = "v1.28.1"
)

var kcBinaryDownloadLinks = map[string]string{
	"windows/amd64": fmt.Sprintf("https://dl.k8s.io/release/%s/bin/windows/amd64/kubectl.exe", kubectlVersion),
	"darwin/amd64":  fmt.Sprintf("https://dl.k8s.io/release/%s/bin/darwin/amd64/kubectl", kubectlVersion),
	"darwin/arm64":  fmt.Sprintf("https://dl.k8s.io/release/%s/bin/darwin/arm64/kubectl", kubectlVersion),
	"linux/amd64":   fmt.Sprintf("https://dl.k8s.io/release/%s/bin/linux/amd64/kubectl", kubectlVersion),
	"linux/s390x":   fmt.Sprintf("https://dl.k8s.io/release/%s/bin/linux/s390x/kubectl", kubectlVersion),
	"linux/ppc64le": fmt.Sprintf("https://dl.k8s.io/release/%s/bin/linux/ppc64le/kubectl", kubectlVersion),
	"linux/arm64":   fmt.Sprintf("https://dl.k8s.io/release/%s/bin/linux/arm64/kubectl", kubectlVersion),
	"linux/386":     fmt.Sprintf("https://dl.k8s.io/release/%s/bin/linux/386/kubectl", kubectlVersion),
}

var _ executor.Executor = &Executor{}

type (
	kcRunner interface {
		RunKubectlCommand(ctx context.Context, kubeConfigPath, defaultNamespace, cmd string) (string, error)
	}
)

// Executor provides functionality for running Helm CLI.
type Executor struct {
	pluginVersion string
	kcRunner      kcRunner
}

// NewExecutor returns a new Executor instance.
func NewExecutor(ver string, kcRunner kcRunner) *Executor {
	return &Executor{
		pluginVersion: ver,
		kcRunner:      kcRunner,
	}
}

// Metadata returns details about Helm plugin.
func (e *Executor) Metadata(context.Context) (api.MetadataOutput, error) {
	return api.MetadataOutput{
		Version:     e.pluginVersion,
		Description: description,
		JSONSchema:  jsonSchema(description),
		Dependencies: map[string]api.Dependency{
			binaryName: {
				URLs: kcBinaryDownloadLinks,
			},
		},
	}, nil
}

// Execute returns a given command as response.
func (e *Executor) Execute(ctx context.Context, in executor.ExecuteInput) (executor.ExecuteOutput, error) {
	if err := pluginx.ValidateKubeConfigProvided(PluginName, in.Context.KubeConfig); err != nil {
		return executor.ExecuteOutput{}, err
	}

	cfg, err := MergeConfigs(in.Configs)
	if err != nil {
		return executor.ExecuteOutput{}, fmt.Errorf("while merging input configs: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return executor.ExecuteOutput{}, fmt.Errorf("while validating configuration: %w", err)
	}

	log := loggerx.New(cfg.Log)

	cmd, err := normalizeCommand(in.Command)
	if err != nil {
		return executor.ExecuteOutput{}, err
	}

	kubeConfigPath, deleteFn, err := pluginx.PersistKubeConfig(ctx, in.Context.KubeConfig)
	if err != nil {
		return executor.ExecuteOutput{}, fmt.Errorf("while writing kubeconfig file: %w", err)
	}
	defer func() {
		if deleteErr := deleteFn(ctx); deleteErr != nil {
			log.Errorf("failed to delete kubeconfig file %s: %w", kubeConfigPath, deleteErr)
		}
	}()

	scopedKubectlRunner := NewKubeconfigScopedRunner(e.kcRunner, kubeConfigPath)
	if builder.ShouldHandle(cmd) {
		guard, k8sCli, err := getBuilderDependencies(log, kubeConfigPath)
		if err != nil {
			return executor.ExecuteOutput{}, fmt.Errorf("while creating builder dependecies: %w", err)
		}

		kcBuilder := builder.NewKubectl(scopedKubectlRunner, cfg.InteractiveBuilder, log, guard, cfg.DefaultNamespace, k8sCli.CoreV1().Namespaces(), accessreview.NewK8sAuth(k8sCli.AuthorizationV1()))
		msg, err := kcBuilder.Handle(ctx, cmd, in.Context.IsInteractivitySupported, in.Context.SlackState)
		if err != nil {
			return executor.ExecuteOutput{}, fmt.Errorf("while running command builder: %w", err)
		}

		return executor.ExecuteOutput{
			Message: msg,
		}, nil
	}

	out, err := scopedKubectlRunner.RunKubectlCommand(ctx, cfg.DefaultNamespace, cmd)
	if err != nil {
		return executor.ExecuteOutput{}, err
	}
	return executor.ExecuteOutput{
		Message: api.NewCodeBlockMessage(out, true),
	}, nil
}

// Help returns help message.
func (*Executor) Help(context.Context) (api.Message, error) {
	return api.NewCodeBlockMessage(help(), true), nil
}

func getBuilderDependencies(log logrus.FieldLogger, kubeconfig string) (*command.CommandGuard, *kubernetes.Clientset, error) {
	kubeConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, nil, fmt.Errorf("while creating kube config: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(kubeConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("while creating discovery client: %w", err)
	}
	guard := command.NewCommandGuard(log, discoveryClient)
	k8sCli, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("while creating typed k8s client: %w", err)
	}

	return guard, k8sCli, nil
}
