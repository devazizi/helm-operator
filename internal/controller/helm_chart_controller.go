package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	helmv1alpha1 "happyhelm.sh/api/helm/v1alpha1"
	helmclient "happyhelm.sh/internal/helm"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const deployChartFinalizer = "helm.k8s.ir/release-cleanup"

type HappyHelmReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
	Helm   helmclient.Client
}

func specHash(app *helmv1alpha1.DeployChart) (string, error) {
	data, err := json.Marshal(app.Spec)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func (r *HappyHelmReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("DeployChart", req.NamespacedName)

	var deploy helmv1alpha1.DeployChart
	if err := r.Get(ctx, req.NamespacedName, &deploy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	identity, err := helmv1alpha1.IdentityFromAnnotations(deploy.Annotations)
	if err != nil {
		statusErr := r.setDeployStatus(ctx, &deploy, false, "Failed", err.Error(), "", "")
		return ctrl.Result{}, statusErr
	}

	if !deploy.DeletionTimestamp.IsZero() {
		if !containsString(deploy.Finalizers, deployChartFinalizer) {
			return ctrl.Result{}, nil
		}
		if err := r.Helm.Uninstall(ctx, deploy.Namespace, deploy.Name, identity); err != nil {
			log.Error(err, "failed to uninstall Helm release", "executedAs", identity.Username)
			return ctrl.Result{}, err
		}
		deploy.Finalizers = removeString(deploy.Finalizers, deployChartFinalizer)
		return ctrl.Result{}, r.Update(ctx, &deploy)
	}

	if !containsString(deploy.Finalizers, deployChartFinalizer) {
		deploy.Finalizers = append(deploy.Finalizers, deployChartFinalizer)
		if err := r.Update(ctx, &deploy); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	currentHash, err := specHash(&deploy)
	if err != nil {
		return ctrl.Result{}, err
	}
	if deploy.Status.Processed && deploy.Status.LastAppliedHash == currentHash {
		return ctrl.Result{}, nil
	}

	var repository helmv1alpha1.Repository
	if err := r.Get(ctx, types.NamespacedName{Name: deploy.Spec.Chart.Repo}, &repository); err != nil {
		message := fmt.Sprintf("get Helm repository %q: %v", deploy.Spec.Chart.Repo, err)
		statusErr := r.setDeployStatus(ctx, &deploy, false, "Failed", message, "", identity.Username)
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, errors.Join(err, statusErr)
	}

	log.Info("installing or upgrading Helm release with impersonation", "executedAs", identity.Username, "chart", deploy.Spec.Chart.Chart)
	if err := r.Helm.InstallOrUpgrade(ctx, &deploy, &repository, identity); err != nil {
		log.Error(err, "Helm SDK action failed", "executedAs", identity.Username)
		statusErr := r.setDeployStatus(ctx, &deploy, false, "Failed", err.Error(), "", identity.Username)
		return ctrl.Result{}, errors.Join(err, statusErr)
	}

	if err := r.setDeployStatus(ctx, &deploy, true, "Succeeded", "Helm release is synchronized", currentHash, identity.Username); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *HappyHelmReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&helmv1alpha1.DeployChart{}).
		Watches(&helmv1alpha1.Repository{}, handler.EnqueueRequestsFromMapFunc(r.requestsForRepository)).
		Complete(r)
}

func (r *HappyHelmReconciler) requestsForRepository(ctx context.Context, repository client.Object) []reconcile.Request {
	var deploys helmv1alpha1.DeployChartList
	if err := r.List(ctx, &deploys); err != nil {
		r.Log.Error(err, "unable to list DeployCharts after Repository change", "repository", repository.GetName())
		return nil
	}
	requests := make([]reconcile.Request, 0)
	for i := range deploys.Items {
		if deploys.Items[i].Spec.Chart.Repo == repository.GetName() {
			requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      deploys.Items[i].Name,
				Namespace: deploys.Items[i].Namespace,
			}})
		}
	}
	return requests
}

func (r *HappyHelmReconciler) setDeployStatus(ctx context.Context, deploy *helmv1alpha1.DeployChart, processed bool, state, message, hash, executedAs string) error {
	oldStatus := deploy.Status
	deploy.Status.Processed = processed
	deploy.Status.State = state
	deploy.Status.Message = message
	deploy.Status.ExecutedAs = executedAs
	if hash != "" {
		deploy.Status.LastAppliedHash = hash
	}
	if reflect.DeepEqual(oldStatus, deploy.Status) {
		return nil
	}
	return r.Status().Update(ctx, deploy)
}
