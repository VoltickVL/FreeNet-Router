# FreeNet Router

FreeNet Router — переносимый слой управления VPN для Keenetic/Netcraze + Entware + XKeen/Xray.

Цель проекта: после базовой установки Entware и XKeen не собирать VPN-схему вручную из SSH-команд. FreeNet устанавливает локальную веб-панель, выбор страны VPN, безопасное обновление endpoint из подписки и управляемую автоматизацию cron.

## Быстрая установка

Сначала перейдите из Keenetic/Netcraze CLI в Entware shell:

```sh
exec /opt/usr/bin/sh
```

После этого FreeNet устанавливается **одной командой**. Основной вариант специально не зависит от локального DNS роутера: адрес `raw.githubusercontent.com` разрешается через внешний DNS и передаётся в `curl --resolve`.

```sh
H=raw.githubusercontent.com; IP="$(nslookup "$H" 77.88.8.8 2>/dev/null | awk '/^Name:/{s=1;next} s&&/^Address [0-9]+:/{if($3~/^[0-9]+\./){print $3;exit}}')"; [ -n "$IP" ] && curl -fLsS --resolve "$H:443:$IP" "https://$H/VoltickVL/FreeNet-Router/main/install.sh" | /opt/usr/bin/sh
```

Если локальный DNS роутера заведомо исправен, можно использовать короткий вариант:

```sh
curl -fLsS https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/install.sh | /opt/usr/bin/sh
```

`install.sh` — единственный installer/manager FreeNet. Он не скачивает второй setup-скрипт. Тот же файл после установки становится `/opt/bin/freenet`.

После установки доступны:

```sh
freenet
```

и веб-панель:

```text
http://<LAN-IP-роутера>:1001/
```

Установщик сам определяет LAN IPv4 интерфейса `br0`; конкретный домашний IP в код не зашит.

## Что делает installer

Перед первой mutation `install.sh`:

1. проверяет Entware, XKeen/Xray и обязательные инструменты;
2. определяет архитектуру роутера;
3. скачивает GitHub Release assets через внешний bootstrap DNS (`77.88.8.8`, fallback `8.8.8.8`) и `curl --resolve`;
4. вручную следует HTTPS redirect до release asset, поэтому не зависит от router-local DNS на каждом redirect-host;
5. проверяет `SHA256SUMS` для каждого устанавливаемого файла;
6. создаёт timestamped backup текущих FreeNet-файлов, subscription URL и crontab;
7. сохраняет контрольные SHA Xray `02/03/04/05`;
8. только после этого меняет файлы и cron.

Если установка, runtime acceptance или проверка SHA завершается ошибкой, installer возвращает свои файлы и crontab из backup. После начала mutation `SIGINT`/`Ctrl-C`, `SIGHUP` и `SIGTERM` также запускают rollback.

## Что умеет FreeNet

- локальная web UI без Python, Node.js, PHP и nginx;
- отдельный статический бинарник `freenet-ui`;
- ручной выбор VPN-профиля:
  - Германия / Frankfurt / Extra;
  - Польша / Warsaw / Extra;
  - Финляндия / Helsinki / Extra;
  - Нидерланды / Amsterdam / Extra;
- отображение текущего endpoint;
- статус Xray, XKeen UI, `dns-out` и updater lock;
- кнопка обновления текущего endpoint из VPN-подписки;
- защита от параллельных операций;
- snapshot + rollback при неуспешном переключении страны;
- автоматическое обновление endpoint/IP по cron;
- отдельное расписание `xkeen -ug`;
- изменение расписаний и порта через `freenet configure`;
- timestamped backup перед установкой/обновлением;
- SHA-256 проверка release assets до mutation;
- прямые release binaries, без ZIP-архивов GitHub Actions.

## Поддерживаемые архитектуры

Release собирает отдельные статические binaries:

- `arm64-v8a`;
- `mips32le`;
- `mips32`.

Архитектура определяется через `opkg print-architecture`.

## Что должно быть на роутере заранее

FreeNet v0.2.0 пока является control/update layer поверх уже работающего XKeen/Xray. Перед установкой должны существовать:

- Entware в `/opt`;
- `opkg`;
- `/opt/sbin/xkeen`;
- `/opt/sbin/xray`;
- `/opt/etc/xray/configs`;
- рабочий Xray config;
- `04_outbounds.json` с единственным `vless-reality` и единственным `dns-out` для hardened updater.

Полный bootstrap «чистый Keenetic → Entware → XKeen/Xray → FreeNet» будет отдельным слоем после проверки v0.2.0 на нескольких роутерах. Текущий installer сознательно не делает догадок о Split DNS, firewall и routing нового устройства.

## Локальные файлы после установки

```text
/opt/sbin/freenet-ui
/opt/etc/init.d/S99freenet-ui
/opt/bin/freenet
/opt/bin/vpn
/opt/bin/blanc_xkeen_update_outbounds.sh
/opt/etc/freenet/freenet.conf
/opt/etc/xray/blanc_subscription.url
/opt/etc/xray/blanc_profile_filter.regex
```

`/opt/etc/xray/blanc_subscription.url` хранится только на конкретном роутере и не входит в репозиторий или Release.

## Команды

Без аргументов открывается локальное меню:

```sh
freenet
```

Прямые команды:

```sh
freenet install
freenet update
freenet status
freenet configure
freenet uninstall
```

VPN helper:

```sh
vpn de
vpn pl
vpn fi
vpn nl
vpn current
vpn update
```

## Настройки

Несекретные локальные настройки находятся в:

```text
/opt/etc/freenet/freenet.conf
```

По умолчанию:

```sh
UI_PORT=1001
AUTO_ENDPOINT_UPDATE=yes
AUTO_ENDPOINT_CRON='50 6 * * *'
AUTO_XKEEN_GEODATA=yes
AUTO_XKEEN_GEODATA_CRON='30 6 * * *'
```

FreeNet управляет только собственным cron-блоком:

```text
# BEGIN FREENET
...
# END FREENET
```

При первой установке существующие одиночные строки `/opt/sbin/xkeen -ug` и `/opt/bin/blanc_xkeen_update_outbounds.sh` мигрируются внутрь этого блока. Остальные cron-задачи сохраняются. Миграция выполняется только после backup и входит в общий rollback installer-а.

## Безопасность

Публичный репозиторий не содержит:

- URL/токен VPN-подписки;
- VLESS UUID;
- Reality public/private keys;
- shortId;
- credential-bearing `04_outbounds.json`;
- пароли роутера;
- историю частных runtime-диагностик.

Web UI принимает только фиксированный allowlist действий `de`, `pl`, `fi`, `nl`, `update`. Произвольной shell-команды через HTTP нет.

Init-файл привязывает UI к LAN IPv4 роутера, а не к wildcard `0.0.0.0`.

## Надёжность updater

`blanc_xkeen_update_outbounds.sh`:

- сериализует запуски через lock;
- использует уникальный temp dir;
- умеет bootstrap DNS через внешний DNS + `curl --resolve`;
- меняет только объект `vless-reality`;
- сохраняет `direct`, `dns-out` и остальные non-VLESS outbounds;
- валидирует candidate полным `xray run -test` до live replace;
- применяет файл атомарно;
- не перезапускает Xray при семантически неизменившемся config;
- делает rolling backup;
- пытается автоматически восстановить предыдущий outbound при failed restart/validation;
- не печатает UUID/PBK/SID/URL подписки в штатный лог.

## Releases

Файл `VERSION` определяет версию релиза. Изменение `VERSION` в `main` запускает release pipeline, который:

1. выполняет Go tests/vet и shell syntax checks;
2. проверяет installer contract и публичную поверхность на известные секреты/частные endpoint;
3. собирает binaries под ARM64/MIPSLE/MIPS;
4. копирует тот же `install.sh` в release как установленный manager `freenet`;
5. формирует `SHA256SUMS`;
6. публикует прямые assets в GitHub Release.

Installer скачивает `releases/latest/download/*` и сверяет SHA-256 до изменения live-файлов.

## Обновление

После выхода новой версии:

```sh
freenet update
```

или повторно выполните ту же одну bootstrap-команду из раздела «Быстрая установка».

## Граница ответственности v0.2.0

FreeNet v0.2.0 не переписывает произвольно Split DNS, firewall или routing. После установки дополнительно проверяется, что контрольные SHA Xray `02_dns.json`, `03_inbounds.json`, `04_outbounds.json` и `05_routing.json` не изменились.
