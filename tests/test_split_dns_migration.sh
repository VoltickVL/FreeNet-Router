#!/bin/sh
set -eu

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM
SRC="$TMP/src"
DST="$TMP/dst"
DST2="$TMP/dst2"
LEGACY="$TMP/legacy"
LEGACY_DST="$TMP/legacy-dst"
mkdir -p "$SRC" "$DST" "$DST2" "$LEGACY" "$LEGACY_DST"

cat > "$SRC/02_dns.json" <<'EOF'
{}
EOF

cat > "$SRC/03_inbounds.json" <<'EOF'
{
  "inbounds": [
    {"tag":"redirect","port":5000,"protocol":"dokodemo-door"},
    {"tag":"tproxy","port":5000,"protocol":"dokodemo-door"}
  ]
}
EOF

cat > "$SRC/04_outbounds.json" <<'EOF'
{
  "outbounds": [
    {"tag":"vless-reality","protocol":"freedom"},
    {"tag":"direct","protocol":"freedom"},
    {"tag":"block","protocol":"blackhole"}
  ]
}
EOF

cat > "$SRC/05_routing.json" <<'EOF'
{
  "routing": {
    "domainStrategy":"AsIs",
    "rules": [
      {"type":"field","domain":["domain:example.ru","ext:geosite.dat:private"],"outboundTag":"direct"},
      {"type":"field","domain":["ext:geosite.dat:ru-blocked"],"outboundTag":"vless-reality"},
      {"type":"field","network":"tcp,udp","outboundTag":"vless-reality"}
    ]
  }
}
EOF

sh scripts/migrate_split_dns.sh --build-only "$SRC" "$DST"

jq -e '.dns.tag == "dns-vless"' "$DST/02_dns.json" >/dev/null
jq -e '([.dns.servers[] | select(.tag == "dns-direct" and .address == "77.88.8.8" and .port == 53) | .domains[]] | index("domain:example.ru")) != null' "$DST/02_dns.json" >/dev/null
jq -e '([.dns.servers[]? | select(type == "object" and .tag == "dns-vless" and .address == "https://8.8.8.8/dns-query" and (has("port") | not))] | length) == 1' "$DST/02_dns.json" >/dev/null
jq -e 'any(.outbounds[]; .tag == "vless-reality")' "$DST/04_outbounds.json" >/dev/null
jq -e 'any(.outbounds[]; .tag == "direct")' "$DST/04_outbounds.json" >/dev/null
jq -e 'any(.outbounds[]; .tag == "block")' "$DST/04_outbounds.json" >/dev/null
jq -e 'any(.outbounds[]; .tag == "dns-out" and .protocol == "dns")' "$DST/04_outbounds.json" >/dev/null
jq -e '.routing.rules[0].inboundTag == ["dns-vless"] and .routing.rules[0].outboundTag == "vless-reality"' "$DST/05_routing.json" >/dev/null
jq -e '.routing.rules[1].inboundTag == ["dns-direct"] and .routing.rules[1].outboundTag == "direct"' "$DST/05_routing.json" >/dev/null
jq -e '.routing.rules[2].port == 53 and .routing.rules[2].outboundTag == "dns-out"' "$DST/05_routing.json" >/dev/null
jq -e 'any(.routing.rules[]; .domain == ["ext:geosite.dat:ru-blocked"] and .outboundTag == "vless-reality")' "$DST/05_routing.json" >/dev/null
cmp "$SRC/03_inbounds.json" "$DST/03_inbounds.json"

sh scripts/migrate_split_dns.sh --build-only "$DST" "$DST2"
for F in 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json; do
  jq -S . "$DST/$F" > "$TMP/a"
  jq -S . "$DST2/$F" > "$TMP/b"
  cmp "$TMP/a" "$TMP/b"
done

cp "$SRC/03_inbounds.json" "$LEGACY/03_inbounds.json"
cp "$SRC/04_outbounds.json" "$LEGACY/04_outbounds.json"
cp "$SRC/05_routing.json" "$LEGACY/05_routing.json"
cat > "$LEGACY/02_dns.json" <<'EOF'
{
  "dns": {
    "tag":"dns-vless",
    "servers":[
      {"address":"77.88.8.8","port":53,"domains":["domain:example.ru"],"skipFallback":true,"tag":"dns-direct","clientIP":"192.0.2.10"},
      {"address":"8.8.8.8","port":53,"tag":"dns-vless","skipFallback":false}
    ],
    "queryStrategy":"UseIPv4"
  }
}
EOF

DIRECT_BEFORE="$(jq -cS '[.dns.servers[] | select(.tag == "dns-direct")]' "$LEGACY/02_dns.json")"
sh scripts/migrate_split_dns.sh --build-only "$LEGACY" "$LEGACY_DST"
DIRECT_AFTER="$(jq -cS '[.dns.servers[] | select(.tag == "dns-direct")]' "$LEGACY_DST/02_dns.json")"
[ "$DIRECT_BEFORE" = "$DIRECT_AFTER" ]
jq -e '([.dns.servers[]? | select(type == "object" and .tag == "dns-vless" and .address == "https://8.8.8.8/dns-query" and (has("port") | not) and .skipFallback == false)] | length) == 1' "$LEGACY_DST/02_dns.json" >/dev/null

echo "split DNS migration candidate test: PASS"
