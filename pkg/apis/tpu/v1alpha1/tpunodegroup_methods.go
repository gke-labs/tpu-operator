package v1alpha1

// InstanceTemplateName returns the name of the child InstanceTemplate.
func (t *TPUNodeGroup) InstanceTemplateName() string {
	return t.Name + "-template"
}

// WorkloadPolicyName returns the name of the child WorkloadPolicy.
func (t *TPUNodeGroup) WorkloadPolicyName() string {
	return t.Name + "-policy"
}

// ManagedInstanceGroupName returns the name of the child ManagedInstanceGroup.
func (t *TPUNodeGroup) ManagedInstanceGroupName() string {
	return t.Name + "-mig"
}
