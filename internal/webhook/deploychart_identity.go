package webhook

import (
	"context"
	"encoding/json"
	"fmt"

	helmv1alpha1 "happyhelm.sh/api/helm/v1alpha1"
	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const DeployChartIdentityPath = "/mutate-helm-k8s-ir-v1alpha1-deploychart-identity"

type DeployChartIdentityHandler struct{}

func (h *DeployChartIdentityHandler) Handle(_ context.Context, req admission.Request) admission.Response {
	var deploy helmv1alpha1.DeployChart
	if err := json.Unmarshal(req.Object.Raw, &deploy); err != nil {
		return admission.Errored(400, fmt.Errorf("decode DeployChart: %w", err))
	}

	switch req.Operation {
	case admissionv1.Create:
		annotations, err := helmv1alpha1.SetCreatorIdentity(deploy.Annotations, helmv1alpha1.CreatorIdentity{
			Username: req.UserInfo.Username,
			Groups:   append([]string(nil), req.UserInfo.Groups...),
		})
		if err != nil {
			return admission.Denied(err.Error())
		}
		deploy.Annotations = annotations
	case admissionv1.Update:
		var oldDeploy helmv1alpha1.DeployChart
		if err := json.Unmarshal(req.OldObject.Raw, &oldDeploy); err != nil {
			return admission.Errored(400, fmt.Errorf("decode previous DeployChart: %w", err))
		}
		identity, err := helmv1alpha1.IdentityFromAnnotations(oldDeploy.Annotations)
		if err != nil {
			return admission.Denied(err.Error())
		}
		deploy.Annotations, err = helmv1alpha1.SetCreatorIdentity(deploy.Annotations, identity)
		if err != nil {
			return admission.Denied(err.Error())
		}
	default:
		return admission.Allowed("operation does not require creator identity mutation")
	}

	marshaled, err := json.Marshal(&deploy)
	if err != nil {
		return admission.Errored(500, fmt.Errorf("encode DeployChart: %w", err))
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaled)
}

var _ admission.Handler = &DeployChartIdentityHandler{}
