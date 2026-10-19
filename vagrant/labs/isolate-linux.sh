#!/usr/bin/env bash
# First provisioner of every Linux lab VM: no traffic to or from the production LANs (LAB_BLOCK, from the
# Vagrantfile), persistent across reboots. The Vagrant NAT adapter stays (SSH, packages), so the block is in the guest.
set -euo pipefail
LAB_BLOCK=${LAB_BLOCK:-192.168.10.0/24 192.168.20.0/24}
LAB_PROBE=${LAB_PROBE:-192.168.10.10}
nets=$(echo "$LAB_BLOCK" | tr -s ' ' | sed 's/ /, /g')
cat > /etc/nftables.conf <<NFT
#!/usr/sbin/nft -f
flush ruleset
table inet labiso {
  chain out { type filter hook output priority filter; policy accept; ip daddr { $nets } drop; }
  chain in  { type filter hook input  priority filter; policy accept; ip saddr { $nets } drop; }
  chain fwdlan { type filter hook forward priority filter; policy accept; ip daddr { $nets } drop; }
}
NFT
systemctl enable --now nftables >/dev/null 2>&1 || true
nft -f /etc/nftables.conf
if timeout 3 bash -c "</dev/tcp/$LAB_PROBE/389" 2>/dev/null; then echo "ERROR: the production LAN is reachable" >&2; exit 1; fi
echo "isolation ok: $LAB_BLOCK blocked"
