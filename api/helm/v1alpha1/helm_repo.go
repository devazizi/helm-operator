package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type Repository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HelmRepoSpec   `json:"spec,omitempty"`
	Status HelmRepoStatus `json:"status,omitempty"`
}

type HelmRepoSpec struct {
	URL            string `json:"url"`
	HasCredentials bool   `json:"hasCredentials,omitempty"`
	Username       string `json:"username,omitempty"`
	Password       string `json:"password,omitempty"`
}

type HelmRepoStatus struct {
	Processed          bool  `json:"processed"`
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
type RepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Repository `json:"items"`
}

func (in *Repository) DeepCopyInto(out *Repository) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
}

func (in *Repository) DeepCopy() *Repository {
	if in == nil {
		return nil
	}
	out := new(Repository)
	in.DeepCopyInto(out)
	return out
}

func (in *Repository) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *RepositoryList) DeepCopyInto(out *RepositoryList) {
	*out = *in
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]Repository, len(in.Items))
		copy(out.Items, in.Items)
	}
}

func (in *RepositoryList) DeepCopy() *RepositoryList {
	if in == nil {
		return nil
	}
	out := new(RepositoryList)
	in.DeepCopyInto(out)
	return out
}

func (in *RepositoryList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

var HelmRepoGroupVersion = schema.GroupVersion{Group: "helm.k8s.ir", Version: "v1alpha1"}

func HelmRepoAddToScheme(s *runtime.Scheme) error {
	s.AddKnownTypes(HelmRepoGroupVersion, &Repository{}, &RepositoryList{})
	metav1.AddToGroupVersion(s, HelmRepoGroupVersion)
	return nil
}
