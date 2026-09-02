#!/bin/sh

CONFIG_DIR="/opt/etc/xray/configs"
DNS_FILE="$CONFIG_DIR/02_dns.json"
INBOUND_FILE="$CONFIG_DIR/03_inbounds.json"
OUTBOUND_FILE="$CONFIG_DIR/04_outbounds.json"
ROUTING_FILE="$CONFIG_DIR/05_routing.json"
XKEEN_INIT="/opt/etc/init.d/S99xkeen"
XKEEN_BIN="/opt/sbin/xkeen"
XRAY_BIN="/opt/sbin/xray"
XRAY_ASSET_DIR="/opt/etc/xray/dat"
BACKUP_ROOT="/opt/backups"
TMP_DIR=""
BACKUP_DIR=""
APPLIED=0

say() { printf '%s\n' "$*"; }
info() { printf '[FreeNet DNS] %s\n' "$*"; }
err() { printf '[FreeNet DNS] ERROR: %s\n' "$*" >&2; }

cleanup() {
    [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null
}

trap cleanup 0 1 2 15

need_cmd() {
    command -v "$1" >/dev/null 2>&1 || {
        err "не найдена обязательная команда: $1"
        exit 1
    }
}

make_tmp() {
    TMP_DIR="$(mktemp -d /tmp/freenet-dns.XXXXXX 2>/dev/null)"
    if [ -z "$TMP_DIR" ] || [ ! -d "$TMP_DIR" ]; then
        TMP_DIR="/tmp/freenet-dns.$$"
        mkdir -p "$TMP_DIR" || {
            err "не удалось создать временный каталог"
            exit 1
        }
    fi
}

build_candidate() {
    SRC="$1"
    DST="$2"

    mkdir -p "$DST" || return 1

    [ -f "$SRC/03_inbounds.json" ] || return 1
    [ -f "$SRC/04_outbounds.json" ] || return 1
    [ -f "$SRC/05_routing.json" ] || return 1

    jq -e . "$SRC/04_outbounds.json" >/dev/null 2>&1 || return 1
    jq -e . "$SRC/05_routing.json" >/dev/null 2>&1 || return 1

    DIRECT_DOMAINS="$(jq -c '[.routing.rules[]? | select(.outboundTag == "direct") | .domain[]?] | unique' "$SRC/05_routing.json" 2>/dev/null)" || return 1
    [ -n "$DIRECT_DOMAINS" ] || DIRECT_DOMAINS='[]'

    jq -n --argjson domains "$DIRECT_DOMAINS" '
      {
        dns: {
          tag: "dns-vless",
          servers: [
            {
              address: "77.88.8.8",
              port: 53,
              domains: $domains,
              skipFallback: true,
              tag: "dns-direct"
            },
            {
              address: "8.8.8.8",
              port: 53,
              tag: "dns-vless"
            }
          ],
          queryStrategy: "UseIPv4"
        }
      }
    ' > "$DST/02_dns.json" || return 1

    cp -p "$SRC/03_inbounds.json" "$DST/03_inbounds.json" || return 1

    jq '
      if any(.outbounds[]?; .tag == "dns-out") then
        .
      else
        .outbounds += [{"protocol":"dns","tag":"dns-out"}]
      end
    ' "$SRC/04_outbounds.json" > "$DST/04_outbounds.json" || return 1

    jq '
      .routing.rules = (
        [
          {"type":"field","inboundTag":["dns-vless"],"outboundTag":"vless-reality"},
          {"type":"field","inboundTag":["dns-direct"],"outboundTag":"direct"},
          {"type":"field","port":53,"outboundTag":"dns-out"}
        ]
        +
        [
          .routing.rules[]?
          | select((.outboundTag // "") != "dns-out")
          | select(((.inboundTag // []) | index("dns-vless")) == null)
          | select(((.inboundTag // []) | index("dns-direct")) == null)
        ]
      )
    ' "$SRC/05_routing.json" > "$DST/05_routing.json" || return 1

    jq -e '.dns.tag == "dns-vless" and ([.dns.servers[]?.tag] | index("dns-direct") != null) and ([.dns.servers[]?.tag] | index("dns-vless") != null)' "$DST/02_dns.json" >/dev/null 2>&1 || return 1
    jq -e 'any(.outbounds[]?; .tag == "dns-out" and .protocol == "dns")' "$DST/04_outbounds.json" >/dev/null 2>&1 || return 1
    jq -e '(.routing.rules[0].inboundTag | index("dns-vless")) != null and .routing.rules[0].outboundTag == "vless-reality"' "$DST/05_routing.json" >/dev/null 2>&1 || return 1
    jq -e '(.routing.rules[1].inboundTag | index("dns-direct")) != null and .routing.rules[1].outboundTag == "direct"' "$DST/05_routing.json" >/dev/null 2>&1 || return 1
    jq -e '.routing.rules[2].port == 53 and .routing.rules[2].outboundTag == "dns-out"' "$DST/05_routing.json" >/dev/null 2>&1 || return 1

    return 0
}

if [ "${1:-}" = "--build-only" ]; then
    [ $# -eq 3 ] || {
        err "usage: $0 --build-only INPUT_CONFIG_DIR OUTPUT_CONFIG_DIR"
        exit 1
    }
    need_cmd jq
    build_candidate "$2" "$3" || {
        err "не удалось построить candidate Split DNS"
        exit 1
    }
    exit 0
fi

for C in jq cp mv grep awk sed mktemp sha256sum netstat pidof; do
    need_cmd "$C"
done

[ -x "$XKEEN_BIN" ] || { err "XKeen не найден: $XKEEN_BIN"; exit 1; }
[ -x "$XRAY_BIN" ] || { err "Xray не найден: $XRAY_BIN"; exit 1; }
[ -f "$XKEEN_INIT" ] || { err "init XKeen не найден: $XKEEN_INIT"; exit 1; }
[ -d "$CONFIG_DIR" ] || { err "Xray config dir не найден: $CONFIG_DIR"; exit 1; }
[ -d "$XRAY_ASSET_DIR" ] || { err "Xray asset dir не найден: $XRAY_ASSET_DIR"; exit 1; }

for F in "$DNS_FILE" "$INBOUND_FILE" "$OUTBOUND_FILE" "$ROUTING_FILE"; do
    [ -f "$F" ] || { err "не найден обязательный Xray config: $F"; exit 1; }
done

if jq -e '.dns.tag == "dns-vless" and ([.dns.servers[]?.tag] | index("dns-direct") != null)' "$DNS_FILE" >/dev/null 2>&1 \
   && jq -e 'any(.outbounds[]?; .tag == "dns-out" and .protocol == "dns")' "$OUTBOUND_FILE" >/dev/null 2>&1 \
   && jq -e 'any(.routing.rules[]?; .outboundTag == "dns-out")' "$ROUTING_FILE" >/dev/null 2>&1; then
    info "Split DNS baseline уже присутствует; migration не требуется."
    exit 0
fi

if ! grep -Eq '^[[:space:]]*proxy_dns="?on"?[[:space:]]*$' "$XKEEN_INIT"; then
    err "XKeen proxy_dns не включён; автоматическая миграция legacy state остановлена до отдельного preflight"
    exit 1
fi

PORT53="$(netstat -lnp 2>/dev/null | grep ':53[[:space:]]' || true)"
if printf '%s\n' "$PORT53" | grep -q '/xray'; then
    err "порт 53 уже занят Xray; legacy migration не применяется вслепую"
    exit 1
fi
if ! printf '%s\n' "$PORT53" | grep -q '/ndnproxy'; then
    err "не подтверждён firmware DNS owner ndnproxy на порту 53"
    exit 1
fi

XPID="$(pidof xray 2>/dev/null | awk '{print $1}')"
[ -n "$XPID" ] || { err "Xray process не запущен"; exit 1; }
XGID="$(awk '/^Gid:/ {print $2; exit}' "/proc/$XPID/status" 2>/dev/null)"
[ "$XGID" = "11111" ] || {
    err "Xray GID не равен XKeen exclusion GID 11111; migration остановлена"
    exit 1
}

make_tmp
CANDIDATE_DIR="$TMP_DIR/candidate"
mkdir -p "$CANDIDATE_DIR" || { err "не удалось создать candidate dir"; exit 1; }

for F in "$CONFIG_DIR"/*; do
    [ -f "$F" ] || continue
    cp -p "$F" "$CANDIDATE_DIR/" || { err "не удалось собрать candidate confdir"; exit 1; }
done

build_candidate "$CONFIG_DIR" "$CANDIDATE_DIR" || {
    err "не удалось построить Split DNS candidate"
    exit 1
}

XRAY_LOCATION_ASSET="$XRAY_ASSET_DIR" "$XRAY_BIN" run -test -confdir "$CANDIDATE_DIR" > "$TMP_DIR/xray-candidate.log" 2>&1 || {
    tail -n 40 "$TMP_DIR/xray-candidate.log" >&2 2>/dev/null
    err "candidate Xray config не прошёл validation; live state не изменён"
    exit 1
}

if jq -S . "$DNS_FILE" 2>/dev/null | cmp - "$CANDIDATE_DIR/02_dns.json" >/dev/null 2>&1 \
   && jq -S . "$OUTBOUND_FILE" 2>/dev/null | cmp - "$CANDIDATE_DIR/04_outbounds.json" >/dev/null 2>&1 \
   && jq -S . "$ROUTING_FILE" 2>/dev/null | cmp - "$CANDIDATE_DIR/05_routing.json" >/dev/null 2>&1; then
    info "Split DNS candidate семантически совпадает с live state."
    exit 0
fi

STAMP="$(date +%Y%m%d-%H%M%S)"
BACKUP_DIR="$BACKUP_ROOT/freenet-dns-migrate-$STAMP"
mkdir -p "$BACKUP_DIR" || { err "не удалось создать backup: $BACKUP_DIR"; exit 1; }

for NAME in 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json; do
    cp -p "$CONFIG_DIR/$NAME" "$BACKUP_DIR/$NAME" || {
        err "не удалось сохранить backup $NAME"
        exit 1
    }
done
sha256sum "$BACKUP_DIR"/*.json > "$BACKUP_DIR/SHA256SUMS.before" 2>/dev/null || true

rollback_live() {
    RB_OK=1
    for NAME in 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json; do
        cp -p "$BACKUP_DIR/$NAME" "$CONFIG_DIR/$NAME.rollback.$$" 2>/dev/null || RB_OK=0
        [ "$RB_OK" = "1" ] && mv -f "$CONFIG_DIR/$NAME.rollback.$$" "$CONFIG_DIR/$NAME" 2>/dev/null || RB_OK=0
    done

    "$XKEEN_BIN" -restart > "$TMP_DIR/xkeen-rollback.log" 2>&1 || RB_OK=0
    pidof xray >/dev/null 2>&1 || RB_OK=0
    XRAY_LOCATION_ASSET="$XRAY_ASSET_DIR" "$XRAY_BIN" run -test -confdir "$CONFIG_DIR" > "$TMP_DIR/xray-rollback.log" 2>&1 || RB_OK=0

    [ "$RB_OK" = "1" ]
}

fail_after_apply() {
    PRIMARY="$1"
    err "PRIMARY ERROR: $PRIMARY"
    if rollback_live; then
        err "ROLLBACK: SUCCESS; предыдущая Xray конфигурация восстановлена"
        exit 1
    fi
    err "ROLLBACK ERROR: FAILED/UNKNOWN; дальнейшая mutation запрещена до read-only inspection"
    exit 2
}

for NAME in 02_dns.json 04_outbounds.json 05_routing.json; do
    cp -p "$CANDIDATE_DIR/$NAME" "$CONFIG_DIR/$NAME.freenet.$$" || {
        rm -f "$CONFIG_DIR"/*.freenet.$$ 2>/dev/null
        err "не удалось stage $NAME; live state не изменён"
        exit 1
    }
done

mv -f "$CONFIG_DIR/02_dns.json.freenet.$$" "$DNS_FILE" || fail_after_apply "не удалось применить 02_dns.json"
APPLIED=1
mv -f "$CONFIG_DIR/04_outbounds.json.freenet.$$" "$OUTBOUND_FILE" || fail_after_apply "не удалось применить 04_outbounds.json"
mv -f "$CONFIG_DIR/05_routing.json.freenet.$$" "$ROUTING_FILE" || fail_after_apply "не удалось применить 05_routing.json"

"$XKEEN_BIN" -restart > "$TMP_DIR/xkeen-restart.log" 2>&1 || fail_after_apply "xkeen -restart завершился ошибкой"
pidof xray >/dev/null 2>&1 || fail_after_apply "Xray не запущен после migration restart"

XRAY_LOCATION_ASSET="$XRAY_ASSET_DIR" "$XRAY_BIN" run -test -confdir "$CONFIG_DIR" > "$TMP_DIR/xray-live.log" 2>&1 || fail_after_apply "live Xray config не проходит validation после apply"

jq -e 'any(.outbounds[]?; .tag == "dns-out" and .protocol == "dns")' "$OUTBOUND_FILE" >/dev/null 2>&1 || fail_after_apply "dns-out отсутствует после apply"
jq -e 'any(.routing.rules[]?; .outboundTag == "dns-out")' "$ROUTING_FILE" >/dev/null 2>&1 || fail_after_apply "dns-out routing rule отсутствует после apply"
jq -e '.dns.tag == "dns-vless" and ([.dns.servers[]?.tag] | index("dns-direct") != null)' "$DNS_FILE" >/dev/null 2>&1 || fail_after_apply "Split DNS config отсутствует после apply"

grep -Eq '^[[:space:]]*proxy_dns="?on"?[[:space:]]*$' "$XKEEN_INIT" || fail_after_apply "XKeen proxy_dns изменился после apply"

APPLIED=0
info "Split DNS migration: SUCCESS"
info "Xray validation: PASS"
info "dns-out/non-VLESS preservation: PASS"
info "Backup: $BACKUP_DIR"
info "Firmware DNS owner ndnproxy оставлен на порту 53; отдельный Xray listener :53 не создавался."
exit 0
