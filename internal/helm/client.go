package helm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	helmv1alpha1 "happyhelm.sh/api/helm/v1alpha1"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/getter"
	repo "helm.sh/helm/v4/pkg/repo/v1"
	"helm.sh/helm/v4/pkg/storage/driver"
)

const defaultActionTimeout = 5 * time.Minute

type Client interface {
	ValidateRepository(context.Context, *helmv1alpha1.Repository) error
	InstallOrUpgrade(context.Context, *helmv1alpha1.DeployChart, *helmv1alpha1.Repository, helmv1alpha1.CreatorIdentity) error
	Uninstall(context.Context, string, string, helmv1alpha1.CreatorIdentity) error
}

type SDKClient struct{}

func NewSDKClient() *SDKClient {
	return &SDKClient{}
}

func (c *SDKClient) ValidateRepository(_ context.Context, repository *helmv1alpha1.Repository) error {
	settings, cleanup, err := newSettings("default", helmv1alpha1.CreatorIdentity{})
	if err != nil {
		return err
	}
	defer cleanup()

	entry := &repo.Entry{
		Name: repository.Name,
		URL:  repository.Spec.URL,
	}
	if repository.Spec.HasCredentials {
		entry.Username = repository.Spec.Username
		entry.Password = repository.Spec.Password
	}

	chartRepository, err := repo.NewChartRepository(entry, getter.All(settings))
	if err != nil {
		return fmt.Errorf("initialize Helm repository %q: %w", repository.Name, err)
	}
	chartRepository.CachePath = settings.RepositoryCache
	if _, err := chartRepository.DownloadIndexFile(); err != nil {
		return fmt.Errorf("download index for Helm repository %q: %w", repository.Name, err)
	}
	return nil
}

func (c *SDKClient) InstallOrUpgrade(ctx context.Context, deploy *helmv1alpha1.DeployChart, repository *helmv1alpha1.Repository, identity helmv1alpha1.CreatorIdentity) error {
	settings, cleanup, err := newSettings(deploy.Namespace, identity)
	if err != nil {
		return err
	}
	defer cleanup()

	actionConfig, err := newActionConfiguration(settings, deploy.Namespace)
	if err != nil {
		return err
	}

	chartOptions := chartPathOptions(deploy, repository)
	chartPath, err := chartOptions.LocateChart(deploy.Spec.Chart.Chart, settings)
	if err != nil {
		return fmt.Errorf("locate chart %q in repository %q: %w", deploy.Spec.Chart.Chart, repository.Name, err)
	}
	loadedChart, err := loader.Load(chartPath)
	if err != nil {
		return fmt.Errorf("load chart %q: %w", chartPath, err)
	}

	_, getErr := action.NewGet(actionConfig).Run(deploy.Name)
	switch {
	case getErr == nil:
		upgrade := action.NewUpgrade(actionConfig)
		upgrade.Namespace = deploy.Namespace
		upgrade.Timeout = defaultActionTimeout
		upgrade.ChartPathOptions = chartOptions
		if _, err := upgrade.RunWithContext(ctx, deploy.Name, loadedChart, deploy.Spec.Values); err != nil {
			return fmt.Errorf("upgrade Helm release %q as %q: %w", deploy.Name, identity.Username, err)
		}
		return nil
	case errors.Is(getErr, driver.ErrReleaseNotFound):
		install := action.NewInstall(actionConfig)
		install.ReleaseName = deploy.Name
		install.Namespace = deploy.Namespace
		install.Timeout = defaultActionTimeout
		install.ChartPathOptions = chartOptions
		if _, err := install.RunWithContext(ctx, loadedChart, deploy.Spec.Values); err != nil {
			return fmt.Errorf("install Helm release %q as %q: %w", deploy.Name, identity.Username, err)
		}
		return nil
	default:
		return fmt.Errorf("inspect Helm release %q as %q: %w", deploy.Name, identity.Username, getErr)
	}
}

func (c *SDKClient) Uninstall(_ context.Context, namespace, releaseName string, identity helmv1alpha1.CreatorIdentity) error {
	settings, cleanup, err := newSettings(namespace, identity)
	if err != nil {
		return err
	}
	defer cleanup()

	actionConfig, err := newActionConfiguration(settings, namespace)
	if err != nil {
		return err
	}
	uninstall := action.NewUninstall(actionConfig)
	uninstall.Timeout = defaultActionTimeout
	uninstall.IgnoreNotFound = true
	if _, err := uninstall.Run(releaseName); err != nil {
		return fmt.Errorf("uninstall Helm release %q as %q: %w", releaseName, identity.Username, err)
	}
	return nil
}

func newActionConfiguration(settings *cli.EnvSettings, namespace string) (*action.Configuration, error) {
	discardLogger := slog.NewTextHandler(io.Discard, nil)
	configuration := action.NewConfiguration(action.ConfigurationSetLogger(discardLogger))
	if err := configuration.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER")); err != nil {
		return nil, fmt.Errorf("initialize Helm action configuration: %w", err)
	}
	return configuration, nil
}

func newSettings(namespace string, identity helmv1alpha1.CreatorIdentity) (*cli.EnvSettings, func(), error) {
	tmpDir, err := os.MkdirTemp("", "helm-operator-sdk-")
	if err != nil {
		return nil, nil, fmt.Errorf("create Helm SDK temporary directory: %w", err)
	}

	settings := cli.New()
	settings.SetNamespace(namespace)
	settings.KubeAsUser = identity.Username
	settings.KubeAsGroups = append([]string(nil), identity.Groups...)
	settings.RepositoryCache = filepath.Join(tmpDir, "repository")
	settings.RepositoryConfig = filepath.Join(tmpDir, "repositories.yaml")
	settings.ContentCache = filepath.Join(tmpDir, "content")
	settings.RegistryConfig = filepath.Join(tmpDir, "registry.json")
	return settings, func() { _ = os.RemoveAll(tmpDir) }, nil
}

func chartPathOptions(deploy *helmv1alpha1.DeployChart, repository *helmv1alpha1.Repository) action.ChartPathOptions {
	options := action.ChartPathOptions{
		RepoURL: repository.Spec.URL,
		Version: deploy.Spec.Chart.Version,
	}
	if repository.Spec.HasCredentials {
		options.Username = repository.Spec.Username
		options.Password = repository.Spec.Password
	}
	return options
}

var _ Client = &SDKClient{}
