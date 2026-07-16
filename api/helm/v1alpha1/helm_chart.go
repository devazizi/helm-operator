package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type DeployChart struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeployChartSpec   `json:"spec,omitempty"`
	Status DeployChartStatus `json:"status,omitempty"`
}

type DeployChartSpec struct {
	Chart  ChartSpec              `json:"chart"`
	Values map[string]interface{} `json:"values,omitempty"` // preserve unknown fields
}

type ChartSpec struct {
	Repo    string `json:"repo"`
	Chart   string `json:"chart"`
	Version string `json:"version"`
}

type DeployChartStatus struct {
	Processed       bool   `json:"processed,omitempty"`
	State           string `json:"state,omitempty"`           // Pending, Deploying, Succeeded, Failed
	Message         string `json:"message,omitempty"`         // optional message
	LastAppliedHash string `json:"lastAppliedHash,omitempty"` // hash of the last applied spec
	ExecutedAs      string `json:"executedAs,omitempty"`      // authenticated creator used for Helm impersonation
}

// +kubebuilder:object:root=true
type DeployChartList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DeployChart `json:"items"`
}

func (in *DeployChart) DeepCopyInto(out *DeployChart) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	if in.Spec.Values != nil {
		out.Spec.Values = runtime.DeepCopyJSON(in.Spec.Values)
	}
}

func (in *DeployChart) DeepCopy() *DeployChart {
	if in == nil {
		return nil
	}
	out := new(DeployChart)
	in.DeepCopyInto(out)
	return out
}

func (in *DeployChart) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *DeployChartList) DeepCopyInto(out *DeployChartList) {
	*out = *in
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]DeployChart, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *DeployChartList) DeepCopy() *DeployChartList {
	if in == nil {
		return nil
	}
	out := new(DeployChartList)
	in.DeepCopyInto(out)
	return out
}

func (in *DeployChartList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

var GroupVersion = schema.GroupVersion{Group: "helm.k8s.ir", Version: "v1alpha1"}

func DeployAddToScheme(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &DeployChart{}, &DeployChartList{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
}
