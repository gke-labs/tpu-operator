#!/bin/bash
set -eo pipefail

# =========================================================
# FUNCTIONS
# =========================================================

# Wait for background apt/dpkg processes to release locks
wait_for_apt() {
  echo "Waiting for apt processes to finish..."
  while pgrep -f "apt-get|dpkg" >/dev/null 2>&1 ; do
    sleep 5
  done
}

# Retrieve GCE metadata with retries
get_metadata() {
  local attr=$1
  local result=""
  local max_retries=30
  local count=0

  while [ -z "$result" ] && [ "$count" -lt "$max_retries" ]; do
    sleep 5
    result=$(curl -fs -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/attributes/$attr || true)
    count=$((count+1))
  done
  echo "$result"
}

# =========================================================
# 1. SYSTEM PREPARATION
# =========================================================

# Disable Swap
sudo swapoff -a
sudo sed -i '/ swap / s/^\(.*\)$/#\1/g' /etc/fstab

# Kernel Modules & Networking
cat <<'EOF' | sudo tee /etc/modules-load.d/k8s.conf >/dev/null
overlay
br_netfilter
EOF
sudo modprobe overlay
sudo modprobe br_netfilter
cat <<'EOF' | sudo tee /etc/sysctl.d/k8s.conf >/dev/null
net.bridge.bridge-nf-call-iptables  = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward                 = 1
EOF
sudo sysctl --system >/dev/null

# 3. Configure and Install Containerd
VERSION="{{VERSION}}"
MINOR_VERSION=$(echo "$VERSION" | cut -d. -f2)
export DEBIAN_FRONTEND=noninteractive

# =========================================================
# 2. PACKAGE REPOSITORIES & UPDATES
# =========================================================

# Configure containerd for CRI
sudo mkdir -p /etc/containerd
containerd config default | sudo tee /etc/containerd/config.toml >/dev/null
sudo sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml
sudo systemctl restart containerd

wait_for_apt

sudo apt-get update && sudo apt-get install -y containerd

# 4. Install K8s prerequisites and packages
wait_for_apt

sudo apt-get install -y conntrack apt-transport-https curl
sudo mkdir -p /etc/apt/keyrings
# Delete existing Kubernetes keyring file if it exists.
# On GCE VM reboots, the startup script re-runs. Because it is set -eo pipefail, the dearmor
# command will fail non-interactively if the destination file already exists.
sudo rm -f /etc/apt/keyrings/kubernetes-apt-keyring.gpg
curl -fsSL https://pkgs.k8s.io/core:/stable:/v${VERSION}/deb/Release.key | sudo gpg --batch --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
echo 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v${VERSION}/deb/ /' | sudo tee /etc/apt/sources.list.d/kubernetes.list

wait_for_apt

# Mask kubelet to prevent it from starting during installation
sudo systemctl mask kubelet

sudo apt-get update && sudo apt-get install -y kubelet kubeadm kubectl
sudo apt-mark hold kubelet kubeadm kubectl

# =========================================================
# 4. CRI & KUBELET CONFIGURATION
# =========================================================

# Mask kubelet during configuration
sudo mkdir -p /sys/fs/cgroup/kubelet.slice

# =========================================================
# 5. METADATA & KUBEADM CONFIG
# =========================================================


echo "Retrieving cluster join metadata..."
TOKEN=$(get_metadata kubeadm-join-token)
CP_IP=$(get_metadata kubeadm-control-plane-ip)
CA_HASH=$(get_metadata kubeadm-ca-hash)

if [ -z "$TOKEN" ] || [ -z "$CP_IP" ] || [ -z "$CA_HASH" ]; then
  echo "Error: Failed to retrieve required metadata for cluster join." >&2
  exit 1
fi

PROJECT="{{PROJECT}}"
ZONE="{{ZONE}}"
NAME=$(curl -fs -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/name || true)

if [ -z "$PROJECT" ] || [ -z "$ZONE" ] || [ -z "$NAME" ]; then
  echo "Error: Failed to retrieve standard instance metadata." >&2
  exit 1
fi

PROVIDER_ID="gce://$PROJECT/$ZONE/$NAME"
echo "Constructed ProviderID: $PROVIDER_ID"

NEEDS_V1BETA2_API="false"
NEEDS_EXPLICIT_CRI="false"
if [ "${MINOR_VERSION:-0}" -le 22 ]; then
  NEEDS_V1BETA2_API="true"
fi
if [ "${MINOR_VERSION:-0}" -le 25 ]; then
  NEEDS_EXPLICIT_CRI="true"
fi

sudo mkdir -p /etc/kubernetes
KUBEADM_API_VERSION="v1beta3"
if [ "$NEEDS_V1BETA2_API" = "true" ]; then
  KUBEADM_API_VERSION="v1beta2"
fi

CRI_SOCKET_YAML=""
if [ "$NEEDS_EXPLICIT_CRI" = "true" ]; then
  CRI_SOCKET_YAML="  criSocket: \"unix:///var/run/containerd/containerd.sock\""
fi

# Generate Join Configuration
sudo mkdir -p /etc/kubernetes
cat <<EOF | sudo tee /etc/kubernetes/kubeadm-join.yaml >/dev/null
apiVersion: kubeadm.k8s.io/${KUBEADM_API_VERSION}
kind: JoinConfiguration
discovery:
  bootstrapToken:
    token: "$TOKEN"
    apiServerEndpoint: "$CP_IP:6443"
    caCertHashes:
    - "$CA_HASH"
nodeRegistration:
${CRI_SOCKET_YAML}
  kubeletExtraArgs:
    cgroup-driver: "systemd"
    provider-id: "$PROVIDER_ID"
EOF

echo "Unmasking and enabling kubelet..."
sudo systemctl unmask kubelet
sudo systemctl enable --now kubelet

MAX_RETRIES=2
attempt=0
backoff=15

while true; do
  attempt=$((attempt + 1))
  echo "Attempt $attempt of $MAX_RETRIES to join cluster..."

  if sudo kubeadm join --config /etc/kubernetes/kubeadm-join.yaml; then
    echo "Successfully joined the cluster!"
    break
  fi

  echo "Join attempt $attempt failed."

  if [ "$attempt" -ge "$MAX_RETRIES" ]; then
    echo "Error: Exhausted all join retries. Failing." >&2
    exit 1
  fi

  echo "Resetting kubeadm state before retry..."
  if [ "$NEEDS_EXPLICIT_CRI" = "true" ]; then
    sudo kubeadm reset --force --cri-socket "unix:///var/run/containerd/containerd.sock"
  else
    sudo kubeadm reset --force
  fi

  echo "Sleeping for ${backoff}s before next attempt..."
  sleep "$backoff"
  backoff=$((backoff * 2))
  [ "$backoff" -gt 120 ] && backoff=120
done
