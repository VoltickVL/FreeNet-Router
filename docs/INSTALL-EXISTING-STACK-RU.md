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

## Контракт совместимости при обновлении существующей установки

Веб-обновление FreeNet не является переустановкой XKeen или Xray. Оно должно заменять **только** файлы FreeNet. Исправность старого Xray при `run -test` означает валидность конфига, **не** совместимость старого XKeen с новым механизмом переключения VPN.

Перед заменой FreeNet-owned файлов update-helper выполняет **read-only preflight**:

- присутствуют установленный XKeen, Xray, конфигурации и `S99xkeen`;
- поддержка foreground-старта XKeen объявлена; legacy `pidof xray` без неё — `LEGACY_PIDOF`, `READY=no`;
- нет активного mutation-lock и конфликтующих unmanaged cron-команд, которые отдельно изменяют Xray;
- не используется проверка «Xray online» как условие обновления: намеренно отключённый Xray допускается.

Неизвестный/частичный стек — **STOP до изменения файлов**. Не делаем автоматический downgrade Xray, замену XKeen, удаление cron или «ремонт» фильтров. Только наличие строкового foreground-маркера не доказывает фактическую работоспособность старта: после разработки безопасного runtime-manager нужна отдельная реальная приёмка.

При штатном обновлении FreeNet делает защищённый backup собственных компонентов и сравнивает хеши конфигураций Xray, бинарников XKeen/Xray, init, netfilter, подписки, профиля и cron **до** и **после**. На обнаруженном расхождении не заявляет SUCCESS и пытается вернуть FreeNet-owned файлы; сторонние Xray-конфиги и сервисы не переписывает.

**Важно при переходе со старых версий:** защита действует только в новой версии update-helper; существующие старые updater'ы не приобретают её автоматически. Поэтому до интеграционной приёмки новой версии **не запускать обновления действующих рабочих роутеров**. Релизный workflow и успешный CI сами по себе не доказывают совместимость с конкретной установкой. После P0 транзакционного исправления требуется контролируемая эксплуатационная проверка на переносном Giga, и только затем выпуск/развёртывание для остальных.

## После bootstrap

Открыть:

```text
http://<LAN-IP>:1001/
```

Дальше subscription/VPN/DNS/routing/Automation управляются через Browser Setup/Control Center.

При любой операции rollback result является отдельным фактом. `FAILED/UNKNOWN` = STOP.
