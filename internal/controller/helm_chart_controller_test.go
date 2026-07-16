package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	helmv1alpha1 "happyhelm.sh/api/helm/v1alpha1"
	helmclient "happyhelm.sh/internal/helm"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type recordingHelmClient struct {
	installIdentity   helmv1alpha1.CreatorIdentity
	uninstallIdentity helmv1alpha1.CreatorIdentity
	installCalls      int
	uninstallCalls    int
}

func (r *recordingHelmClient) ValidateRepository(context.Context, *helmv1alpha1.Repository) error {
	return nil
}

func (r *recordingHelmClient) InstallOrUpgrade(_ context.Context, _ *helmv1alpha1.DeployChart, _ *helmv1alpha1.Repository, identity helmv1alpha1.CreatorIdentity) error {
	r.installCalls++
	r.installIdentity = identity
	return nil
}

func (r *recordingHelmClient) Uninstall(_ context.Context, _, _ string, identity helmv1alpha1.CreatorIdentity) error {
	r.uninstallCalls++
	r.uninstallIdentity = identity
	return nil
}

var _ helmclient.Client = &recordingHelmClient{}

func TestDeployChartReconcilePassesCreatorIdentityToHelm(t *testing.T) {
	identity := helmv1alpha1.CreatorIdentity{Username: "john.anderson", Groups: []string{"developers", "system:authenticated"}}
	deploy := testDeployChart(t, identity)
	deploy.Finalizers = []string{deployChartFinalizer}
	repository := &helmv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "stable"},
		Spec:       helmv1alpha1.HelmRepoSpec{URL: "https://charts.example.com"},
	}
	helm := &recordingHelmClient{}
	reconciler := newHelmChartTestReconciler(t, helm, deploy, repository)

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(deploy)}); err != nil {
		t.Fatalf("Reconcile returned an error: %v", err)
	}
	if helm.installCalls != 1 {
		t.Fatalf("InstallOrUpgrade calls = %d, want 1", helm.installCalls)
	}
	if helm.installIdentity.Username != identity.Username || len(helm.installIdentity.Groups) != len(identity.Groups) {
		t.Fatalf("InstallOrUpgrade identity = %#v, want %#v", helm.installIdentity, identity)
	}

	var updated helmv1alpha1.DeployChart
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(deploy), &updated); err != nil {
		t.Fatalf("get reconciled DeployChart: %v", err)
	}
	if !updated.Status.Processed || updated.Status.ExecutedAs != identity.Username {
		t.Fatalf("status = %#v, want processed release executed as creator", updated.Status)
	}
}

func TestDeployChartDeletionPassesCreatorIdentityToUninstall(t *testing.T) {
	identity := helmv1alpha1.CreatorIdentity{Username: "john.anderson", Groups: []string{"developers"}}
	deploy := testDeployChart(t, identity)
	deploy.Finalizers = []string{deployChartFinalizer}
	deletedAt := metav1.NewTime(time.Now())
	deploy.DeletionTimestamp = &deletedAt
	helm := &recordingHelmClient{}
	reconciler := newHelmChartTestReconciler(t, helm, deploy)

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(deploy)}); err != nil {
		t.Fatalf("Reconcile returned an error: %v", err)
	}
	if helm.uninstallCalls != 1 || helm.uninstallIdentity.Username != identity.Username {
		t.Fatalf("Uninstall calls = %d, identity = %#v", helm.uninstallCalls, helm.uninstallIdentity)
	}
}

func TestDeployChartWithoutTrustedIdentityNeverCallsHelm(t *testing.T) {
	deploy := testDeployChart(t, helmv1alpha1.CreatorIdentity{})
	helm := &recordingHelmClient{}
	reconciler := newHelmChartTestReconciler(t, helm, deploy)

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(deploy)}); err != nil {
		t.Fatalf("Reconcile returned an error: %v", err)
	}
	if helm.installCalls != 0 || helm.uninstallCalls != 0 {
		t.Fatalf("Helm was called without a trusted identity: install=%d uninstall=%d", helm.installCalls, helm.uninstallCalls)
	}

	var updated helmv1alpha1.DeployChart
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(deploy), &updated); err != nil {
		t.Fatalf("get reconciled DeployChart: %v", err)
	}
	if updated.Status.State != "Failed" {
		t.Fatalf("status state = %q, want Failed", updated.Status.State)
	}
}

func newHelmChartTestReconciler(t *testing.T, helm helmclient.Client, objects ...client.Object) *HappyHelmReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := helmv1alpha1.DeployAddToScheme(scheme); err != nil {
		t.Fatalf("register DeployChart API: %v", err)
	}
	if err := helmv1alpha1.HelmRepoAddToScheme(scheme); err != nil {
		t.Fatalf("register Repository API: %v", err)
	}
	return &HappyHelmReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&helmv1alpha1.DeployChart{}).WithObjects(objects...).Build(),
		Scheme: scheme,
		Log:    logr.Discard(),
		Helm:   helm,
	}
}

func testDeployChart(t *testing.T, identity helmv1alpha1.CreatorIdentity) *helmv1alpha1.DeployChart {
	t.Helper()
	deploy := &helmv1alpha1.DeployChart{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "apps"},
		Spec: helmv1alpha1.DeployChartSpec{
			Chart: helmv1alpha1.ChartSpec{Repo: "stable", Chart: "example", Version: "1.2.3"},
		},
	}
	if identity.Username != "" {
		annotations, err := helmv1alpha1.SetCreatorIdentity(deploy.Annotations, identity)
		if err != nil {
			t.Fatalf("set creator identity: %v", err)
		}
		deploy.Annotations = annotations
	}
	return deploy
}
