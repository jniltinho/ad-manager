#!/usr/bin/env bash
# Seeds a local test domain with fictitious data for demos and screenshots. Never run it against production: AD_URL
# (and PANEL_URL) must point to 127.0.0.1.
#
#   scripts/demo-seed.sh                                  # the test Samba of `make samba-up` (empresa.test)
#   AD_DOMAIN=domain.local AD_URL=ldaps://127.0.0.1:16636 CONTAINER=adm-docs \
#     PANEL_URL=https://127.0.0.1:18443 scripts/demo-seed.sh   # the all-in-one image (docs/docker.md)
#
# The Administrator password comes from AD_TEST_PASSWORD (environment only). With CONTAINER, computers are created
# inside it (samba-tool); with PANEL_URL, shares are created through the panel's agent.
set -euo pipefail
DOMAIN=${AD_DOMAIN:-empresa.test}
export AD_URL="${AD_URL:-ldaps://127.0.0.1:6636}" AD_DOMAIN=$DOMAIN AD_INSECURE_SKIP_VERIFY=true
export AD_USER=Administrator AD_PASSWORD="${AD_TEST_PASSWORD:-Teste-ad-manager-2026}"
[[ "$AD_URL" =~ ^ldaps?://127\.0\.0\.1(:[0-9]+)?/?$ ]] || { echo "only against a local test domain" >&2; exit 1; }
[[ -z "${PANEL_URL:-}" || "$PANEL_URL" =~ ^https://127\.0\.0\.1(:[0-9]+)?/?$ ]] || { echo "PANEL_URL must be local" >&2; exit 1; }
B=$(sed 's/^/DC=/; s/\./,DC=/g' <<<"$DOMAIN")
bin=${BIN:-dist/ad-manager}
q() { "$bin" --config /dev/null "$@" >/dev/null 2>&1 || true; }

for ou in HQ Branch-Office IT Former-Staff; do q ou create "$ou"; done
for g in Finance Sales Board Support Design HR Purchasing Legal; do q group create "$g" --description "$g team"; done
i=0
while IFS=';' read -r sam name dept title ou group; do
  i=$((i + 1))
  printf 'Demo-%04d#ok\n' "$i" | "$bin" --config /dev/null user create "$sam" --name "$name" --mail "$sam@$DOMAIN" \
    --department "$dept" --title "$title" --ou "OU=$ou,$B" >/dev/null 2>&1 || true
  q group add "$group" "$sam"
done <<'CSV'
ana.souza;Ana Souza;Finance;Financial analyst;HQ;Finance
bruno.lima;Bruno Lima;Sales;Account executive;HQ;Sales
carla.mendes;Carla Mendes;Board;Chief executive officer;HQ;Board
diego.rocha;Diego Rocha;IT;Support analyst;IT;Support
elisa.prado;Elisa Prado;Design;Designer;Branch-Office;Design
fabio.nunes;Fabio Nunes;Sales;Salesperson;Branch-Office;Sales
gabi.torres;Gabriela Torres;Finance;Accountant;HQ;Finance
heitor.alves;Heitor Alves;IT;Network administrator;IT;Support
isabela.costa;Isabela Costa;HR;HR analyst;HQ;HR
joao.pereira;Joao Pereira;Purchasing;Buyer;Branch-Office;Purchasing
karina.dias;Karina Dias;Legal;Lawyer;HQ;Legal
lucas.martins;Lucas Martins;Sales;Sales manager;HQ;Sales
marina.ribeiro;Marina Ribeiro;Design;Art director;Branch-Office;Design
nicolas.ferreira;Nicolas Ferreira;Finance;Treasurer;HQ;Finance
olivia.santos;Olivia Santos;HR;HR coordinator;HQ;HR
paulo.gomes;Paulo Gomes;Purchasing;Purchasing manager;Branch-Office;Purchasing
rafaela.barros;Rafaela Barros;Board;Chief financial officer;HQ;Board
sergio.melo;Sergio Melo;IT;Intern;IT;Support
CSV
q user disable fabio.nunes
q user move fabio.nunes "OU=Former-Staff,$B"
q user disable sergio.melo
q group add "Domain Admins" diego.rocha
q group add "Domain Admins" heitor.alves
q group add Board carla.mendes

if [[ -n "${CONTAINER:-}" ]]; then # computer accounts, inside the DC container (no password needed as root)
  for pc in PC-FIN-01:HQ PC-FIN-02:HQ PC-SALES-01:HQ PC-BOARD-01:HQ NB-DESIGN-01:Branch-Office \
    NB-SALES-02:Branch-Office SRV-FILES-01:IT SRV-PRINT-01:IT; do
    docker exec "$CONTAINER" samba-tool computer create "${pc%%:*}" --computerou="OU=${pc#*:}" >/dev/null 2>&1 || true
  done
fi

if [[ -n "${PANEL_URL:-}" ]]; then # shares on the panel's first file server (its agent creates folder and ACL)
  jar=$(mktemp)
  trap 'rm -f "$jar"' EXIT
  python3 -c 'import json,os;print(json.dumps({"user":"Administrator","password":os.environ["AD_PASSWORD"]}))' |
    curl -skf -c "$jar" -H 'Content-Type: application/json' -d @- "$PANEL_URL/api/login" >/dev/null
  id=$(curl -skf -b "$jar" "$PANEL_URL/api/agents" | python3 -c 'import json,sys;print(json.load(sys.stdin)[0]["id"])')
  nb=$(cut -d. -f1 <<<"${DOMAIN^^}")
  share() { # name, comment, full, rw, ro
    python3 -c 'import json,sys
n,c,f,w,r,nb=sys.argv[1:7]
g=lambda s:[nb+"\\"+x for x in s.split(",") if x]
print(json.dumps({"type":"share:create","payload":{"name":n,"path":"/srv/shares/"+n.lower(),"comment":c,
 "full_groups":g(f),"rw_groups":g(w),"ro_groups":g(r),"create_path":True,"recycle":True,"abe":n!="Public"}}))' "$@" "$nb" |
      curl -skf -b "$jar" -H 'Content-Type: application/json' -d @- "$PANEL_URL/api/agents/$id/tasks" >/dev/null || true
  }
  share Finance "Spreadsheets and invoices" "Board" "Finance" ""
  share Sales "Proposals and contracts" "" "Sales" "Board"
  share Board "Board documents" "Board" "" ""
  share HR "Personnel (restricted)" "" "HR" "Board"
  share Design "Creative files" "" "Design" "Sales"
  share Public "Files for everyone" "" "Domain Users" ""
  if [[ -n "${CONTAINER:-}" ]]; then # a few files in Public, for the sync screens
    docker exec "$CONTAINER" bash -c 'for i in $(seq 30); do [[ -d /srv/shares/public ]] && break; sleep 1; done
      mkdir -p /srv/shares/public/Manuals /srv/shares/public/Templates
      for i in 1 2 3; do head -c 200000 /dev/urandom > /srv/shares/public/Manuals/manual-$i.pdf; done
      echo template > /srv/shares/public/Templates/proposal.docx' || true
  fi
fi
echo "demo ready at $AD_URL"
