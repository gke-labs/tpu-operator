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
  local attr="$1"
  local result=""
  local max_retries=30
  local count=0

  while [ -z "$result" ] && [ "$count" -lt "$max_retries" ]; do
    sleep 5
    result=$(curl -fs -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/attributes/$attr" || true)
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

VERSION="{{VERSION}}"
MINOR_VERSION=$(echo "$VERSION" | cut -d. -f2)
export DEBIAN_FRONTEND=noninteractive

# Establish feature flags based on MINOR_VERSION
NEEDS_MODERN_REPO="false"
if [ "${MINOR_VERSION:-0}" -ge 28 ]; then
  NEEDS_MODERN_REPO="true"
fi

NEEDS_LEGACY_INSTALL="false"
if [ "${MINOR_VERSION:-0}" -le 27 ]; then
  NEEDS_LEGACY_INSTALL="true"
fi

NEEDS_EXPLICIT_CRI="false"
if [ "${MINOR_VERSION:-0}" -le 25 ]; then
  NEEDS_EXPLICIT_CRI="true"
fi

NEEDS_V1BETA2_API="false"
if [ "${MINOR_VERSION:-0}" -le 22 ]; then
  NEEDS_V1BETA2_API="true"
fi

# =========================================================
# 2. PACKAGE REPOSITORIES & UPDATES
# =========================================================

wait_for_apt

# If using modern K8s (>= 1.28), configure the package repository FIRST
# so we only have to run `apt-get update` once.
if [ "$NEEDS_MODERN_REPO" = "true" ]; then
  sudo apt-get install -y conntrack apt-transport-https curl gnupg
  sudo mkdir -p /etc/apt/keyrings

  sudo rm -f /etc/apt/keyrings/kubernetes-apt-keyring.gpg
  curl -fsSL "https://pkgs.k8s.io/core:/stable:/v${VERSION}/deb/Release.key" | sudo gpg --batch --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
  echo "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v${VERSION}/deb/ /" | sudo tee /etc/apt/sources.list.d/kubernetes.list >/dev/null
fi

wait_for_apt
sudo apt-get update

# Mask kubelet to prevent it from starting during installation
sudo systemctl mask kubelet

# =========================================================
# 3. INSTALLATION PHASE (Grouped by Version)
# =========================================================

if [ "$NEEDS_LEGACY_INSTALL" = "true" ]; then
  # --- LEGACY INSTALLATION (<= 1.27) ---

  # Install Containerd
  if [ "$NEEDS_EXPLICIT_CRI" = "true" ]; then
    # Support CRI v1alpha2 for K8s <= 1.25
    CONTAINERD_VERSION=$(apt-cache madison containerd | awk '{print $3}' | grep -E "^1\." | head -n 1 || true)
    if [ -n "$CONTAINERD_VERSION" ]; then
      echo "Installing containerd version $CONTAINERD_VERSION to support CRI v1alpha2"
      sudo apt-get install -y --allow-downgrades containerd="$CONTAINERD_VERSION"
    else
      sudo apt-get install -y containerd
    fi
  else
    sudo apt-get install -y containerd
  fi

  # Install prerequisites
  sudo apt-get install -y conntrack socat iptables apt-transport-https curl

  # Prepare directories
  sudo mkdir -p /opt/cni/bin /usr/bin /etc/systemd/system/kubelet.service.d

  # Download K8s binaries directly via curl
  echo "Version $VERSION uses direct binary downloads..."
  K8S_VERSION=$(curl -fsSL "https://dl.k8s.io/release/stable-${VERSION}.txt")
  CNI_VERSION="v1.1.1"
  RELEASE_VERSION="v0.16.2"

  echo "Installing CNI plugins ${CNI_VERSION}..."
  curl -fsSLo cni-plugins.tgz "https://github.com/containernetworking/plugins/releases/download/${CNI_VERSION}/cni-plugins-linux-amd64-${CNI_VERSION}.tgz"
  sudo tar -C /opt/cni/bin -xzf cni-plugins.tgz
  rm cni-plugins.tgz

  echo "Downloading K8s binaries ${K8S_VERSION}..."
  for bin in kubeadm kubelet kubectl; do
    sudo curl -fsSLo "/usr/bin/$bin" "https://dl.k8s.io/release/${K8S_VERSION}/bin/linux/amd64/$bin"
    sudo chmod +x "/usr/bin/$bin"
  done

  echo "Downloading crictl..."
  CRICTL_VERSION="v${VERSION}.0"
  curl -fsSLo crictl.tar.gz "https://github.com/kubernetes-sigs/cri-tools/releases/download/${CRICTL_VERSION}/crictl-${CRICTL_VERSION}-linux-amd64.tar.gz"
  sudo tar -C /usr/bin -xzf crictl.tar.gz
  rm crictl.tar.gz

  echo "Configuring systemd for kubelet..."
  sudo curl -fsSLo /lib/systemd/system/kubelet.service "https://raw.githubusercontent.com/kubernetes/release/${RELEASE_VERSION}/cmd/krel/templates/latest/kubelet/kubelet.service"
  sudo curl -fsSLo /etc/systemd/system/kubelet.service.d/10-kubeadm.conf "https://raw.githubusercontent.com/kubernetes/release/${RELEASE_VERSION}/cmd/krel/templates/latest/kubeadm/10-kubeadm.conf"
  sudo systemctl daemon-reload

else
  # --- MODERN INSTALLATION (>= 1.28) ---
  wait_for_apt
  sudo apt-get install -y containerd kubelet kubeadm kubectl
  sudo apt-mark hold kubelet kubeadm kubectl
fi

# =========================================================
# 4. CRI & KUBELET CONFIGURATION
# =========================================================

# Configure containerd for CRI
sudo mkdir -p /etc/containerd
containerd config default | sudo tee /etc/containerd/config.toml >/dev/null
sudo sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml
sudo systemctl restart containerd

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

# =========================================================
# 6. CLUSTER JOIN (WITH RETRIES)
# =========================================================

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
