package controllers

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	apiVersion "happyhelm.sh/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMatchingNamespacesUsesGlobPatternsAndSkipsSource(t *testing.T) {
	reconciler := newReflectorTestReconciler(t,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cert-manager"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "dev-one"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "prod-one"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "staging"}},
	)
	reflectorResource := testReflector()

	got, err := reconciler.matchingNamespaces(context.Background(), reflectorResource)
	if err != nil {
		t.Fatalf("matchingNamespaces returned an error: %v", err)
	}
	want := []string{"dev-one", "prod-one"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("matchingNamespaces = %v, want %v", got, want)
	}
}

func TestReflectSecretCreatesAndUpdatesManagedCopy(t *testing.T) {
	ctx := context.Background()
	reconciler := newReflectorTestReconciler(t)
	reflectorResource := testReflector()
	source := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: reflectorResource.Name, Namespace: reflectorResource.Namespace},
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{"tls.crt": []byte("first")},
	}

	if err := reconciler.reflectSecret(ctx, reflectorResource, source, "dev-one"); err != nil {
		t.Fatalf("reflectSecret create returned an error: %v", err)
	}

	key := client.ObjectKey{Name: source.Name, Namespace: "dev-one"}
	var created corev1.Secret
	if err := reconciler.Get(ctx, key, &created); err != nil {
		t.Fatalf("unable to get reflected Secret: %v", err)
	}
	if string(created.Data["tls.crt"]) != "first" || created.Type != corev1.SecretTypeTLS {
		t.Fatalf("reflected Secret did not preserve source data and type: %#v", created)
	}
	if created.Labels[reflectorUIDLabel] != string(reflectorResource.UID) {
		t.Fatalf("reflected Secret is missing its ownership label")
	}

	source.Data["tls.crt"] = []byte("second")
	if err := reconciler.reflectSecret(ctx, reflectorResource, source, "dev-one"); err != nil {
		t.Fatalf("reflectSecret update returned an error: %v", err)
	}
	if err := reconciler.Get(ctx, key, &created); err != nil {
		t.Fatalf("unable to get updated reflected Secret: %v", err)
	}
	if string(created.Data["tls.crt"]) != "second" {
		t.Fatalf("reflected Secret data = %q, want %q", created.Data["tls.crt"], "second")
	}
}

func TestReflectConfigMapDoesNotOverwriteUnmanagedObject(t *testing.T) {
	ctx := context.Background()
	reflectorResource := testReflector()
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: reflectorResource.Name, Namespace: "dev-one"},
		Data:       map[string]string{"owner": "user"},
	}
	reconciler := newReflectorTestReconciler(t, existing)
	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: reflectorResource.Name, Namespace: reflectorResource.Namespace},
		Data:       map[string]string{"owner": "reflector"},
	}

	if err := reconciler.reflectConfigMap(ctx, reflectorResource, source, "dev-one"); err == nil {
		t.Fatal("reflectConfigMap should reject an unmanaged target")
	}

	var unchanged corev1.ConfigMap
	if err := reconciler.Get(ctx, client.ObjectKeyFromObject(existing), &unchanged); err != nil {
		t.Fatalf("unable to get existing ConfigMap: %v", err)
	}
	if unchanged.Data["owner"] != "user" {
		t.Fatalf("unmanaged ConfigMap was overwritten: %#v", unchanged.Data)
	}
}

func TestCleanupDeletesOnlyStaleManagedCopies(t *testing.T) {
	ctx := context.Background()
	reflectorResource := testReflector()
	source := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: reflectorResource.Name, Namespace: reflectorResource.Namespace},
		Data:       map[string][]byte{"token": []byte("value")},
	}
	reconciler := newReflectorTestReconciler(t)
	for _, namespace := range []string{"dev-one", "prod-one"} {
		if err := reconciler.reflectSecret(ctx, reflectorResource, source, namespace); err != nil {
			t.Fatalf("unable to create reflected Secret in %s: %v", namespace, err)
		}
	}

	desired := map[string]struct{}{"prod-one": {}}
	if err := reconciler.cleanupManagedObjects(ctx, reflectorResource, desired, apiVersion.ReflectorKindSecret); err != nil {
		t.Fatalf("cleanupManagedObjects returned an error: %v", err)
	}

	var stale corev1.Secret
	err := reconciler.Get(ctx, types.NamespacedName{Name: source.Name, Namespace: "dev-one"}, &stale)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("stale Secret still exists or returned unexpected error: %v", err)
	}
	var retained corev1.Secret
	if err := reconciler.Get(ctx, types.NamespacedName{Name: source.Name, Namespace: "prod-one"}, &retained); err != nil {
		t.Fatalf("desired Secret was deleted: %v", err)
	}
}

func newReflectorTestReconciler(t *testing.T, objects ...client.Object) *ReflectorReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("unable to register core API: %v", err)
	}
	if err := apiVersion.ReflectorAddToScheme(scheme); err != nil {
		t.Fatalf("unable to register Reflector API: %v", err)
	}
	return &ReflectorReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(),
		Scheme: scheme,
		Log:    logr.Discard(),
	}
}

func testReflector() *apiVersion.Reflector {
	return &apiVersion.Reflector{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "star-example-com",
			Namespace: "cert-manager",
			UID:       types.UID("11111111-1111-1111-1111-111111111111"),
		},
		Spec: apiVersion.ReflectorSpec{
			Kind:      apiVersion.ReflectorKindSecret,
			ReflectTo: []string{"dev-*", "prod-*"},
		},
	}
}
