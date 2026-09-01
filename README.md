# FreeNet Router

FreeNet Router — переносимый надстроечный слой для Keenetic/Netcraze + Entware + XKeen/Xray.

Цель проекта: после базовой установки Entware и XKeen не собирать домашнюю VPN-схему вручную из десятков SSH-команд. FreeNet ставит локальную веб-панель, управление странами VPN, безопасное обновление endpoint из подписки и управляемую автоматизацию cron.

## Быстрая установка

После публикации первого GitHub Release установка или переустановка выполняется одной командой:

```sh
curl -Ls https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/setup.sh | sh -s -- install
```

После установки доступно локальное меню:

```sh
freenet
```

и веб-панель:

```text
http://<LAN-IP-роутера>:1001/
```

Установщик сам определяет LAN IPv4 интерфейса `br0`; адрес `192.168.50.1` не зашит в установленный init-файл.

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
- автоматический rollback installer-файлов и cron при failed install;
- SHA-256 проверка всех release assets до установки;
- прямые release binaries, без ZIP-архивов GitHub Actions.

## Поддерживаемые архитектуры

Первый релиз собирает отдельные статические binaries:

- `arm64-v8a`;
- `mips32le`;
- `mips32`.

Архитектура определяется через `opkg print-architecture`.

## Что должно быть на роутере заранее

Версия `v0.1.0` сознательно не переустанавливает и не переписывает базовый XKeen/Xray runtime. Перед установкой должны уже существовать:

- Entware в `/opt`;
- `opkg`;
- `/opt/sbin/xkeen`;
- `/opt/sbin/xray`;
- `/opt/etc/xray/configs`;
- рабочий Xray config;
- `04_outbounds.json` с единственным `vless-reality` и единственным `dns-out` для работы hardened updater.

Полностью автоматический bootstrap «чистый Keenetic → Entware/XKeen/Xray/FreeNet» планируется отдельным слоем. Текущая версия не делает опасных догадок о существующем routing/DNS.

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

FreeNet управляет только собственным cron-блоком между маркерами:

```text
# BEGIN FREENET
...
# END FREENET
```

Чужие cron-задания установщик не удаляет.

## Безопасность

Публичный репозиторий специально не содержит:

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
- умеет bootstrap DNS через внешний DNS + `curl --resolve`, если локальный resolver не работает;
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
2. проверяет публичную поверхность на известные секреты/частные endpoint;
3. собирает binaries под ARM64/MIPSLE/MIPS;
4. формирует `SHA256SUMS`;
5. публикует прямые assets в GitHub Release.

Installer всегда скачивает `releases/latest/download/*` и сверяет SHA-256 перед mutation.

## Обновление

После выхода новой версии:

```sh
freenet update
```

или повторно той же одной командой:

```sh
curl -Ls https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/setup.sh | sh -s -- update
```

## Граница ответственности v0.1.0

FreeNet v0.1.0 — control/update layer поверх уже работающего XKeen/Xray. Он не переписывает произвольно Split DNS, firewall или routing. Следующий этап проекта — перенос validated DNS/routing-профиля в отдельный безопасный bootstrap/configuration layer для новых роутеров.
