#!/bin/sh

# FreeNet Doctor: read-only preflight for Keenetic/Netcraze + Entware.
# It does not install, restart, rewrite config, cron or firewall.

say() { printf '%s\n' "$*"; }
found() { command -v "$1" >/dev/null 2>&1; }
yesno() { if "$@" >/dev/null 2>&1; then printf 'YES'; else printf 'NO'; fi; }

LAN_IP="$(ip -4 addr show br0 2>/dev/null | sed -n 's/.*inet \([0-9.]*\)\/.*/\1/p' | head -n 1)"
ARCH_RAW="$(opkg print-architecture 2>/dev/null)"
case "$ARCH_RAW" in
  *aarch64*) ARCH=arm64-v8a ;;
  *mipsel*) ARCH=mips32le ;;
  *mips*) ARCH=mips32 ;;
  *) ARCH=unknown ;;
esac

HAS_OPT=NO; [ -d /opt ] && HAS_OPT=YES
HAS_OPKG=NO; found opkg && HAS_OPKG=YES
HAS_XKEEN=NO; [ -x /opt/sbin/xkeen ] && HAS_XKEEN=YES
HAS_XRAY=NO; [ -x /opt/sbin/xray ] && HAS_XRAY=YES
HAS_CONFIGS=NO; [ -d /opt/etc/xray/configs ] && HAS_CONFIGS=YES
HAS_XKEEN_UI=NO; [ -x /opt/sbin/xkeen-ui ] && HAS_XKEEN_UI=YES
HAS_FREENET=NO; [ -x /opt/sbin/freenet-ui ] && HAS_FREENET=YES
HAS_SUB=NO; [ -s /opt/etc/xray/blanc_subscription.url ] && HAS_SUB=YES
HAS_FILTER=NO; [ -s /opt/etc/xray/blanc_profile_filter.regex ] && HAS_FILTER=YES

say '========== FreeNet Doctor =========='
say "Shell: ${0:-sh}"
say "Kernel: $(uname -srm 2>/dev/null)"
say "LAN IPv4: ${LAN_IP:-NOT_FOUND}"
say "Architecture: $ARCH"
say "Entware /opt: $HAS_OPT"
say "opkg: $HAS_OPKG"
say "XKeen: $HAS_XKEEN"
say "Xray: $HAS_XRAY"
say "Xray configs: $HAS_CONFIGS"
say "XKeen UI: $HAS_XKEEN_UI"
say "FreeNet UI: $HAS_FREENET"
say "Subscription: $HAS_SUB"
say "Profile filter: $HAS_FILTER"

say ''
say '--- Tools ---'
TOOLS='curl sha256sum sed awk grep cmp mktemp crontab ip netstat nslookup jq tar'
MISSING=''
for T in $TOOLS; do
  if found "$T"; then
    say "$T: OK"
  else
    say "$T: MISSING"
    MISSING="$MISSING $T"
  fi
done

say ''
say '--- Storage ---'
df -h /opt 2>/dev/null || true

say ''
say '--- Processes ---'
ps w 2>/dev/null | grep -E '[x]ray run|[x]keen-ui|[f]reenet-ui' || true

say ''
say '--- Listeners ---'
netstat -lntp 2>/dev/null | grep -E ':222[[:space:]]|:1000[[:space:]]|:1001[[:space:]]' || true

say ''
say '--- Xray validation ---'
XRAY_VALID=UNKNOWN
if [ "$HAS_XRAY" = YES ] && [ "$HAS_CONFIGS" = YES ]; then
  if XRAY_LOCATION_ASSET=/opt/etc/xray/dat /opt/sbin/xray run -test -confdir /opt/etc/xray/configs >/tmp/freenet-doctor-xray.$$.log 2>&1; then
    XRAY_VALID=YES
    say 'Xray config: VALID'
  else
    XRAY_VALID=NO
    say 'Xray config: INVALID'
    tail -n 20 /tmp/freenet-doctor-xray.$$.log 2>/dev/null || true
  fi
  rm -f /tmp/freenet-doctor-xray.$$.log 2>/dev/null
else
  say 'Xray config: NOT_TESTED'
fi

say ''
say '--- Current VPN (safe fields only) ---'
if [ -x /opt/bin/vpn ]; then
  /opt/bin/vpn current 2>/dev/null || true
else
  say 'vpn helper: NOT_INSTALLED'
fi

say ''
say '--- Result ---'
MODE=UNKNOWN
NEXT=''
if [ "$HAS_OPT" != YES ] || [ "$HAS_OPKG" != YES ]; then
  MODE=NO_ENTWARE
  NEXT='Сначала установить Entware/OPKG. FreeNet ничего не менял.'
elif [ "$ARCH" = unknown ]; then
  MODE=UNSUPPORTED_ARCH
  NEXT='Архитектура не распознана. Mutation запрещена до разбора.'
elif [ "$HAS_XKEEN" = YES ] && [ "$HAS_XRAY" = YES ] && [ "$HAS_CONFIGS" = YES ] && [ "$XRAY_VALID" = YES ]; then
  MODE=READY_EXISTING_STACK
  NEXT='Можно использовать текущий FreeNet install/update path без переустановки XKeen/Xray.'
elif [ "$HAS_XKEEN" = NO ] || [ "$HAS_XRAY" = NO ]; then
  MODE=ENTWARE_ONLY
  NEXT='Нужен P1 bootstrap XKeen/Xray. Этот doctor ничего не устанавливает.'
else
  MODE=NEEDS_REVIEW
  NEXT='Есть частично установленный stack или невалидный Xray. Сначала read-only разбор.'
fi

say "MODE=$MODE"
say "NEXT=$NEXT"
if [ -n "$MISSING" ]; then
  say "MISSING_TOOLS=$MISSING"
fi
say 'MUTATION=NONE'
say '========== END =========='
exit 0
