package helm

import (
	"testing"

	helmv1alpha1 "happyhelm.sh/api/helm/v1alpha1"
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
