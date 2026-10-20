#!/usr/bin/env bash
# Proxied A records for the apex and www on Cloudflare, SSL mode strict. Env: CF (token), SERVER_IP, DOMAIN (default wayfeed.app).
set -euo pipefail
: "${CF:?}" "${SERVER_IP:?}"; D=${DOMAIN:-wayfeed.app}; API=https://api.cloudflare.com/client/v4
call() { curl -fsS -H "Authorization: Bearer $CF" -H 'Content-Type: application/json' "$@"; }
Z=$(call "$API/zones?name=$D" | python3 -c 'import json,sys;print(json.load(sys.stdin)["result"][0]["id"])')
for n in "$D" "www.$D"; do
  id=$(call "$API/zones/$Z/dns_records?type=A&name=$n" | python3 -c 'import json,sys;r=json.load(sys.stdin)["result"];print(r[0]["id"] if r else "")')
  body="{\"type\":\"A\",\"name\":\"$n\",\"content\":\"$SERVER_IP\",\"proxied\":true,\"ttl\":1}"
  if [ -z "$id" ]; then call -X POST "$API/zones/$Z/dns_records" -d "$body" >/dev/null; else call -X PUT "$API/zones/$Z/dns_records/$id" -d "$body" >/dev/null; fi
  echo "ok $n"
done
call -X PATCH "$API/zones/$Z/settings/ssl" -d '{"value":"strict"}' >/dev/null && echo "ssl strict"
