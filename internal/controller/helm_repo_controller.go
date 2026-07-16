package controllers

import (
	"context"

	"github.com/go-logr/logr"
	helmv1alpha1 "happyhelm.sh/api/helm/v1alpha1"
	helmclient "happyhelm.sh/internal/helm"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type HelmRepoReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Helm   helmclient.Client
}

func (r *HelmRepoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("Repository", req.NamespacedName)

	var repository helmv1alpha1.Repository
	if err := r.Get(ctx, req.NamespacedName, &repository); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if repository.Status.Processed && repository.Status.ObservedGeneration == repository.Generation {
		return ctrl.Result{}, nil
	}

	if err := r.Helm.ValidateRepository(ctx, &repository); err != nil {
		log.Error(err, "failed to validate Helm repository")
		return ctrl.Result{}, err
	}
	repository.Status.Processed = true
	repository.Status.ObservedGeneration = repository.Generation
	if err := r.Status().Update(ctx, &repository); err != nil {
		return ctrl.Result{}, err
	}
	log.Info("Helm repository validated successfully")
	return ctrl.Result{}, nil
}

func (r *HelmRepoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&helmv1alpha1.Repository{}).
		Complete(r)
}
