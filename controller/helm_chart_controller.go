package controllers

import (
	_ "bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/go-logr/logr"
	"gopkg.in/yaml.v3"
	"happyhelm.sh/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"os"
	"os/exec"
	"path/filepath"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

type HappyHelmReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

func specHash(app *v1alpha1.DeployChart) (string, error) {
	data, err := json.Marshal(app.Spec)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func (r *HappyHelmReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("happyHelm", req.NamespacedName)

	var app v1alpha1.DeployChart
	err := r.Get(ctx, req.NamespacedName, &app)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Resource deleted
			log.Info("DeployChart resource deleted, uninstalling Helm release", "release", req.Name)
			if err := r.uninstall(ctx, req.Namespace, req.Name); err != nil {
				log.Error(err, "Failed to uninstall Helm chart on delete")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		// Other errors
		return ctrl.Result{}, err
	}

	currentHash, err := specHash(&app)
	if err != nil {
		log.Error(err, "Failed to calculate spec hash")
		return ctrl.Result{}, err
	}

	// If spec changed, redeploy
	if app.Status.Processed == false || app.Status.LastAppliedHash != currentHash {
		log.Info("Deploying or upgrading chart", "Chart", app.Spec.Chart)

		if err := r.deploy(ctx, &app); err != nil {
			log.Error(err, "Failed to deploy Helm chart")
			return ctrl.Result{}, err
		}

		app.Status.Processed = true
		app.Status.LastAppliedHash = currentHash
		app.Status.State = "Succeeded"
		if err := r.Status().Update(ctx, &app); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *HappyHelmReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.DeployChart{}).
		Complete(r)
}

func (r *HappyHelmReconciler) deploy(ctx context.Context, app *v1alpha1.DeployChart) error {
	helmCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	app.Status.State = "Pending"

	releaseName := app.Name
	targetNamespace := app.Namespace
	chartName := app.Spec.Chart.Chart
	chartVersion := app.Spec.Chart.Version
	values := app.Spec.Values

	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("helm-values-%s-", releaseName))
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	valuesYAML, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("failed to marshal values: %w", err)
	}
	valuesFile := filepath.Join(tmpDir, "values.yaml")
	if err := os.WriteFile(valuesFile, valuesYAML, 0644); err != nil {
		return fmt.Errorf("failed to write values.yaml: %w", err)
	}

	chartRef := fmt.Sprintf("%s/%s", app.Spec.Chart.Repo, chartName)
	cmdDeploy := exec.CommandContext(helmCtx, "helm", "upgrade", "--install", releaseName, chartRef,
		"--namespace", targetNamespace,
		"-f", valuesFile,
		"--version", chartVersion,
		"--create-namespace",
	)

	if out, err := cmdDeploy.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to install or upgrade chart: %w, output: %s", err, string(out))
	}
	app.Status.Processed = true
	app.Status.State = "Succeeded"
	r.Log.Info("Helm deploy succeeded", "release", releaseName)
	return nil
}

func (r *HappyHelmReconciler) uninstall(ctx context.Context, namespace string, appName string) error {
	helmCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	fmt.Println(namespace, appName)
	cmdUninstall := exec.CommandContext(helmCtx, "helm", "uninstall", appName, "--namespace", namespace)
	if out, err := cmdUninstall.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to uninstall release: %w, output: %s", err, string(out))
	}

	r.Log.Info("Helm uninstall succeeded", "release", appName)
	return nil
}
