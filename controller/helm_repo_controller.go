package controllers

import (
	"context"
	"github.com/go-logr/logr"
	"happyhelm.sh/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"os/exec"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type HelmRepoReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func (r *HelmRepoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("Repository", req.NamespacedName)

	var repo v1alpha1.Repository
	err := r.Get(ctx, req.NamespacedName, &repo)
	if err != nil {
		log.Info("HelmRepo resource deleted")
		deleteHelmRepo(log, req.Name)

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !repo.Status.Processed {
		log.Info("Reconciling Helm Repository", "name", repo.Name)
		addErr := addHelmRepo(log, repo.Name, repo.Spec.URL, repo.Spec.Username, repo.Spec.Password, repo.Spec.HasCredentials)
		if addErr != nil {
			log.Error(addErr, "Failed to add Helm repo")
			return ctrl.Result{}, addErr
		}
		updateHelmRepos(log)

		repo.Status.Processed = true
		if err := r.Status().Update(ctx, &repo); err != nil {
			log.Error(err, "Failed to update HelmRepo status")
			return ctrl.Result{}, err
		}
		log.Info("Helm Repository reconciled successfully", "name", repo.Name)
	}

	return ctrl.Result{}, nil
}

func (r *HelmRepoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Repository{}).
		Complete(r)
}

func addHelmRepo(logger logr.Logger, name string, url string, username string, password string, hasCredentials bool) error {
	args := []string{"repo", "add", name, url, "--force-update"}
	if hasCredentials {
		args = append(args, "--username", username, "--password", password)
	}
	cmd := exec.Command("helm", args...)
	output, err := cmd.CombinedOutput()
	logger.Info("Adding Helm repo", "name", name, "url", url, "output", string(output))
	if err != nil {
		logger.Error(err, "Failed to add Helm repo", "name", name, "url", url, "output", string(output))
	}
	return nil
}

func updateHelmRepos(logger logr.Logger) {
	cmd := exec.Command("helm", "repo", "update")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error(err, "Failed to update Helm repos", "output", string(output))
	}
	logger.Info("Helm repos updated successfully", "output", string(output))
}

func deleteHelmRepo(logger logr.Logger, name string) {
	cmd := exec.Command("helm", "repo", "remove", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error(err, "Failed to remove Helm repo", "name", name, "output", string(output))
	}
}
