package controllers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"reflect"
	"sort"

	"github.com/go-logr/logr"
	apiVersion "happyhelm.sh/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	reflectorFinalizer = "others.helm.k8s.ir/cleanup"

	reflectorUIDLabel = "others.helm.k8s.ir/reflector-uid"

	sourceNamespaceAnnotation    = "others.helm.k8s.ir/source-namespace"
	sourceNameAnnotation         = "others.helm.k8s.ir/source-name"
	sourceKindAnnotation         = "others.helm.k8s.ir/source-kind"
	reflectorNamespaceAnnotation = "others.helm.k8s.ir/reflector-namespace"
	reflectorNameAnnotation      = "others.helm.k8s.ir/reflector-name"
)

type ReflectorReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func (r *ReflectorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("reflector", req.NamespacedName)

	var reflectorResource apiVersion.Reflector
	if err := r.Get(ctx, req.NamespacedName, &reflectorResource); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !reflectorResource.DeletionTimestamp.IsZero() {
		if !containsString(reflectorResource.Finalizers, reflectorFinalizer) {
			return ctrl.Result{}, nil
		}
		if err := r.cleanupManagedObjects(ctx, &reflectorResource, nil, ""); err != nil {
			return ctrl.Result{}, err
		}
		reflectorResource.Finalizers = removeString(reflectorResource.Finalizers, reflectorFinalizer)
		return ctrl.Result{}, r.Update(ctx, &reflectorResource)
	}

	if !containsString(reflectorResource.Finalizers, reflectorFinalizer) {
		reflectorResource.Finalizers = append(reflectorResource.Finalizers, reflectorFinalizer)
		if err := r.Update(ctx, &reflectorResource); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if err := validateReflector(&reflectorResource); err != nil {
		statusErr := r.updateReflectorStatus(ctx, &reflectorResource, nil, metav1.ConditionFalse, "InvalidSpec", err.Error())
		return ctrl.Result{}, statusErr
	}

	targetNamespaces, err := r.matchingNamespaces(ctx, &reflectorResource)
	if err != nil {
		return ctrl.Result{}, err
	}

	var reconcileErr error
	successfulNamespaces := make([]string, 0, len(targetNamespaces))
	switch reflectorResource.Spec.Kind {
	case apiVersion.ReflectorKindSecret:
		var source corev1.Secret
		if err := r.Get(ctx, req.NamespacedName, &source); err != nil {
			if apierrors.IsNotFound(err) {
				cleanupErr := r.cleanupManagedObjects(ctx, &reflectorResource, nil, "")
				message := fmt.Sprintf("source Secret %s/%s was not found", req.Namespace, req.Name)
				statusErr := r.updateReflectorStatus(ctx, &reflectorResource, nil, metav1.ConditionFalse, "SourceNotFound", message)
				return ctrl.Result{}, errors.Join(cleanupErr, statusErr)
			}
			return ctrl.Result{}, err
		}
		for _, namespace := range targetNamespaces {
			if err := r.reflectSecret(ctx, &reflectorResource, &source, namespace); err != nil {
				reconcileErr = errors.Join(reconcileErr, err)
			} else {
				successfulNamespaces = append(successfulNamespaces, namespace)
			}
		}
	case apiVersion.ReflectorKindConfigMap:
		var source corev1.ConfigMap
		if err := r.Get(ctx, req.NamespacedName, &source); err != nil {
			if apierrors.IsNotFound(err) {
				cleanupErr := r.cleanupManagedObjects(ctx, &reflectorResource, nil, "")
				message := fmt.Sprintf("source ConfigMap %s/%s was not found", req.Namespace, req.Name)
				statusErr := r.updateReflectorStatus(ctx, &reflectorResource, nil, metav1.ConditionFalse, "SourceNotFound", message)
				return ctrl.Result{}, errors.Join(cleanupErr, statusErr)
			}
			return ctrl.Result{}, err
		}
		for _, namespace := range targetNamespaces {
			if err := r.reflectConfigMap(ctx, &reflectorResource, &source, namespace); err != nil {
				reconcileErr = errors.Join(reconcileErr, err)
			} else {
				successfulNamespaces = append(successfulNamespaces, namespace)
			}
		}
	}

	desired := make(map[string]struct{}, len(targetNamespaces))
	for _, namespace := range targetNamespaces {
		desired[namespace] = struct{}{}
	}
	reconcileErr = errors.Join(reconcileErr, r.cleanupManagedObjects(ctx, &reflectorResource, desired, reflectorResource.Spec.Kind))

	if reconcileErr != nil {
		statusErr := r.updateReflectorStatus(ctx, &reflectorResource, successfulNamespaces, metav1.ConditionFalse, "ReflectionFailed", reconcileErr.Error())
		return ctrl.Result{}, errors.Join(reconcileErr, statusErr)
	}

	message := fmt.Sprintf("reflected %s to %d namespace(s)", reflectorResource.Spec.Kind, len(targetNamespaces))
	if err := r.updateReflectorStatus(ctx, &reflectorResource, targetNamespaces, metav1.ConditionTrue, "ReflectionSucceeded", message); err != nil {
		return ctrl.Result{}, err
	}
	log.Info("reflection completed", "kind", reflectorResource.Spec.Kind, "namespaces", targetNamespaces)
	return ctrl.Result{}, nil
}

func (r *ReflectorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiVersion.Reflector{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.requestsForSecret)).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.requestsForConfigMap)).
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(r.requestsForNamespace)).
		Complete(r)
}

func validateReflector(reflectorResource *apiVersion.Reflector) error {
	if reflectorResource.Spec.Kind != apiVersion.ReflectorKindSecret && reflectorResource.Spec.Kind != apiVersion.ReflectorKindConfigMap {
		return fmt.Errorf("spec.kind must be Secret or ConfigMap")
	}
	if len(reflectorResource.Spec.ReflectTo) == 0 {
		return fmt.Errorf("spec.reflectTo must contain at least one namespace pattern")
	}
	for _, pattern := range reflectorResource.Spec.ReflectTo {
		if pattern == "" {
			return fmt.Errorf("spec.reflectTo cannot contain an empty pattern")
		}
		if _, err := path.Match(pattern, "namespace"); err != nil {
			return fmt.Errorf("invalid namespace pattern %q: %w", pattern, err)
		}
	}
	return nil
}

func (r *ReflectorReconciler) matchingNamespaces(ctx context.Context, reflectorResource *apiVersion.Reflector) ([]string, error) {
	var namespaces corev1.NamespaceList
	if err := r.List(ctx, &namespaces); err != nil {
		return nil, err
	}

	matched := make([]string, 0)
	for i := range namespaces.Items {
		name := namespaces.Items[i].Name
		if name == reflectorResource.Namespace {
			continue
		}
		for _, pattern := range reflectorResource.Spec.ReflectTo {
			ok, err := path.Match(pattern, name)
			if err != nil {
				return nil, err
			}
			if ok {
				matched = append(matched, name)
				break
			}
		}
	}
	sort.Strings(matched)
	return matched, nil
}

func (r *ReflectorReconciler) reflectSecret(ctx context.Context, reflectorResource *apiVersion.Reflector, source *corev1.Secret, namespace string) error {
	key := types.NamespacedName{Name: source.Name, Namespace: namespace}
	var target corev1.Secret
	err := r.Get(ctx, key, &target)
	if apierrors.IsNotFound(err) {
		target = corev1.Secret{
			ObjectMeta: reflectedObjectMeta(reflectorResource, namespace, apiVersion.ReflectorKindSecret),
			Data:       copyBytesMap(source.Data),
			Type:       source.Type,
		}
		return r.Create(ctx, &target)
	}
	if err != nil {
		return err
	}
	if !isManagedBy(&target.ObjectMeta, reflectorResource) {
		return fmt.Errorf("Secret %s/%s already exists and is not managed by Reflector %s/%s", namespace, source.Name, reflectorResource.Namespace, reflectorResource.Name)
	}

	annotations := reflectedAnnotations(reflectorResource, apiVersion.ReflectorKindSecret)
	labels := reflectedLabels(reflectorResource)
	if bytesMapEqual(target.Data, source.Data) && target.Type == source.Type && reflect.DeepEqual(target.Annotations, annotations) && reflect.DeepEqual(target.Labels, labels) {
		return nil
	}
	target.Data = copyBytesMap(source.Data)
	target.Type = source.Type
	target.Annotations = annotations
	target.Labels = labels
	return r.Update(ctx, &target)
}

func (r *ReflectorReconciler) reflectConfigMap(ctx context.Context, reflectorResource *apiVersion.Reflector, source *corev1.ConfigMap, namespace string) error {
	key := types.NamespacedName{Name: source.Name, Namespace: namespace}
	var target corev1.ConfigMap
	err := r.Get(ctx, key, &target)
	if apierrors.IsNotFound(err) {
		target = corev1.ConfigMap{
			ObjectMeta: reflectedObjectMeta(reflectorResource, namespace, apiVersion.ReflectorKindConfigMap),
			Data:       copyStringMap(source.Data),
			BinaryData: copyBytesMap(source.BinaryData),
		}
		return r.Create(ctx, &target)
	}
	if err != nil {
		return err
	}
	if !isManagedBy(&target.ObjectMeta, reflectorResource) {
		return fmt.Errorf("ConfigMap %s/%s already exists and is not managed by Reflector %s/%s", namespace, source.Name, reflectorResource.Namespace, reflectorResource.Name)
	}

	annotations := reflectedAnnotations(reflectorResource, apiVersion.ReflectorKindConfigMap)
	labels := reflectedLabels(reflectorResource)
	if reflect.DeepEqual(target.Data, source.Data) && bytesMapEqual(target.BinaryData, source.BinaryData) && reflect.DeepEqual(target.Annotations, annotations) && reflect.DeepEqual(target.Labels, labels) {
		return nil
	}
	target.Data = copyStringMap(source.Data)
	target.BinaryData = copyBytesMap(source.BinaryData)
	target.Annotations = annotations
	target.Labels = labels
	return r.Update(ctx, &target)
}

func (r *ReflectorReconciler) cleanupManagedObjects(ctx context.Context, reflectorResource *apiVersion.Reflector, desiredNamespaces map[string]struct{}, desiredKind string) error {
	selector := client.MatchingLabels{reflectorUIDLabel: string(reflectorResource.UID)}
	var cleanupErr error

	var secrets corev1.SecretList
	if err := r.List(ctx, &secrets, selector); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	} else {
		for i := range secrets.Items {
			secret := &secrets.Items[i]
			_, desired := desiredNamespaces[secret.Namespace]
			if desiredKind != apiVersion.ReflectorKindSecret || !desired {
				cleanupErr = errors.Join(cleanupErr, client.IgnoreNotFound(r.Delete(ctx, secret)))
			}
		}
	}

	var configMaps corev1.ConfigMapList
	if err := r.List(ctx, &configMaps, selector); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	} else {
		for i := range configMaps.Items {
			configMap := &configMaps.Items[i]
			_, desired := desiredNamespaces[configMap.Namespace]
			if desiredKind != apiVersion.ReflectorKindConfigMap || !desired {
				cleanupErr = errors.Join(cleanupErr, client.IgnoreNotFound(r.Delete(ctx, configMap)))
			}
		}
	}

	return cleanupErr
}

func (r *ReflectorReconciler) updateReflectorStatus(ctx context.Context, reflectorResource *apiVersion.Reflector, namespaces []string, conditionStatus metav1.ConditionStatus, reason, message string) error {
	oldStatus := reflectorResource.DeepCopy().Status
	reflectorResource.Status.ObservedGeneration = reflectorResource.Generation
	reflectorResource.Status.ReflectedNamespaces = append([]string(nil), namespaces...)
	reflectorResource.Status.ReflectedNamespaceCount = int32(len(namespaces))
	meta.SetStatusCondition(&reflectorResource.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             conditionStatus,
		ObservedGeneration: reflectorResource.Generation,
		Reason:             reason,
		Message:            message,
	})
	if reflect.DeepEqual(oldStatus, reflectorResource.Status) {
		return nil
	}
	return r.Status().Update(ctx, reflectorResource)
}

func (r *ReflectorReconciler) requestsForSecret(_ context.Context, object client.Object) []reconcile.Request {
	return requestsForObject(object, apiVersion.ReflectorKindSecret)
}

func (r *ReflectorReconciler) requestsForConfigMap(_ context.Context, object client.Object) []reconcile.Request {
	return requestsForObject(object, apiVersion.ReflectorKindConfigMap)
}

func requestsForObject(object client.Object, kind string) []reconcile.Request {
	annotations := object.GetAnnotations()
	if annotations == nil || annotations[sourceKindAnnotation] != kind {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: object.GetName(), Namespace: object.GetNamespace()}}}
	}
	reflectorName := annotations[reflectorNameAnnotation]
	reflectorNamespace := annotations[reflectorNamespaceAnnotation]
	if reflectorName == "" || reflectorNamespace == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: reflectorName, Namespace: reflectorNamespace}}}
}

func (r *ReflectorReconciler) requestsForNamespace(ctx context.Context, _ client.Object) []reconcile.Request {
	var reflectors apiVersion.ReflectorList
	if err := r.List(ctx, &reflectors); err != nil {
		r.Log.Error(err, "unable to list Reflectors after namespace change")
		return nil
	}
	requests := make([]reconcile.Request, 0, len(reflectors.Items))
	for i := range reflectors.Items {
		requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      reflectors.Items[i].Name,
			Namespace: reflectors.Items[i].Namespace,
		}})
	}
	return requests
}

func reflectedObjectMeta(reflectorResource *apiVersion.Reflector, namespace, kind string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:        reflectorResource.Name,
		Namespace:   namespace,
		Labels:      reflectedLabels(reflectorResource),
		Annotations: reflectedAnnotations(reflectorResource, kind),
	}
}

func reflectedLabels(reflectorResource *apiVersion.Reflector) map[string]string {
	return map[string]string{reflectorUIDLabel: string(reflectorResource.UID)}
}

func reflectedAnnotations(reflectorResource *apiVersion.Reflector, kind string) map[string]string {
	return map[string]string{
		sourceNamespaceAnnotation:    reflectorResource.Namespace,
		sourceNameAnnotation:         reflectorResource.Name,
		sourceKindAnnotation:         kind,
		reflectorNamespaceAnnotation: reflectorResource.Namespace,
		reflectorNameAnnotation:      reflectorResource.Name,
	}
}

func isManagedBy(objectMeta *metav1.ObjectMeta, reflectorResource *apiVersion.Reflector) bool {
	return objectMeta.Labels != nil && objectMeta.Labels[reflectorUIDLabel] == string(reflectorResource.UID)
}

func copyBytesMap(input map[string][]byte) map[string][]byte {
	if input == nil {
		return nil
	}
	output := make(map[string][]byte, len(input))
	for key, value := range input {
		output[key] = bytes.Clone(value)
	}
	return output
}

func bytesMapEqual(left, right map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftValue := range left {
		rightValue, ok := right[key]
		if !ok || !bytes.Equal(leftValue, rightValue) {
			return false
		}
	}
	return true
}

func copyStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func removeString(values []string, value string) []string {
	result := make([]string, 0, len(values))
	for _, item := range values {
		if item != value {
			result = append(result, item)
		}
	}
	return result
}
