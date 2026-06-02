#!/bin/bash
set -e

# 1. Disable Swap
sudo swapoff -a
sudo sed -i '/ swap / s/^\(.*\)$/#\1/g' /etc/fstab

# 2. Kernel Modules & Networking
cat <<EOF2 | sudo tee /etc/modules-load.d/k8s.conf
overlay
br_netfilter
EOF2
sudo modprobe overlay
sudo modprobe br_netfilter
cat <<EOF2 | sudo tee /etc/sysctl.d/k8s.conf
net.bridge.bridge-nf-call-iptables  = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward                 = 1
EOF2
sudo sysctl --system

# 3. Configure and Install Containerd
export DEBIAN_FRONTEND=noninteractive

sudo mkdir -p /etc/containerd
cat <<EOF2 | sudo tee /etc/containerd/config.toml
version = 2
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc.options]
  SystemdCgroup = true
EOF2

echo "Waiting for other apt processes to finish..."
while pgrep -f "apt-get|dpkg" >/dev/null 2>&1 ; do
  sleep 5
done

sudo apt-get update && sudo apt-get install -y containerd

# 4. Install K8s prerequisites and packages
echo "Waiting for other apt processes to finish..."
while pgrep -f "apt-get|dpkg" >/dev/null 2>&1 ; do
  sleep 5
done

sudo apt-get install -y conntrack apt-transport-https curl
sudo mkdir -p /etc/apt/keyrings
curl -fsSL https://pkgs.k8s.io/core:/stable:/v{{VERSION}}/deb/Release.key | sudo gpg --batch --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
echo 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v{{VERSION}}/deb/ /' | sudo tee /etc/apt/sources.list.d/kubernetes.list

echo "Waiting for other apt processes to finish..."
while pgrep -f "apt-get|dpkg" >/dev/null 2>&1 ; do
  sleep 5
done

# Mask kubelet to prevent it from starting during installation
sudo systemctl mask kubelet

sudo apt-get update && sudo apt-get install -y kubelet kubeadm kubectl
sudo apt-mark hold kubelet kubeadm kubectl

# Additional fixes for manual testing via tunnel
sudo mkdir -p /sys/fs/cgroup/kubelet.slice

# Helper function to retrieve metadata with retries
get_metadata() {
  local attr=$1
  local result=""
  local max_retries=30
  local count=0
  while [ -z "$result" ] && [ $count -lt $max_retries ]; do
    sleep 5
    result=$(curl -f -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/attributes/$attr || true)
    count=$((count+1))
  done
  echo "$result"
}

echo "Retrieving metadata..."
TOKEN=$(get_metadata kubeadm-join-token)
CP_IP=$(get_metadata kubeadm-control-plane-ip)
CA_HASH=$(get_metadata kubeadm-ca-hash)

if [ -z "$TOKEN" ] || [ -z "$CP_IP" ] || [ -z "$CA_HASH" ]; then
  echo "Failed to retrieve required metadata for cluster join."
  exit 1
fi

echo "Using templated project and zone for providerID..."
PROJECT="{{PROJECT}}"
ZONE="{{ZONE}}"
NAME=$(curl -f -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/name || true)

if [ -z "$PROJECT" ] || [ -z "$ZONE" ] || [ -z "$NAME" ]; then
  echo "Failed to retrieve standard instance metadata."
  exit 1
fi

PROVIDER_ID="gce://$PROJECT/$ZONE/$NAME"
echo "Constructed ProviderID: $PROVIDER_ID"

sudo mkdir -p /etc/kubernetes
cat <<EOF2 | sudo tee /etc/kubernetes/kubeadm-join.yaml
apiVersion: kubeadm.k8s.io/v1beta3
kind: JoinConfiguration
discovery:
  bootstrapToken:
    token: "$TOKEN"
    apiServerEndpoint: "$CP_IP:6443"
    caCertHashes:
    - "$CA_HASH"
nodeRegistration:
  kubeletExtraArgs:
    cgroup-driver: "systemd"
    provider-id: "$PROVIDER_ID"
EOF2

echo "Unmasking kubelet and joining cluster..."
sudo systemctl unmask kubelet

# Retry logic for kubeadm join to handle transient network issues or control plane startup delays.
MAX_RETRIES=2
attempt=0
backoff=15

while true; do
  attempt=$((attempt + 1))
  echo "Attempt $attempt of $MAX_RETRIES to join cluster..."

  # Run kubeadm join. We use an if-statement so that 'set -e' doesn't exit the script on failure.
  if kubeadm join --config /etc/kubernetes/kubeadm-join.yaml; then
    echo "Successfully joined the cluster!"
    break
  fi

  echo "Join attempt $attempt failed."

  if [ $attempt -ge $MAX_RETRIES ]; then
    echo "Exhausted all join retries. Failing."
    exit 1
  fi

  echo "Resetting kubeadm state before retry..."
  kubeadm reset --force

  echo "Sleeping for ${backoff}s before next attempt..."
  sleep $backoff
  backoff=$((backoff * 2))
  if [ $backoff -gt 120 ]; then
    backoff=120
  fi
done
