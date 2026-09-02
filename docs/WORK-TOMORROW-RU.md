# FreeNet на рабочем роутере — штатная установка

Цель — повторить рабочую FreeNet-схему без ручного редактирования Xray и без копирования `04_outbounds.json` между роутерами.

## 1. Войти в Entware shell

Если на роутере уже работает Entware Dropbear на `222`:

```sh
ssh -p 222 root@<LAN-IP-роутера>
```

Если прямой Entware SSH не настроен, используйте штатный SSH Keenetic/Netcraze и затем:

```sh
exec /opt/bin/sh
```

## 2. Сначала только read-only preflight

```sh
curl -fLsS https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/doctor.sh | /opt/bin/sh
```

`doctor.sh` ничего не устанавливает, не перезапускает и не меняет cron/config/firewall.

### Если результат

```text
MODE=READY_EXISTING_STACK
```

На роутере уже есть валидные Entware + XKeen + Xray. Можно переходить к текущей установке FreeNet без переустановки XKeen/Xray.

### Если результат

```text
MODE=ENTWARE_ONLY
```

Есть Entware, но XKeen/Xray нет. Для такого состояния предназначен P1 bootstrap из Roadmap #5; current P0 installer не должен импровизировать установку XKeen.

### Если `NEEDS_REVIEW`, `NO_ENTWARE`, `UNSUPPORTED_ARCH`

Остановиться и разобрать факты до mutation.

## 3. Установка поверх уже готового XKeen/Xray

Только после `MODE=READY_EXISTING_STACK`:

```sh
curl -fLsS https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/install.sh | /opt/bin/sh
```

Если router-local DNS не работает, используется bootstrap-вариант с внешним DNS из README.

Начиная с v0.2.5 установщик сам выполняет отдельную transactional-фазу Split DNS до установки FreeNet app-файлов.

### Для legacy WORK-состояния

Подтверждённый WORK baseline до миграции:

- XKeen `proxy_dns=on`;
- firmware `ndnproxy` остаётся владельцем TCP/UDP `:53`;
- Xray работает с XKeen exclusion GID `11111`;
- `02_dns.json` может быть пустым;
- `03_inbounds.json` содержит существующие redirect/tproxy и не переписывается;
- `04_outbounds.json` может содержать `vless-reality`, `direct`, `block` без `dns-out`;
- `05_routing.json` может быть legacy routing без DNS rules.

Установщик не создаёт отдельный Xray listener на `:53` поверх `ndnproxy`. Вместо этого он строит Xray DNS-over-VLESS candidate в соответствии с моделью XKeen: `dns-direct`, `dns-vless`, `dns-out` и DNS routing rules, сохраняя существующий VLESS и все non-VLESS outbounds.

Перед live mutation выполняются:

1. detection текущего DNS state;
2. построение candidate в отдельном каталоге;
3. проверка сохранности существующих non-VLESS outbounds;
4. полный `xray run -test -confdir <candidate>`;
5. backup `02/03/04/05` в `/opt/backups/freenet-dns-migrate-*`.

Только после PASS candidate применяется атомарно и выполняется `xkeen -restart`.

При post-apply ошибке скрипт возвращает все четыре Xray config из backup и повторно поднимает XKeen/Xray. `PRIMARY ERROR` и `ROLLBACK ERROR/STATE` выводятся раздельно.

### Repair существующего HOME/MOM Split DNS

Если Split DNS уже настроен, но после ручного копирования чужого `04_outbounds.json` пропал только `dns-out`, installer не перестраивает DNS/routing: он добавляет `dns-out` обратно, сохраняя существующие `02/03/05`.

## 4. FreeNet app-этап

Только после успешной DNS-фазы установщик:

- скачивает direct GitHub Release assets;
- сверяет SHA-256;
- делает отдельный app backup;
- сохраняет существующую subscription URL;
- ставит FreeNet UI/manager/updater;
- мигрирует legacy cron в управляемый `# BEGIN FREENET` block;
- для новой установки обновляет endpoint subscription каждые 15 минут; unchanged candidate не должен перезапускать Xray;
- валидирует Xray и FreeNet UI;
- проверяет, что после завершённой DNS-фазы app-этап сам больше не менял `02/03/04/05`;
- при app-ошибке выполняет app rollback.

## 5. Acceptance после установки

Нужно подтвердить:

```text
Split DNS baseline: OK
FreeNet UI health/API: OK
LAN-only listener: OK
Xray config не изменён app-этапом installer-а: OK
```

После этого:

1. открыть `http://<LAN-IP>:1001/`;
2. проверить текущий профиль и endpoint;
3. сделать одно controlled переключение страны;
4. убедиться, что UI вернулся из busy-state;
5. проверить внешний VPN IP;
6. проверить клиентский DNS leak test;
7. только после этого считать WORK установленным.

## 6. Что не делать

- не переустанавливать XKeen/Xray, если `doctor` показывает уже рабочий stack;
- не открывать `root:222`, FreeNet `1001` или XKeen UI `1000` напрямую в WAN;
- не копировать `04_outbounds.json` целиком между WORK/HOME/MOM;
- не переносить subscription URL через GitHub/чат;
- не делать повторный install после FAIL без read-only проверки rollback/current state.

## Roadmap

Полный путь `Entware-only -> XKeen/Xray/XKeen UI -> browser setup -> provider -> routing -> FreeNet` ведётся в GitHub Issue #5.
