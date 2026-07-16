package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	ReflectorKindSecret    = "Secret"
	ReflectorKindConfigMap = "ConfigMap"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Kind",type=string,JSONPath=`.spec.kind`
// +kubebuilder:printcolumn:name="Reflected",type=integer,JSONPath=`.status.reflectedNamespaceCount`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Reflector struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ReflectorSpec   `json:"spec,omitempty"`
	Status ReflectorStatus `json:"status,omitempty"`
}

type ReflectorSpec struct {
	// Kind is the source resource kind. The source has the same name and
	// namespace as this Reflector.
	// +kubebuilder:validation:Enum=Secret;ConfigMap
	Kind string `json:"kind"`

	// ReflectTo contains shell-style namespace patterns such as "dev-*".
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	ReflectTo []string `json:"reflectTo"`
}

type ReflectorStatus struct {
	ObservedGeneration      int64              `json:"observedGeneration,omitempty"`
	ReflectedNamespaceCount int32              `json:"reflectedNamespaceCount,omitempty"`
	ReflectedNamespaces     []string           `json:"reflectedNamespaces,omitempty"`
	Conditions              []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type ReflectorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Reflector `json:"items"`
}

var ReflectorGroupVersion = schema.GroupVersion{Group: "others.helm.k8s.ir", Version: "v1alpha1"}

func ReflectorAddToScheme(s *runtime.Scheme) error {
	s.AddKnownTypes(ReflectorGroupVersion, &Reflector{}, &ReflectorList{})
	metav1.AddToGroupVersion(s, ReflectorGroupVersion)
	return nil
}

func (in *Reflector) DeepCopyInto(out *Reflector) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = ReflectorSpec{
		Kind:      in.Spec.Kind,
		ReflectTo: append([]string(nil), in.Spec.ReflectTo...),
	}
	out.Status = ReflectorStatus{
		ObservedGeneration:      in.Status.ObservedGeneration,
		ReflectedNamespaceCount: in.Status.ReflectedNamespaceCount,
		ReflectedNamespaces:     append([]string(nil), in.Status.ReflectedNamespaces...),
		Conditions:              append([]metav1.Condition(nil), in.Status.Conditions...),
	}
}

func (in *Reflector) DeepCopy() *Reflector {
	if in == nil {
		return nil
	}
	out := new(Reflector)
	in.DeepCopyInto(out)
	return out
}

func (in *Reflector) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *ReflectorList) DeepCopyInto(out *ReflectorList) {
	*out = *in
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]Reflector, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *ReflectorList) DeepCopy() *ReflectorList {
	if in == nil {
		return nil
	}
	out := new(ReflectorList)
	in.DeepCopyInto(out)
	return out
}

func (in *ReflectorList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}
