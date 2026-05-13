package tpunodegroup

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	tpuapi "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NodeBootstrapper handles node bootstrapping logic.
type NodeBootstrapper struct {
	client client.Client
	igm    gce.IGMClient
}

// NewNodeBootstrapper creates a new NodeBootstrapper.
func NewNodeBootstrapper(client client.Client, igm gce.IGMClient) *NodeBootstrapper {
	return &NodeBootstrapper{
		client: client,
		igm:    igm,
	}
}

// ReconcileNodeJoin checks if nodes have joined the cluster and mutates NodeSummary in memory.
// Note: This helper defers persistence to the main Reconcile loop to prevent intermediate API
// patches from wiping out uncommitted status conditions set by earlier sub-reconcilers.
func (b *NodeBootstrapper) ReconcileNodeJoin(ctx context.Context, group *tpuapi.TPUNodeGroup) error {
	// 1. Get list of expected instances from MIG
	// Convention: migName = group.Name for now.
	// TODO(b/500810349): Get actual MIG name from status or child CR when available.
	migName := group.Name

	instances, err := b.igm.ListManagedInstances(ctx, group.Spec.Project, group.Spec.NodeLocation, migName)
	if err != nil {
		return fmt.Errorf("failed to list managed instances: %w", err)
	}

	// 2. List Node objects in the cluster
	var nodeList corev1.NodeList
	if err := b.client.List(ctx, &nodeList); err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	// 3. Match Nodes to expected instances
	nodeNames := make(map[string]bool)
	for _, inst := range instances {
		// inst.GetInstance() returns the full URL of the instance.
		// We extract the name (last part) to match against Node name.
		name := instanceShortName(inst.GetInstance())
		if name != "" {
			nodeNames[name] = true
		}
	}

	readyCount := 0

	for _, node := range nodeList.Items {
		// Match by name as requested
		if nodeNames[node.Name] {
			// Check if node is ready
			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
					readyCount++
					break
				}
			}
		}
	}

	// 4. Update TPUNodeGroup status
	if group.Status.NodeSummary == nil {
		group.Status.NodeSummary = &tpuapi.NodeSummary{}
	}
	group.Status.NodeSummary.Total = group.Spec.NodeCount
	group.Status.NodeSummary.Ready = int32(readyCount)
	// For now, reconciling means expected but not ready.
	group.Status.NodeSummary.Reconciling = group.Spec.NodeCount - int32(readyCount)

	// TODO(b/500810349): Use providerID for lookup in the future.

	return nil
}

// instanceShortName extracts the instance name from its full URL.
func instanceShortName(url string) string {
	if url == "" {
		return ""
	}
	parts := strings.Split(url, "/")
	return parts[len(parts)-1]
}

// generateBootstrapToken generates a random kubeadm bootstrap token and creates a K8s Secret for it.
func (b *NodeBootstrapper) generateBootstrapToken(ctx context.Context) (string, error) {
	tokenID := rand.String(6)
	tokenSecret := rand.String(16)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bootstrap-token-" + tokenID,
			Namespace: "kube-system",
		},
		Type: corev1.SecretType("bootstrap.kubernetes.io/token"),
		StringData: map[string]string{
			"token-id":                       tokenID,
			"token-secret":                   tokenSecret,
			"usage-bootstrap-authentication": "true",
			"usage-bootstrap-signing":        "true",
			"auth-extra-groups":              "system:bootstrappers:kubeadm:default-node-token",
			"expiration":                     time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		},
	}

	if err := b.client.Create(ctx, secret); err != nil {
		return "", fmt.Errorf("failed to create bootstrap token secret: %w", err)
	}

	return fmt.Sprintf("%s.%s", tokenID, tokenSecret), nil
}

// getCAHash reads the cluster's CA certificate and computes the SHA-256 hash of its public key.
func (b *NodeBootstrapper) getCAHash(ctx context.Context) (string, error) {
	var cm corev1.ConfigMap
	if err := b.client.Get(ctx, client.ObjectKey{Name: "kube-root-ca.crt", Namespace: "kube-system"}, &cm); err != nil {
		return "", fmt.Errorf("failed to get kube-root-ca.crt configmap: %w", err)
	}

	caCertPEM, ok := cm.Data["ca.crt"]
	if !ok {
		return "", errors.New("ca.crt not found in configmap")
	}

	block, _ := pem.Decode([]byte(caCertPEM))
	if block == nil {
		return "", errors.New("failed to decode PEM CA certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	hash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return fmt.Sprintf("sha256:%x", hash), nil
}
