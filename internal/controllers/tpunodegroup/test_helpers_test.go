package tpunodegroup

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// withTPU adds TPU resources to a Node's capacity and allocatable status.
func withTPU(node corev1.Node, capacity, allocatable string) corev1.Node {
	if node.Status.Capacity == nil {
		node.Status.Capacity = corev1.ResourceList{}
	}
	if node.Status.Allocatable == nil {
		node.Status.Allocatable = corev1.ResourceList{}
	}
	if capacity != "" {
		node.Status.Capacity[resourceNameTPU] = resource.MustParse(capacity)
	}
	if allocatable != "" {
		node.Status.Allocatable[resourceNameTPU] = resource.MustParse(allocatable)
	}
	return node
}
