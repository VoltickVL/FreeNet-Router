# Установка FreeNet поверх существующего XKeen/Xray

Главная пошаговая инструкция находится в корневом [README.md](../README.md). Здесь зафиксирован existing-stack contract.

## Предпосылка

На роутере уже есть:

- Entware/OPKG;
- полный рабочий XKeen/Xray stack;
- Xray configs, которые проходят validation.

Ручной preflight через SSH не требуется: bootstrap должен сам определить состояние.

## Одна команда

```sh
/opt/bin/opkg update && /opt/bin/opkg install ca-bundle curl && /opt/bin/curl -fLsS https://github.com/VoltickVL/FreeNet-Router/releases/latest/download/bootstrap.sh -o /tmp/freenet-bootstrap.sh && /opt/bin/sh /tmp/freenet-bootstrap.sh
```

Ожидаемая классификация полного существующего стека:

```text
MODE=READY_EXISTING_STACK
```

В этом режиме FreeNet не переустанавливает working XKeen/Xray core. Недостающие userland tools для самого FreeNet могут быть доставлены targeted `opkg install`.

## Что должно сохраниться

FreeNet не должен без необходимости менять:

- VLESS UUID / Reality credentials;
- subscription secret;
- рабочие Xray configs;
- выбранный VPN;
- unrelated cron;
- existing core binaries.

App-фаза делает snapshot Xray configs и проверяет, что установка самого Control Center их не переписала.

## Partial state

Если состояние неполное или противоречивое:

```text
MODE=NEEDS_REVIEW
```

Установка останавливается до mutation. Не надо вручную «доустанавливать Xray» только для обхода STOP.

## После bootstrap

Открыть:

```text
http://<LAN-IP>:1001/
```

Дальше subscription/VPN/DNS/routing/Automation управляются через Browser Setup/Control Center.

При любой операции rollback result является отдельным фактом. `FAILED/UNKNOWN` = STOP.
