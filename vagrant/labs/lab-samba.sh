#!/usr/bin/env bash
# Samba4 → Samba4 lab (openspec lab-samba-ubuntu): origem-samba.lab (samba-src DC, samba-fs file server) taken to
#   path A — import into destino.lab (samba-dst DC, samba-dst-fs file server): objects, DNS (--create-zones),
#            shares with their access by name, files by the panel's sync;
#   path B — migration of the DC itself: samba-mig joins as an additional DC, takes the FSMO roles and SYSVOL,
#            samba-src is demoted.
# Every machine is installed by the .deb and its setup wizard (API), as an operator would.
#
#   LAB=1 bash vagrant/labs/lab-samba.sh setup|populate|path-a|validate|path-b|all
#
# Passwords: LAB_SAMBA_SRC_PASSWORD, LAB_SAMBA_DST_PASSWORD and LAB_SAMBA_USER_PASSWORD of .run/labs/secrets.env
# (lab-lib.sh).
set -euo pipefail
cd "$(dirname "$0")"
# shellcheck source=lab-lib.sh
. ./lab-lib.sh
OUT=../../.run/lab-samba
mkdir -p "$OUT"
SRC=origem-samba.lab SRCNB=ORIGEMSAMBA DST=destino.lab DSTNB=DESTINO

setup() {
  install samba-src samba-dst samba-fs samba-dst-fs samba-mig
  wizard samba-src "$LAB_SAMBA_SRC_PASSWORD" "{\"role\":\"new-dc\",\"realm\":\"$SRC\",\"domain\":\"$SRCNB\",\"ip\":\"192.168.57.30\",\"files\":false}"
  wizard samba-dst "$LAB_SAMBA_DST_PASSWORD" "{\"role\":\"new-dc\",\"realm\":\"$DST\",\"domain\":\"$DSTNB\",\"ip\":\"192.168.57.40\",\"files\":false}"
  wizard samba-fs "$LAB_SAMBA_SRC_PASSWORD" "{\"role\":\"member\",\"realm\":\"$SRC\",\"domain\":\"$SRCNB\",\"dns_dc\":\"192.168.57.30\",\"share_root\":\"/srv/shares\"}"
  wizard samba-dst-fs "$LAB_SAMBA_DST_PASSWORD" "{\"role\":\"member\",\"realm\":\"$DST\",\"domain\":\"$DSTNB\",\"dns_dc\":\"192.168.57.40\",\"share_root\":\"/srv/shares\"}"
  wizard samba-mig "$LAB_SAMBA_SRC_PASSWORD" "{\"role\":\"dc-join\",\"realm\":\"$SRC\",\"domain\":\"$SRCNB\",\"ip\":\"192.168.57.32\",\"src_dc\":\"192.168.57.30\",\"files\":false}"
  register samba-src "$LAB_SAMBA_SRC_PASSWORD" samba-fs srcfs 192.168.57.31
  register samba-dst "$LAB_SAMBA_DST_PASSWORD" samba-dst-fs dstfs 192.168.57.41
}

populate() {
  log "objects, passwords, GPOs and DNS of $SRC (populate-samba.sh dc, ad-manager CLI)"
  printf '%s\n%s\n' "$LAB_SAMBA_SRC_PASSWORD" "$LAB_SAMBA_USER_PASSWORD" |
    vm samba-src "sudo bash /vagrant/vagrant/labs/populate-samba.sh dc $SRCNB" | grep -vE '^(Password for|$)' | tail -20
  log "folders, permissions and files on samba-fs (populate-samba.sh fs, ad-manager agent acl set)"
  vm samba-fs "sudo bash /vagrant/vagrant/labs/populate-samba.sh fs $SRCNB"
  log "shares of samba-fs (samba-src panel)"
  printf '%s\n' "$LAB_SAMBA_SRC_PASSWORD" | vm samba-src "$api_prelude"'
A=$(api GET /agents | jq ".[]|select(.name==\"srcfs\")|.id")
grep -v "^#" /vagrant/vagrant/labs/data/folders.csv | while IFS=";" read -r share path rw ro full sub; do
  # groups as the screen sends them: DOMAIN\group (a member server knows only qualified names)
  body=$(jq -n --arg n "$share" --arg p "/srv/shares/$path" --arg rw "$rw" --arg ro "$ro" --arg f "$full" --arg d "ORIGEMSAMBA\\" \
    "def g: split(\",\")|map(select(.!=\"\")|\$d+.); {type:\"share:create\",payload:{name:\$n,path:\$p,browseable:true,rw_groups:(\$rw|g),ro_groups:(\$ro|g),full_groups:(\$f|g)}}")
  id=$(api POST /agents/$A/tasks "$body" | jq -r .id); sleep 3
  echo "share $share: $(api GET /agents/$A/tasks/$id | jq -c "{state,error:.last_error}")"
done'
}

path_a() {
  log "path A: import of $SRC into $DST (objects, DNS with --create-zones, shares)"
  printf '%s\n%s\n' "$LAB_SAMBA_DST_PASSWORD" "$LAB_SAMBA_SRC_PASSWORD" | vm samba-dst "$api_prelude"'
read -r SPW; export SPW
A=$(api GET /agents | jq ".[]|select(.name==\"dstfs\")|.id")
body() { jq -n --argjson a "$A" "{url:\"ldaps://192.168.57.30\",domain:\"origem-samba.lab\",user:\"Administrator\",password:env.SPW,insecure:true,objects:true,dns:true,create_zones:true,shares:{host:\"192.168.57.31\",agent:\$a,map:\"C:\\\\srv\\\\shares=/srv/shares\"}}"; }
body | api POST /import/ad/plan - | jq -c "{plan: ([.plan[]|.kind]|group_by(.)|map({(.[0]):length})|add), error}"
body | api POST /import/ad/apply - | jq -c "{failed: [.plan[]|select(.ok==false and .kind!=\"skip\")|{kind,target,result}], error}"' | tee "$OUT/path-a-import.log"
  printf '%s\n' "$LAB_SAMBA_USER_PASSWORD" | vm samba-dst "sudo bash /vagrant/vagrant/labs/populate-samba.sh passwords $DSTNB"
  log "path A: files by the panel's sync (samba-fs → samba-dst-fs, NTLM, the target's inheritance)"
  printf '%s\n%s\n' "$LAB_SAMBA_DST_PASSWORD" "$LAB_SAMBA_SRC_PASSWORD" | vm samba-dst "$api_prelude"'
read -r SPW; export SPW
A=$(api GET /agents | jq ".[]|select(.name==\"dstfs\")|.id")
for share in Financeiro Diretoria TI Publico; do
  id=$(tr "[:upper:]" "[:lower:]" <<<"lab-$share")
  jq -n --arg id "$id" --arg s "$share" "{id:\$id,name:(\"Lab \"+\$s),source:{host:\"192.168.57.31\",share:\$s,auth:\"ntlm\",user:\"Administrator\",domain:\"ORIGEMSAMBA\"},password:env.SPW,dest:(\"/srv/shares/\"+\$s),mode:\"copy\",reinherit:true,max_delete:0,max_delete_pct:0,enabled:false}" |
    api PUT /agents/$A/syncs/$id - | jq -c "{saved:.id,error}"
  run=$(api POST /agents/$A/syncs/$id/run | jq -r ".run // .id")
  for _ in $(seq 60); do st=$(api GET /agents/$A/syncs | jq -r --arg r "$run" ".runs[]|select(.id==\$r)|.status"); [ "$st" = running ] || [ "$st" = queued ] || break; sleep 3; done
  echo "sync $share: $(api GET /agents/$A/syncs | jq -c --arg r "$run" ".runs[]|select(.id==\$r)|{status,error,transfers:.result.transfers}")"
done' | tee "$OUT/path-a-sync.log"
}

validate() {
  log "path A validation: access by name at the root of each share (source × target)"
  local report="$OUT/path-a-validate.md"
  {
    echo "# Path A — access by name (origem-samba.lab → destino.lab)"
    echo
    echo "| Share | Source | Target | Same |"
    echo "|---|---|---|---|"
    grep -v '^#' data/folders.csv | while IFS=';' read -r share path _; do
      norm='[.aces[]|select(.type=="allow")|{n:(.name|split("\\")|last|ascii_downcase),a:.access}|select(.n|test("^(system|administrators|domain admins|creator owner|enterprise admins)$")|not)|"\(.n)=\(.a)"]|unique|join(" ")'
      # </dev/null: ssh would read the rest of the list this loop reads
      s=$(vm samba-fs "sudo ad-manager agent acl --json /srv/shares/$path | jq -r '$norm'" </dev/null | tr -d '\r')
      d=$(vm samba-dst-fs "sudo ad-manager agent acl --json /srv/shares/$path | jq -r '$norm'" </dev/null | tr -d '\r')
      echo "| $share | $s | $d | $([ "$s" = "$d" ] && echo yes || echo **NO**) |"
    done
    echo
    echo "## Not carried over by the current path (expected)"
    echo
    echo "- A subfolder's own ACL: \`Financeiro/Restrito\` at the source: $(vm samba-fs "sudo ad-manager agent acl --json /srv/shares/Financeiro/Restrito | jq -r '[.aces[]|select(.inherited|not)|.name]|join(\", \")'" | tr -d '\r'); at the target: $(vm samba-dst-fs "sudo ad-manager agent acl --json /srv/shares/Financeiro/Restrito | jq -r 'if .protected then \"protected\" else \"inherits from the root\" end'" | tr -d '\r')"
    echo
    echo "## Effective access over SMB at the target"
    echo
    printf '%s\n' "$LAB_SAMBA_USER_PASSWORD" | vm samba-dst-fs 'read -r PASSWD; export PASSWD; echo ok > /tmp/t.txt
try() { r=$(smbclient "//127.0.0.1/$2" -U "DESTINO\\$1" -c "$3" 2>&1 | grep -oE "NT_STATUS_[A-Z_]+" | head -1); echo "- $1 on $2 ($4): ${r:-ok}"; }
try lab.ana Financeiro "put /tmp/t.txt t-ana.txt; del t-ana.txt" "writes: expected ok"
try lab.caio Financeiro "ls" "reads: expected ok"
try lab.caio Financeiro "put /tmp/t.txt t-caio.txt" "writes: expected ACCESS_DENIED"
try lab.edu Financeiro "ls" "expected ACCESS_DENIED"
try lab.edu Publico "ls" "reads: expected ok"
try lab.dani Diretoria "ls" "expected ACCESS_DENIED"
try lab.dani TI "put /tmp/t.txt t-dani.txt; del t-dani.txt" "writes: expected ok"'
    echo
    echo "## Files"
    echo
    for p in Financeiro/data.bin Publico/readme.txt; do
      echo "- $p: source $(vm samba-fs "sudo sha256sum /srv/shares/$p | cut -c1-16" | tr -d '\r'), target $(vm samba-dst-fs "sudo sha256sum /srv/shares/$p 2>/dev/null | cut -c1-16" | tr -d '\r')"
    done
  } | tee "$report"
}

path_b() {
  log "path B: samba-mig takes over $SRC (replication, FSMO, SYSVOL, demotion of samba-src)"
  local d=/vagrant/vagrant/labs/samba-dc.sh
  {
    printf '%s\n' "$LAB_SAMBA_SRC_PASSWORD" | vm samba-mig "sudo bash $d replicate srcdc"
    printf '%s\n' "$LAB_SAMBA_SRC_PASSWORD" | vm samba-mig "sudo bash $d take-roles"
    # SYSVOL: Samba does not replicate it; copied from samba-src with its attributes, then its ACLs reset
    vm samba-src 'sudo tar --xattrs --xattrs-include="*" -C /var/lib/samba -cf - sysvol' </dev/null |
      vm samba-mig "sudo tar --xattrs --xattrs-include='*' -C /var/lib/samba -xpf - && echo 'SYSVOL copied'"
    vm samba-mig "sudo bash $d sysvol-reset" </dev/null
    printf '%s\n' "$LAB_SAMBA_SRC_PASSWORD" | vm samba-src "sudo bash $d demote"
    vm samba-mig "sudo bash $d cleanup" </dev/null
    vm samba-mig "sudo bash $d report" </dev/null
  } 2>&1 | tee "$OUT/path-b.log"
}

case "${1:-}" in
  setup) setup ;;
  populate) populate ;;
  path-a) path_a ;;
  validate) validate ;;
  path-b) path_b ;;
  all) # one after the other: in an "a && b" list bash would not stop at a failure inside a
    setup
    populate
    path_a
    validate
    path_b
    ;;
  *) sed -n '2,12p' "$0"; exit 1 ;;
esac
