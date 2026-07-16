package v1alpha1

import (
	"encoding/json"
	"fmt"
)

const (
	CreatorUsernameAnnotation = "helm.k8s.ir/creator-username"
	CreatorGroupsAnnotation   = "helm.k8s.ir/creator-groups"
)

type CreatorIdentity struct {
	Username string
	Groups   []string
}

func IdentityFromAnnotations(annotations map[string]string) (CreatorIdentity, error) {
	if annotations == nil || annotations[CreatorUsernameAnnotation] == "" {
		return CreatorIdentity{}, fmt.Errorf("trusted creator identity is missing; recreate the DeployChart while the admission webhook is enabled")
	}

	identity := CreatorIdentity{Username: annotations[CreatorUsernameAnnotation]}
	encodedGroups := annotations[CreatorGroupsAnnotation]
	if encodedGroups == "" {
		return CreatorIdentity{}, fmt.Errorf("trusted creator groups are missing; recreate the DeployChart while the admission webhook is enabled")
	}
	if err := json.Unmarshal([]byte(encodedGroups), &identity.Groups); err != nil {
		return CreatorIdentity{}, fmt.Errorf("trusted creator groups are invalid: %w", err)
	}
	return identity, nil
}

func SetCreatorIdentity(annotations map[string]string, identity CreatorIdentity) (map[string]string, error) {
	if identity.Username == "" {
		return nil, fmt.Errorf("creator username cannot be empty")
	}
	groups, err := json.Marshal(identity.Groups)
	if err != nil {
		return nil, fmt.Errorf("encode creator groups: %w", err)
	}
	if annotations == nil {
		annotations = make(map[string]string, 2)
	}
	annotations[CreatorUsernameAnnotation] = identity.Username
	annotations[CreatorGroupsAnnotation] = string(groups)
	return annotations, nil
}
