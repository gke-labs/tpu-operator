package v1alpha1

import (
	"crypto/sha256"
	"encoding/hex"
)

func (t *TPUNodeGroup) hashSuffix() string {
	if t.UID == "" {
		return ""
	}
	hashBytes := sha256.Sum256([]byte(t.UID))
	hashHex := hex.EncodeToString(hashBytes[:])
	return "-" + hashHex[:8]
}

// InstanceTemplateName returns the name of the child InstanceTemplate.
func (t *TPUNodeGroup) InstanceTemplateName() string {
	return t.Name + "-template" + t.hashSuffix()
}

// WorkloadPolicyName returns the name of the child WorkloadPolicy.
func (t *TPUNodeGroup) WorkloadPolicyName() string {
	return t.Name + "-policy" + t.hashSuffix()
}

// ManagedInstanceGroupName returns the name of the child ManagedInstanceGroup.
func (t *TPUNodeGroup) ManagedInstanceGroupName() string {
	return t.Name + "-mig" + t.hashSuffix()
}
