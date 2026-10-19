#!/usr/bin/env bash
# The Ubuntu client of destino.lab (lab-clients.sh), as root on the machine:
#   ubuntu-client.sh join     — realmd/sssd by the delegated join account (its password on stdin), OU=Estacoes,
#                               home folder created on the first logon (pam_mkhomedir), DNS through the domain's DC
#   ubuntu-client.sh check    — a domain user (its password on stdin): id, an SSH logon with password on this machine
#                               (PAM: home folder, Kerberos ticket from sssd), a share read with that ticket
#   ubuntu-client.sh adsys    — ADSys (Ubuntu's own policies from GPOs): what works without Ubuntu Pro
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }
REALM=destino.lab DC=192.168.57.40 OU="OU=Estacoes,DC=destino,DC=lab"

case $1 in
join)
  read -r pw
  if realm list --name-only 2>/dev/null | grep -qx "$REALM"; then echo "already in $REALM"; exit 0; fi
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq realmd sssd sssd-tools adcli krb5-user libnss-sss libpam-sss \
    smbclient sshpass openssh-server bind9-dnsutils >/dev/null # bind9-dnsutils: nsupdate, sssd's dynamic DNS
  systemctl disable --now systemd-resolved >/dev/null 2>&1 || true
  rm -f /etc/resolv.conf
  printf 'nameserver %s\nsearch %s\n' "$DC" "$REALM" >/etc/resolv.conf
  # the FQDN: the computer account's DNS name and the one sssd registers
  hostnamectl set-hostname "$(hostname -s).$REALM"
  printf '%s\n' "$pw" | realm join --user=lab.ingresso --computer-ou="$OU" "$REALM"
  pam-auth-update --enable mkhomedir
  # sssd registers this machine in the domain's DNS with the card that reaches the DC: left to itself it picks
  # Vagrant's NAT card (10.0.2.15) and registers nothing
  iface=$(ip -o route get "$DC" | sed -n 's/.* dev \([^ ]*\).*/\1/p')
  sed -i "/^\[domain\/$REALM\]/a dyndns_update = True\ndyndns_iface = $iface\ndyndns_update_ptr = False" /etc/sssd/sssd.conf
  systemctl restart sssd
  # domain users log on by password through SSH (the lab's check); the Vagrant key stays for vagrant
  printf 'PasswordAuthentication yes\nKbdInteractiveAuthentication yes\n' >/etc/ssh/sshd_config.d/60-lab-domain.conf
  systemctl restart ssh
  echo "$(hostname) joined $REALM ($OU)"
  ;;
check)
  read -r pw
  u=lab.ana@$REALM
  echo "id: $(id "$u" | cut -c1-120)"
  # SSHPASS in the environment (sshpass -e), never an argument
  SSHPASS=$pw sshpass -e ssh -o StrictHostKeyChecking=no -o PubkeyAuthentication=no "$u@localhost" \
    'echo "logon: $(whoami) in $PWD"; t=$(klist 2>/dev/null | grep -m1 krbtgt | awk "{print \$NF}"); echo "ticket: $t"; s=$(smbclient //dstfs.destino.lab/Publico --use-kerberos=required -c "get readme.txt -" 2>/dev/null | head -1); echo "share (Kerberos): $s"; [ -n "$t" ] && [ -n "$s" ]'
  echo "home folder: $(ls -d "/home/$u" 2>/dev/null || echo absent)"
  ;;
adsys)
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq adsys >/dev/null 2>&1 || { echo "adsys: package unavailable"; exit 0; }
  echo "adsys: $(adsysctl version 2>/dev/null | head -1)"
  echo "computer policies: $(adsysctl policy applied --machine 2>&1 | tail -3 | tr '\n' ' ')"
  ;;
*)
  sed -n '2,8p' "$0"
  exit 1
  ;;
esac
