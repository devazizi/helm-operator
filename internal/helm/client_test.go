package helm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	helmv1alpha1 "happyhelm.sh/api/helm/v1alpha1"
	"helm.sh/helm/v4/pkg/action"
)

func TestNewSettingsConfiguresImpersonation(t *testing.T) {
	identity := helmv1alpha1.CreatorIdentity{
		Username: "john.anderson",
		Groups:   []string{"developers", "system:authenticated"},
	}
	settings, cleanup, err := newSettings("production", identity)
	if err != nil {
		t.Fatalf("newSettings returned an error: %v", err)
	}
	defer cleanup()

	if settings.Namespace() != "production" {
		t.Fatalf("namespace = %q, want production", settings.Namespace())
	}
	if settings.KubeAsUser != identity.Username {
		t.Fatalf("KubeAsUser = %q, want %q", settings.KubeAsUser, identity.Username)
	}
	if len(settings.KubeAsGroups) != 2 || settings.KubeAsGroups[0] != "developers" {
		t.Fatalf("KubeAsGroups = %v, want %v", settings.KubeAsGroups, identity.Groups)
	}
}

func TestNewSDKClientUsesConfiguredChartCache(t *testing.T) {
	t.Setenv(chartCacheEnvironmentVariable, "/var/cache/helm")
	if got := NewSDKClient().ChartCache; got != "/var/cache/helm" {
		t.Fatalf("ChartCache = %q, want /var/cache/helm", got)
	}
}

func TestPersistChartArchiveUsesTarGZCacheFile(t *testing.T) {
	cacheRoot := t.TempDir()
	deploy := &helmv1alpha1.DeployChart{Spec: helmv1alpha1.DeployChartSpec{Chart: helmv1alpha1.ChartSpec{
		Chart:   "example",
		Version: "1.2.3",
	}}}
	repository := &helmv1alpha1.Repository{Spec: helmv1alpha1.HelmRepoSpec{URL: "https://charts.example.com"}}
	cachePath := chartArchiveCachePath(cacheRoot, deploy, repository)
	if !strings.HasSuffix(cachePath, ".tgz") {
		t.Fatalf("cache path = %q, want a .tgz archive", cachePath)
	}

	sourcePath := filepath.Join(t.TempDir(), "downloaded.chart")
	want := []byte("tar-gzip-content")
	if err := os.WriteFile(sourcePath, want, 0o600); err != nil {
		t.Fatalf("write source chart: %v", err)
	}
	if err := persistChartArchive(sourcePath, cachePath); err != nil {
		t.Fatalf("persistChartArchive returned an error: %v", err)
	}
	got, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached chart: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("cached chart = %q, want %q", got, want)
	}
}

func TestLocateChartReusesPersistentArchiveWithoutNetworkSettings(t *testing.T) {
	cacheRoot := t.TempDir()
	deploy := &helmv1alpha1.DeployChart{Spec: helmv1alpha1.DeployChartSpec{Chart: helmv1alpha1.ChartSpec{
		Chart:   "example",
		Version: "1.2.3",
	}}}
	repository := &helmv1alpha1.Repository{Spec: helmv1alpha1.HelmRepoSpec{URL: "https://unreachable.example.com"}}
	cachePath := chartArchiveCachePath(cacheRoot, deploy, repository)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("create cache directory: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("cached-chart"), 0o644); err != nil {
		t.Fatalf("write cached chart: %v", err)
	}

	client := &SDKClient{ChartCache: cacheRoot}
	got, err := client.locateChart(deploy, repository, nil, action.ChartPathOptions{})
	if err != nil {
		t.Fatalf("locateChart returned an error for a cache hit: %v", err)
	}
	if got != cachePath {
		t.Fatalf("locateChart = %q, want %q", got, cachePath)
	}
}

func TestChartPathOptionsUsesRepositoryCredentialsOnlyWhenEnabled(t *testing.T) {
	deploy := &helmv1alpha1.DeployChart{Spec: helmv1alpha1.DeployChartSpec{Chart: helmv1alpha1.ChartSpec{Version: "1.2.3"}}}
	repository := &helmv1alpha1.Repository{Spec: helmv1alpha1.HelmRepoSpec{
		URL:            "https://charts.example.com",
		HasCredentials: true,
		Username:       "john",
		Password:       "secret",
	}}

	options := chartPathOptions(deploy, repository)
	if options.RepoURL != repository.Spec.URL || options.Version != "1.2.3" || options.Username != "john" || options.Password != "secret" {
		t.Fatalf("unexpected chart options: %#v", options)
	}
	repository.Spec.HasCredentials = false
	options = chartPathOptions(deploy, repository)
	if options.Username != "" || options.Password != "" {
		t.Fatalf("credentials were used while disabled: %#v", options)
	}
}
