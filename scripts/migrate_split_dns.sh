#!/bin/sh

CONFIG_DIR="${FREENET_CONFIG_DIR:-/opt/etc/xray/configs}"
DNS_FILE="$CONFIG_DIR/02_dns.json"
INBOUND_FILE="$CONFIG_DIR/03_inbounds.json"
OUTBOUND_FILE="$CONFIG_DIR/04_outbounds.json"
ROUTING_FILE="$CONFIG_DIR/05_routing.json"
XKEEN_INIT="${FREENET_XKEEN_INIT:-}"
XKEEN_BIN="${FREENET_XKEEN_BIN:-/opt/sbin/xkeen}"
XRAY_BIN="${FREENET_XRAY_BIN:-/opt/sbin/xray}"
XRAY_ASSET_DIR="${FREENET_XRAY_ASSET_DIR:-/opt/etc/xray/dat}"
BACKUP_ROOT="${FREENET_BACKUP_ROOT:-/opt/backups}"
RUNTIME_TIMEOUT="${FREENET_XKEEN_RUNTIME_TIMEOUT:-75}"
TMP_DIR=""
BACKUP_DIR=""
MODE=""

info() { printf '[FreeNet DNS] %s\n' "$*"; }
err() { printf '[FreeNet DNS] ERROR: %s\n' "$*" >&2; }

cleanup() { [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null || true; }
trap cleanup 0 1 2 15

need_cmd() {
    command -v "$1" >/dev/null 2>&1 || { err "не найдена обязательная команда: $1"; exit 1; }
}

resolve_xkeen_init() {
    if [ -n "$XKEEN_INIT" ] && [ -f "$XKEEN_INIT" ]; then return 0; fi
    for CANDIDATE in /opt/etc/init.d/S99xkeen /opt/etc/init.d/S05xkeen; do
        if [ -f "$CANDIDATE" ]; then XKEEN_INIT="$CANDIDATE"; return 0; fi
    done
    return 1
}

run_bounded() {
    LIMIT="$1"; LOG_FILE="$2"; shift 2
    "$@" > "$LOG_FILE" 2>&1 &
    CMD_PID=$!; ELAPSED=0
    while kill -0 "$CMD_PID" 2>/dev/null; do
        if [ "$ELAPSED" -ge "$LIMIT" ]; then
            err "runtime command timeout after ${LIMIT}s: $*"
            kill -TERM "$CMD_PID" 2>/dev/null || true
            sleep 2
            kill -0 "$CMD_PID" 2>/dev/null && kill -KILL "$CMD_PID" 2>/dev/null || true
            wait "$CMD_PID" 2>/dev/null || true
            return 124
        fi
        sleep 1; ELAPSED=$((ELAPSED + 1))
    done
    wait "$CMD_PID"
}

restart_xkeen() {
    [ -n "$XKEEN_INIT" ] && [ -x "$XKEEN_INIT" ] || return 1
    run_bounded "$RUNTIME_TIMEOUT" "$1" "$XKEEN_INIT" restart on
}

make_tmp() {
    TMP_DIR="$(mktemp -d /tmp/freenet-dns.XXXXXX 2>/dev/null)"
    if [ -z "$TMP_DIR" ] || [ ! -d "$TMP_DIR" ]; then
        TMP_DIR="/tmp/freenet-dns.$$"
        mkdir -p "$TMP_DIR" || { err 'не удалось создать временный каталог'; exit 1; }
    fi
}

build_split_candidate() {
    SRC="$1"; DST="$2"
    mkdir -p "$DST" || return 1
    for NAME in 03_inbounds.json 04_outbounds.json 05_routing.json; do [ -f "$SRC/$NAME" ] || return 1; jq -e . "$SRC/$NAME" >/dev/null 2>&1 || return 1; done

    # DNS server selection must mirror Xray first-match domain routing. A flat union
    # of all DIRECT domain lists is wrong when an earlier VPN rule overlaps a later
    # DIRECT group (for example YouTube before geosite:google). Keep one ordered DNS
    # server selector per domain rule and stop at the first match with finalQuery.
    DNS_SERVERS="$(jq -c '[
        .routing.rules[]?
        | select((.domain? | type) == "array" and (.domain | length) > 0)
        | if .outboundTag == "direct" then
            {address:"77.88.8.8",port:53,domains:.domain,skipFallback:true,finalQuery:true,tag:"dns-direct"}
          else
            {address:"https://8.8.8.8/dns-query",domains:.domain,skipFallback:true,finalQuery:true,tag:"dns-vless"}
          end
      ] + [{address:"https://8.8.8.8/dns-query",tag:"dns-vless",finalQuery:true}]' "$SRC/05_routing.json" 2>/dev/null)" || return 1
    jq -n --argjson servers "$DNS_SERVERS" '{dns:{tag:"dns-vless",servers:$servers,queryStrategy:"UseIPv4"}}' > "$DST/02_dns.json" || return 1

    cp -p "$SRC/03_inbounds.json" "$DST/03_inbounds.json" || return 1

    jq '.outbounds = ([.outbounds[]? | select((.tag // "") != "dns-out")] + [{"protocol":"dns","tag":"dns-out"}])' \
        "$SRC/04_outbounds.json" > "$DST/04_outbounds.json" || return 1

    jq '.routing.rules = (
        [
          {"type":"field","inboundTag":["dns-vless"],"outboundTag":"vless-reality"},
          {"type":"field","inboundTag":["dns-direct"],"outboundTag":"direct"},
          {"type":"field","port":53,"outboundTag":"dns-out"}
        ] + [
          .routing.rules[]?
          | select((.outboundTag // "") != "dns-out")
          | select(((.inboundTag // []) | index("dns-vless")) == null)
          | select(((.inboundTag // []) | index("dns-direct")) == null)
          | select(((.inboundTag // []) | index("dns-in")) == null)
          | select((((.port // "") | tostring)) != "53")
        ])' "$SRC/05_routing.json" > "$DST/05_routing.json" || return 1

    jq -e '.dns.tag == "dns-vless" and ([.dns.servers[]?.tag] | index("dns-direct") != null) and ([.dns.servers[]?.tag] | index("dns-vless") != null)' "$DST/02_dns.json" >/dev/null 2>&1 || return 1
    jq -e '([.dns.servers[]? | select(type == "object" and .tag == "dns-vless" and .address == "https://8.8.8.8/dns-query" and (has("domains") | not) and .finalQuery == true)] | length) == 1' "$DST/02_dns.json" >/dev/null 2>&1 || return 1
    jq -e 'all(.dns.servers[]? | select(has("domains")); .finalQuery == true and .skipFallback == true)' "$DST/02_dns.json" >/dev/null 2>&1 || return 1
    jq -e '([.outbounds[]? | select(.tag == "dns-out" and .protocol == "dns")] | length) == 1' "$DST/04_outbounds.json" >/dev/null 2>&1 || return 1
    jq -e '([.routing.rules[]? | select(((.inboundTag // []) | index("dns-vless")) != null and .outboundTag == "vless-reality")] | length) == 1' "$DST/05_routing.json" >/dev/null 2>&1 || return 1
    jq -e '([.routing.rules[]? | select(((.inboundTag // []) | index("dns-direct")) != null and .outboundTag == "direct")] | length) == 1' "$DST/05_routing.json" >/dev/null 2>&1 || return 1
    jq -e '([.routing.rules[]? | select((((.port // "") | tostring) == "53") and .outboundTag == "dns-out")] | length) == 1' "$DST/05_routing.json" >/dev/null 2>&1 || return 1
    return 0
}

if [ "${1:-}" = "--build-only" ]; then
    [ $# -eq 3 ] || { err "usage: $0 --build-only INPUT_CONFIG_DIR OUTPUT_CONFIG_DIR"; exit 1; }
    need_cmd jq
    build_split_candidate "$2" "$3" || { err 'не удалось построить candidate Split DNS'; exit 1; }
    exit 0
fi

for C in jq cp mv grep awk sed mktemp sha256sum netstat pidof cmp; do need_cmd "$C"; done
case "$RUNTIME_TIMEOUT" in ''|*[!0-9]*) err 'некорректный timeout XKeen runtime'; exit 1 ;; esac
[ "$RUNTIME_TIMEOUT" -gt 0 ] || { err 'timeout XKeen runtime должен быть больше нуля'; exit 1; }
[ -x "$XKEEN_BIN" ] || { err "XKeen не найден: $XKEEN_BIN"; exit 1; }
[ -x "$XRAY_BIN" ] || { err "Xray не найден: $XRAY_BIN"; exit 1; }
resolve_xkeen_init || { err 'init XKeen не найден: S99xkeen/S05xkeen'; exit 1; }
[ -x "$XKEEN_INIT" ] || { err "init XKeen не исполняемый: $XKEEN_INIT"; exit 1; }
[ -d "$CONFIG_DIR" ] || { err "Xray config dir не найден: $CONFIG_DIR"; exit 1; }
[ -d "$XRAY_ASSET_DIR" ] || { err "Xray asset dir не найден: $XRAY_ASSET_DIR"; exit 1; }
for F in "$DNS_FILE" "$INBOUND_FILE" "$OUTBOUND_FILE" "$ROUTING_FILE"; do [ -f "$F" ] || { err "не найден обязательный Xray config: $F"; exit 1; }; jq -e . "$F" >/dev/null 2>&1 || { err "невалидный JSON: $F"; exit 1; }; done

grep -Eq '^[[:space:]]*proxy_dns="?off"?[[:space:]]*$' "$XKEEN_INIT" || { err 'XKeen proxy_dns должен оставаться off; migration остановлена до безопасного preflight'; exit 1; }
XPID="$(pidof xray 2>/dev/null | awk '{print $1}')"
[ -n "$XPID" ] || { err 'Xray process не запущен'; exit 1; }
XGID="$(awk '/^Gid:/ {print $2; exit}' "/proc/$XPID/status" 2>/dev/null)"
[ "$XGID" = 11111 ] || { err 'Xray GID не равен XKeen exclusion GID 11111; migration остановлена'; exit 1; }

PORT53="$(netstat -lnp 2>/dev/null | grep ':53[[:space:]]' || true)"
if printf '%s\n' "$PORT53" | grep -q '/xray'; then
    MODE=standard-backend
    DNS_INBOUND_COUNT="$(jq -r '[.inbounds[]? | select(((.port // "") | tostring) == "53" and .protocol == "dokodemo-door")] | length' "$INBOUND_FILE")"
    [ "$DNS_INBOUND_COUNT" = 1 ] || { err 'Xray :53 есть, но DNS inbound topology невалидна'; exit 1; }
elif printf '%s\n' "$PORT53" | grep -q '/ndnproxy'; then
    MODE=legacy-direct53
else
    err 'не подтверждён DNS backend: ожидается Xray :53 или firmware owner ndnproxy :53'
    exit 1
fi

make_tmp
CANDIDATE_DIR="$TMP_DIR/candidate"
build_split_candidate "$CONFIG_DIR" "$CANDIDATE_DIR" || { err 'не удалось построить Split DNS candidate'; exit 1; }

VLESS_BEFORE="$(jq -cS '[.outbounds[]? | select(.tag == "vless-reality")]' "$OUTBOUND_FILE" | sha256sum | awk '{print $1}')" || exit 1
VLESS_CANDIDATE="$(jq -cS '[.outbounds[]? | select(.tag == "vless-reality")]' "$CANDIDATE_DIR/04_outbounds.json" | sha256sum | awk '{print $1}')" || exit 1
[ "$VLESS_BEFORE" = "$VLESS_CANDIDATE" ] || { err 'candidate неожиданно меняет VLESS credentials'; exit 1; }

NON_VLESS_BEFORE="$(jq -cS '[.outbounds[]? | select(.tag != "vless-reality" and .tag != "dns-out")]' "$OUTBOUND_FILE" | sha256sum | awk '{print $1}')" || exit 1
NON_VLESS_CANDIDATE="$(jq -cS '[.outbounds[]? | select(.tag != "vless-reality" and .tag != "dns-out")]' "$CANDIDATE_DIR/04_outbounds.json" | sha256sum | awk '{print $1}')" || exit 1
[ "$NON_VLESS_BEFORE" = "$NON_VLESS_CANDIDATE" ] || { err 'candidate неожиданно меняет существующие non-VLESS outbounds'; exit 1; }

INBOUND_BEFORE="$(jq -cS . "$INBOUND_FILE" | sha256sum | awk '{print $1}')"
INBOUND_CANDIDATE="$(jq -cS . "$CANDIDATE_DIR/03_inbounds.json" | sha256sum | awk '{print $1}')"
[ "$INBOUND_BEFORE" = "$INBOUND_CANDIDATE" ] || { err 'candidate неожиданно меняет Xray inbounds'; exit 1; }

XRAY_LOCATION_ASSET="$XRAY_ASSET_DIR" "$XRAY_BIN" run -test -confdir "$CANDIDATE_DIR" > "$TMP_DIR/xray-candidate.log" 2>&1 || {
    tail -n 40 "$TMP_DIR/xray-candidate.log" >&2 2>/dev/null || true
    err 'candidate Xray config не прошёл validation; live state не изменён'
    exit 1
}

STAMP="$(date +%Y%m%d-%H%M%S)"
BACKUP_DIR="$BACKUP_ROOT/freenet-dns-migrate-$STAMP"
mkdir -p "$BACKUP_DIR" || { err "не удалось создать backup: $BACKUP_DIR"; exit 1; }
for NAME in 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json; do cp -p "$CONFIG_DIR/$NAME" "$BACKUP_DIR/$NAME" || { err "не удалось сохранить backup $NAME"; exit 1; }; done
sha256sum "$BACKUP_DIR"/*.json > "$BACKUP_DIR/SHA256SUMS.before" 2>/dev/null || true

rollback_live() {
    RB_OK=1
    for NAME in 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json; do
        if cp -p "$BACKUP_DIR/$NAME" "$CONFIG_DIR/$NAME.rollback.$$" 2>/dev/null; then mv -f "$CONFIG_DIR/$NAME.rollback.$$" "$CONFIG_DIR/$NAME" 2>/dev/null || RB_OK=0; else RB_OK=0; fi
    done
    restart_xkeen "$TMP_DIR/xkeen-rollback.log" || RB_OK=0
    pidof xray >/dev/null 2>&1 || RB_OK=0
    XRAY_LOCATION_ASSET="$XRAY_ASSET_DIR" "$XRAY_BIN" run -test -confdir "$CONFIG_DIR" > "$TMP_DIR/xray-rollback.log" 2>&1 || RB_OK=0
    [ "$RB_OK" = 1 ]
}

fail_after_apply() {
    PRIMARY="$1"
    err "PRIMARY ERROR: $PRIMARY"
    if rollback_live; then err 'ROLLBACK: SUCCESS; предыдущая Xray конфигурация восстановлена'; exit 1; fi
    err 'ROLLBACK ERROR: FAILED/UNKNOWN; дальнейшая mutation запрещена до read-only inspection'
    exit 2
}

for NAME in 02_dns.json 04_outbounds.json 05_routing.json; do cp -p "$CANDIDATE_DIR/$NAME" "$CONFIG_DIR/$NAME.freenet.$$" || { rm -f "$CONFIG_DIR"/*.freenet.$$ 2>/dev/null || true; err "не удалось stage $NAME; live state не изменён"; exit 1; }; done
mv -f "$CONFIG_DIR/02_dns.json.freenet.$$" "$DNS_FILE" || fail_after_apply 'не удалось применить 02_dns.json'
mv -f "$CONFIG_DIR/04_outbounds.json.freenet.$$" "$OUTBOUND_FILE" || fail_after_apply 'не удалось применить 04_outbounds.json'
mv -f "$CONFIG_DIR/05_routing.json.freenet.$$" "$ROUTING_FILE" || fail_after_apply 'не удалось применить 05_routing.json'

restart_xkeen "$TMP_DIR/xkeen-restart.log" || fail_after_apply 'XKeen init restart завершился ошибкой/timeout'
pidof xray >/dev/null 2>&1 || fail_after_apply 'Xray не запущен после migration restart'
XRAY_LOCATION_ASSET="$XRAY_ASSET_DIR" "$XRAY_BIN" run -test -confdir "$CONFIG_DIR" > "$TMP_DIR/xray-live.log" 2>&1 || fail_after_apply 'live Xray config не проходит validation после apply'

jq -e '([.dns.servers[]? | select(type == "object" and .tag == "dns-vless" and .address == "https://8.8.8.8/dns-query" and (has("domains") | not) and .finalQuery == true)] | length) == 1' "$DNS_FILE" >/dev/null 2>&1 || fail_after_apply 'dns-vless fallback DoH transport отсутствует после apply'
jq -e 'all(.dns.servers[]? | select(has("domains")); .finalQuery == true and .skipFallback == true)' "$DNS_FILE" >/dev/null 2>&1 || fail_after_apply 'DNS first-match selectors невалидны после apply'
jq -e '([.outbounds[]? | select(.tag == "dns-out" and .protocol == "dns")] | length) == 1' "$OUTBOUND_FILE" >/dev/null 2>&1 || fail_after_apply 'dns-out отсутствует после apply'
jq -e '([.routing.rules[]? | select(((.inboundTag // []) | index("dns-vless")) != null and .outboundTag == "vless-reality")] | length) == 1' "$ROUTING_FILE" >/dev/null 2>&1 || fail_after_apply 'dns-vless не направлен через vless-reality'
jq -e '([.routing.rules[]? | select(((.inboundTag // []) | index("dns-direct")) != null and .outboundTag == "direct")] | length) == 1' "$ROUTING_FILE" >/dev/null 2>&1 || fail_after_apply 'dns-direct не направлен через direct'
jq -e '([.routing.rules[]? | select((((.port // "") | tostring) == "53") and .outboundTag == "dns-out")] | length) == 1' "$ROUTING_FILE" >/dev/null 2>&1 || fail_after_apply 'dns-out routing rule отсутствует после apply'
grep -Eq '^[[:space:]]*proxy_dns="?off"?[[:space:]]*$' "$XKEEN_INIT" || fail_after_apply 'XKeen proxy_dns перестал быть off после apply'

VLESS_AFTER="$(jq -cS '[.outbounds[]? | select(.tag == "vless-reality")]' "$OUTBOUND_FILE" | sha256sum | awk '{print $1}')" || fail_after_apply 'не удалось проверить VLESS после apply'
[ "$VLESS_BEFORE" = "$VLESS_AFTER" ] || fail_after_apply 'VLESS credentials изменились после apply'
NON_VLESS_AFTER="$(jq -cS '[.outbounds[]? | select(.tag != "vless-reality" and .tag != "dns-out")]' "$OUTBOUND_FILE" | sha256sum | awk '{print $1}')" || fail_after_apply 'не удалось проверить non-VLESS outbounds после apply'
[ "$NON_VLESS_BEFORE" = "$NON_VLESS_AFTER" ] || fail_after_apply 'существующие non-VLESS outbounds изменились после apply'
INBOUND_AFTER="$(jq -cS . "$INBOUND_FILE" | sha256sum | awk '{print $1}')"
[ "$INBOUND_BEFORE" = "$INBOUND_AFTER" ] || fail_after_apply 'Xray inbounds изменились после apply'

info "Split DNS operation: SUCCESS ($MODE)"
info 'Xray validation: PASS'
info 'XKeen proxy_dns: off; Keenetic/ndnproxy DNS path preserved'
info 'DNS selectors: ordered first-match parity with domain routing'
info 'dns-vless transport: routed DoH/443 via vless-reality'
info 'dns-out/VLESS/non-VLESS/inbounds preservation: PASS'
info "Backup: $BACKUP_DIR"
exit 0
