package tpunodegroup

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/compute/apiv1/computepb"
	tpuapi "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	kubeadmJoinTokenKey      = "kubeadm-join-token"
	kubeadmControlPlaneIPKey = "kubeadm-control-plane-ip"
	kubeadmCAHashKey         = "kubeadm-ca-hash"
	tpuAcceleratorKey        = "cloud.google.com/gke-tpu-accelerator"
	tpuAcceleratorCountKey   = "cloud.google.com/gke-accelerator-count"
	tpuTopologyKey           = "cloud.google.com/gke-tpu-topology"
	kubeLabelsKey            = "kube-labels"
)

// injectMetadata handles injecting metadata into instances.
func injectMetadata(ctx context.Context, group *tpuapi.TPUNodeGroup, k8sClient client.Client, igm gce.IGMClient, instanceClient gce.InstanceClient) error {
	migName := group.ManagedInstanceGroupName()
	instances, err := igm.ListManagedInstances(ctx, group.Spec.Project, group.Spec.NodeLocation, migName)
	if err != nil {
		return fmt.Errorf("failed to list managed instances: %w", err)
	}

	var cpIP string
	var caHash string
	var token string // Generate once and reuse!
	if group.Spec.BootstrapKubernetes != nil {
		cpIP = group.Spec.BootstrapKubernetes.ControlPlaneIP
		var err error
		caHash, err = fetchCAHash(ctx, k8sClient)
		if err != nil {
			return fmt.Errorf("failed to get CA hash: %w", err)
		}
	}

	var errs []error
	for _, mi := range instances {
		name := instanceShortName(mi.GetInstance())
		if name == "" {
			continue
		}

		req := &computepb.GetInstanceRequest{
			Project:  group.Spec.Project,
			Zone:     group.Spec.NodeLocation,
			Instance: name,
		}
		gceInst, err := instanceClient.Get(ctx, req)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to get instance %s: %w", name, err))
			continue
		}

		tokenUpdates := make(map[string]string)
		if group.Spec.BootstrapKubernetes != nil && gceInst.Status != nil && *gceInst.Status == "RUNNING" {
			hasToken := false
			if gceInst.Metadata != nil {
				for _, item := range gceInst.Metadata.Items {
					if item.Key != nil && *item.Key == kubeadmJoinTokenKey {
						hasToken = true
						break
					}
				}
			}

			if !hasToken {
				if token == "" {
					var err error
					token, err = generateBootstrapToken(ctx, k8sClient)
					if err != nil {
						errs = append(errs, fmt.Errorf("failed to generate bootstrap token: %w", err))
						continue
					}
				}
				tokenUpdates[kubeadmJoinTokenKey] = token
				tokenUpdates[kubeadmControlPlaneIPKey] = cpIP
				tokenUpdates[kubeadmCAHashKey] = caHash
			}
		}

		sliceUpdates := sliceMetadata(group, gceInst)

		newItems, fingerprint, changed := mergeMetadataItems(gceInst, tokenUpdates, sliceUpdates)
		if !changed {
			continue
		}

		setReq := &computepb.SetMetadataInstanceRequest{
			Project:  group.Spec.Project,
			Zone:     group.Spec.NodeLocation,
			Instance: name,
			MetadataResource: &computepb.Metadata{
				Fingerprint: fingerprint,
				Items:       newItems,
			},
		}

		_, err = instanceClient.SetMetadata(ctx, setReq)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to set metadata for instance %s: %w", name, err))
			continue
		}
	}

	if agg := utilerrors.NewAggregate(errs); agg != nil {
		return agg
	}
	return nil
}

// sliceMetadata returns metadata for slice topology etc.
func sliceMetadata(group *tpuapi.TPUNodeGroup, gceInst *computepb.Instance) map[string]string {
	updates := make(map[string]string)

	// 1. Map the raw GCE machine type to the specific accelerator string expected by the plugin
	acceleratorType := acceleratorLabelValue(group)
	if acceleratorType == "" {
		// Cannot inject labels without a known accelerator type
		return updates
	}

	// 2. Extract the number of chips per VM from the machine type
	chipsPerNode := chipsPerNode(group)
	if chipsPerNode == 0 {
		// The device plugin will crash if count is missing, fail safely here
		return updates
	}

	// 3. Safely extract any existing kube-labels from the GCE Instance so we don't overwrite them
	var existingLabels string
	if gceInst != nil && gceInst.Metadata != nil {
		for _, item := range gceInst.Metadata.Items {
			if item.Key != nil && *item.Key == kubeLabelsKey && item.Value != nil {
				existingLabels = *item.Value
				break
			}
		}
	}

	// 4. Merge the existing labels with our new TPU labels (comma-separated format)
	labelMap := make(map[string]string)
	if existingLabels != "" {
		parts := strings.Split(existingLabels, ",")
		for _, part := range parts {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				labelMap[kv[0]] = kv[1]
			} else if len(kv) == 1 && kv[0] != "" {
				labelMap[kv[0]] = ""
			}
		}
	}

	// Overwrite with new labels
	labelMap[tpuAcceleratorKey] = acceleratorType
	labelMap[tpuAcceleratorCountKey] = strconv.Itoa(chipsPerNode)
	if group.Spec.Topology != "" {
		labelMap[tpuTopologyKey] = group.Spec.Topology
	} else {
		delete(labelMap, tpuTopologyKey)
	}

	// Reconstruct finalLabels in deterministic order
	keys := make([]string, 0, len(labelMap))
	for k := range labelMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var labelStrings []string
	for _, k := range keys {
		labelStrings = append(labelStrings, fmt.Sprintf("%s=%s", k, labelMap[k]))
	}
	finalLabels := strings.Join(labelStrings, ",")

	// 6. Return the updates. The controller will patch this into the GCE VM's metadata.
	updates[kubeLabelsKey] = finalLabels

	return updates
}

// generateBootstrapToken generates a random kubeadm bootstrap token and creates a K8s Secret for it.
func generateBootstrapToken(ctx context.Context, k8sClient client.Client) (string, error) {
	tokenID := strings.ToLower(rand.String(6))
	tokenSecret := strings.ToLower(rand.String(16))

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

	if err := k8sClient.Create(ctx, secret); err != nil {
		return "", fmt.Errorf("failed to create bootstrap token secret: %w", err)
	}

	return fmt.Sprintf("%s.%s", tokenID, tokenSecret), nil
}

// fetchCAHash reads the cluster's CA certificate and computes the SHA-256 hash of its public key.
func fetchCAHash(ctx context.Context, k8sClient client.Client) (string, error) {
	var cm corev1.ConfigMap
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: "kube-root-ca.crt", Namespace: "kube-system"}, &cm); err != nil {
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

// mergeMetadataItems merges existing metadata items with updates, overwriting existing keys if present.
func mergeMetadataItems(gceInst *computepb.Instance, tokenUpdates, sliceUpdates map[string]string) ([]*computepb.Items, *string, bool) {
	updates := make(map[string]string)
	for k, v := range tokenUpdates {
		updates[k] = v
	}
	for k, v := range sliceUpdates {
		updates[k] = v
	}

	if len(updates) == 0 {
		return nil, nil, false
	}

	var existingItems []*computepb.Items
	var fingerprint *string
	if gceInst.Metadata != nil {
		fingerprint = gceInst.Metadata.Fingerprint
		existingItems = gceInst.Metadata.Items
	}

	merged := make([]*computepb.Items, 0, len(existingItems)+len(updates))
	seenUpdates := make(map[string]bool)

	for _, item := range existingItems {
		if item.Key == nil {
			continue
		}
		key := *item.Key
		if val, ok := updates[key]; ok {
			merged = append(merged, &computepb.Items{Key: proto.String(key), Value: proto.String(val)})
			seenUpdates[key] = true
		} else {
			merged = append(merged, item)
		}
	}

	for key, val := range updates {
		if !seenUpdates[key] {
			merged = append(merged, &computepb.Items{Key: proto.String(key), Value: proto.String(val)})
		}
	}

	return merged, fingerprint, true
}

