#!/usr/bin/env bash
# The local Vagrant box of the file-serving DC VM (docs/fileserver-vm.md): the official Ubuntu 26.04 cloud image
# (pinned release and SHA-256) on a 40 GB system disk without LVM, with the `vagrant` user from a cloud-init seed.
# The bento box carries a 64 GB disk with LVM and VirtualBox cannot shrink a disk, so the box is built here.
#
#   vagrant/fileserver/build-box.sh            # -> box "local/ubuntu-26.04-40g"
#   BOX_NAME=… SYS_GB=40 vagrant/fileserver/build-box.sh
set -euo pipefail

RELEASE=${RELEASE:-release-20261016}
IMAGE=ubuntu-26.04-server-cloudimg-amd64.ova
SHA256=${SHA256:-fb52eeb79c0e71bce675729a7d67f5a0c6691deea04d2b13a524c7e376d74ec1}
URL=https://cloud-images.ubuntu.com/releases/resolute/$RELEASE/$IMAGE
BOX_NAME=${BOX_NAME:-local/ubuntu-26.04-40g}
SYS_GB=${SYS_GB:-40}
HERE=$(cd "$(dirname "$0")" && pwd)
CACHE=${CACHE:-$HERE/.cache}
VM=fileserver-box-build

for t in VBoxManage vagrant genisoimage ssh-keygen tar sha256sum; do
  command -v "$t" >/dev/null || { echo "missing: $t" >&2; exit 1; }
done
mkdir -p "$CACHE"
cd "$CACHE"

# 1. the pinned cloud image (a copy already at hand: IMAGE_FILE=/path/to/the.ova)
if [ -n "${IMAGE_FILE:-}" ]; then cp -f "$IMAGE_FILE" "$IMAGE"; fi
[ -f "$IMAGE" ] || curl -fL --retry 3 -o "$IMAGE" "$URL"
echo "$SHA256  $IMAGE" | sha256sum -c -

# 2. its disk as a 40 GB VDI (cloud-init's growpart grows / at the first boot)
rm -f disk.vmdk system.vdi
tar -xOf "$IMAGE" --wildcards '*.vmdk' > disk.vmdk
VBoxManage clonemedium disk disk.vmdk system.vdi --format VDI >/dev/null
VBoxManage closemedium disk disk.vmdk --delete >/dev/null 2>&1 || rm -f disk.vmdk
VBoxManage modifymedium disk system.vdi --resize $((SYS_GB * 1024))

# 3. the seed: the vagrant user (Vagrant's public insecure keys: `vagrant up` replaces them), DHCP on the first NIC,
#    cloud-init off for the next boots, power off when done (a fresh host gets ~/.vagrant.d's keys on its first command)
[ -f ~/.vagrant.d/insecure_private_keys/vagrant.key.ed25519 ] || vagrant box list >/dev/null 2>&1 || true
[ -f ~/.vagrant.d/insecure_private_keys/vagrant.key.ed25519 ] || { echo "Vagrant's insecure keys are missing (~/.vagrant.d)" >&2; exit 1; }
pub=$(for k in ~/.vagrant.d/insecure_private_keys/vagrant.key.ed25519 ~/.vagrant.d/insecure_private_keys/vagrant.key.rsa; do
  ssh-keygen -y -f "$k"; done)
mkdir -p seed
cat > seed/meta-data <<EOF
instance-id: fileserver-box
local-hostname: fileserver-box
EOF
{
  echo "#cloud-config"
  echo "growpart: {mode: auto, devices: ['/']}"
  echo "resize_rootfs: true"
  echo "ssh_pwauth: false"
  echo "package_update: false"
  echo "users:"
  echo "  - name: vagrant"
  echo "    groups: [adm, sudo]"
  echo "    shell: /bin/bash"
  echo "    lock_passwd: true"
  echo "    sudo: 'ALL=(ALL) NOPASSWD:ALL'"
  echo "    ssh_authorized_keys:"
  echo "$pub" | sed 's/^/      - /'
  echo "write_files:"
  echo "  - path: /etc/netplan/01-first-nic.yaml"
  echo "    permissions: '0600'"
  echo "    content: |"
  echo "      network: {version: 2, ethernets: {enp0s3: {dhcp4: true}}}"
  echo "runcmd:"
  echo "  - rm -f /etc/netplan/50-cloud-init.yaml"
  echo "  - touch /etc/cloud/cloud-init.disabled"
  echo "  - cloud-init clean --logs"
  echo "power_state: {mode: poweroff, timeout: 60, condition: true}"
} > seed/user-data
rm -f seed.iso
genisoimage -quiet -output seed.iso -volid cidata -joliet -rock seed/user-data seed/meta-data

# 4. a clean VM: SATA (system on port 0, room for the data disk on port 1), NAT, serial console to nowhere
VBoxManage unregistervm "$VM" --delete >/dev/null 2>&1 || true
VBoxManage createvm --name "$VM" --ostype Ubuntu_64 --register >/dev/null
VBoxManage modifyvm "$VM" --memory 2048 --cpus 2 --firmware bios --nic1 nat --nictype1 virtio \
  --uart1 0x3F8 4 --uartmode1 disconnected --audio-enabled off --usb off --graphicscontroller vmsvga
VBoxManage storagectl "$VM" --name SATA --add sata --controller IntelAhci --portcount 2 --hostiocache on
VBoxManage storageattach "$VM" --storagectl SATA --port 0 --device 0 --type hdd --medium "$CACHE/system.vdi"
VBoxManage storagectl "$VM" --name IDE --add ide
VBoxManage storageattach "$VM" --storagectl IDE --port 0 --device 0 --type dvddrive --medium "$CACHE/seed.iso"
VBoxManage startvm "$VM" --type headless >/dev/null
echo "first boot (cloud-init), waiting for the power-off…"
for _ in $(seq 180); do
  VBoxManage showvminfo "$VM" --machinereadable | grep -q '^VMState="poweroff"' && break
  sleep 5
done
VBoxManage showvminfo "$VM" --machinereadable | grep -q '^VMState="poweroff"' || { echo "the VM did not power off" >&2; exit 1; }
VBoxManage storageattach "$VM" --storagectl IDE --port 0 --device 0 --medium none
VBoxManage storagectl "$VM" --name IDE --remove

# 5. the box
rm -f fileserver.box
vagrant package --base "$VM" --output fileserver.box
vagrant box add --force --name "$BOX_NAME" fileserver.box
VBoxManage unregistervm "$VM" --delete >/dev/null
rm -f fileserver.box system.vdi
echo "box $BOX_NAME ready"
