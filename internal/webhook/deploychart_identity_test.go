package webhook

import (
	"context"
	"encoding/json"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	helmv1alpha1 "happyhelm.sh/api/helm/v1alpha1"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestCreateRecordsAuthenticatedUserAndOverwritesForgedIdentity(t *testing.T) {
	deploy := helmv1alpha1.DeployChart{}
	deploy.Annotations = map[string]string{helmv1alpha1.CreatorUsernameAnnotation: "cluster-admin"}
	raw := mustJSON(t, deploy)
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Create,
		Object:    runtime.RawExtension{Raw: raw},
		UserInfo: authenticationv1.UserInfo{
			Username: "john.anderson",
			Groups:   []string{"developers", "system:authenticated"},
		},
	}}

	mutated := handleAndApply(t, req)
	identity, err := helmv1alpha1.IdentityFromAnnotations(mutated.Annotations)
	if err != nil {
		t.Fatalf("identity annotation is invalid: %v", err)
	}
	if identity.Username != "john.anderson" {
		t.Fatalf("creator username = %q, want john.anderson", identity.Username)
	}
}

func TestUpdatePreservesOriginalCreator(t *testing.T) {
	oldDeploy := helmv1alpha1.DeployChart{}
	oldDeploy.Annotations, _ = helmv1alpha1.SetCreatorIdentity(nil, helmv1alpha1.CreatorIdentity{
		Username: "john.anderson",
		Groups:   []string{"developers"},
	})
	newDeploy := oldDeploy
	newDeploy.Annotations = map[string]string{
		helmv1alpha1.CreatorUsernameAnnotation: "cluster-admin",
		helmv1alpha1.CreatorGroupsAnnotation:   `["system:masters"]`,
	}
	raw := mustJSON(t, newDeploy)
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Update,
		Object:    runtime.RawExtension{Raw: raw},
		OldObject: runtime.RawExtension{Raw: mustJSON(t, oldDeploy)},
		UserInfo: authenticationv1.UserInfo{
			Username: "another.user",
			Groups:   []string{"system:masters"},
		},
	}}

	mutated := handleAndApply(t, req)
	identity, err := helmv1alpha1.IdentityFromAnnotations(mutated.Annotations)
	if err != nil {
		t.Fatalf("identity annotation is invalid: %v", err)
	}
	if identity.Username != "john.anderson" || len(identity.Groups) != 1 || identity.Groups[0] != "developers" {
		t.Fatalf("identity changed during update: %#v", identity)
	}
}

func handleAndApply(t *testing.T, req admission.Request) helmv1alpha1.DeployChart {
	t.Helper()
	response := (&DeployChartIdentityHandler{}).Handle(context.Background(), req)
	if !response.Allowed {
		t.Fatalf("request was denied: %#v", response.Result)
	}
	if err := response.Complete(req); err != nil {
		t.Fatalf("complete response: %v", err)
	}
	patch, err := jsonpatch.DecodePatch(response.Patch)
	if err != nil {
		t.Fatalf("decode response patch: %v", err)
	}
	mutatedRaw, err := patch.Apply(req.Object.Raw)
	if err != nil {
		t.Fatalf("apply response patch: %v", err)
	}
	var mutated helmv1alpha1.DeployChart
	if err := json.Unmarshal(mutatedRaw, &mutated); err != nil {
		t.Fatalf("decode mutated DeployChart: %v", err)
	}
	return mutated
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test value: %v", err)
	}
	return data
}
