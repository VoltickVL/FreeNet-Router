# FreeNet Router

FreeNet Router — переносимый слой управления VPN для Keenetic/Netcraze + Entware + XKeen/Xray.

Цель проекта: после подготовки USB/Entware не собирать VPN-схему вручную из SSH-команд. FreeNet должен довести роутер до рабочего VPN через один installer/browser wizard, а затем управлять VPN, подпиской, автоматизацией, обновлениями, backup/rollback и безопасным удалённым доступом.

Полный план развития: **GitHub Issue #5 — ROADMAP: FreeNet Router — от Entware до готового VPN через один мастер**.

## Текущий стабильный слой v0.2.2

Сейчас FreeNet умеет безопасно устанавливаться **поверх уже работающего Entware + XKeen + Xray**:

- direct release binaries ARM64/MIPSLE/MIPS;
- SHA-256 verification до mutation;
- timestamped backup + rollback;
- сохранение локальной VPN subscription URL;
- безопасная миграция собственного cron;
- FreeNet UI;
- выбор VPN-профиля;
- hardened endpoint updater;
- проверка Xray runtime и `dns-out`;
- запрет wildcard bind для FreeNet UI;
- loopback listener для CrazeDNS proxy;
- recovery UI после разрыва HTTP-соединения во время restart Xray/XKeen;
- read-only `doctor.sh` для HOME/WORK preflight.

## Перед установкой: FreeNet Doctor

На новом или рабочем роутере сначала выполните read-only preflight:

```sh
curl -fLsS https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/doctor.sh | sh
```

`doctor.sh` **ничего не устанавливает и не меняет**. Он проверяет Entware, архитектуру, инструменты, XKeen/Xray, Xray configs, процессы, порты, валидность Xray и возвращает режим:

```text
MODE=READY_EXISTING_STACK
```

— можно ставить текущий FreeNet поверх существующего XKeen/Xray.

```text
MODE=ENTWARE_ONLY
```

— Entware есть, но XKeen/Xray ещё нет. Для этого состояния разрабатывается P1 bootstrap из Roadmap #5; current installer не делает blind mutation.

Подробный порядок на рабочий роутер: [`docs/WORK-TOMORROW-RU.md`](docs/WORK-TOMORROW-RU.md).

## Быстрая установка

Сначала перейдите из Keenetic/Netcraze CLI в Entware shell:

```sh
exec /opt/usr/bin/sh
```

Если доступен отдельный Entware SSH, можно войти сразу, например:

```sh
ssh -p 222 root@<LAN-IP>
```

После `MODE=READY_EXISTING_STACK` FreeNet устанавливается одной командой:

```sh
curl -fLsS https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/install.sh | /opt/usr/bin/sh
```

Если router-local DNS не работает, используйте bootstrap DNS + `curl --resolve`:

```sh
H=raw.githubusercontent.com; IP="$(nslookup "$H" 77.88.8.8 2>/dev/null | awk '/^Name:/{s=1;next} s&&/^Address [0-9]+:/{if($3~/^[0-9]+\./){print $3;exit}}')"; [ -n "$IP" ] && curl -fLsS --resolve "$H:443:$IP" "https://$H/VoltickVL/FreeNet-Router/main/install.sh" | /opt/usr/bin/sh
```

`install.sh` — единственный installer/manager FreeNet. После установки тот же файл находится в `/opt/bin/freenet`.

## После установки

CLI:

```sh
freenet
```

Web UI:

```text
http://<LAN-IP-роутера>:1001/
```

FreeNet определяет LAN IPv4 интерфейса `br0`; конкретный домашний IP в installer не зашит.

## Что делает installer

Перед первой mutation `install.sh`:

1. проверяет Entware, XKeen/Xray и обязательные инструменты;
2. определяет архитектуру;
3. скачивает GitHub Release assets через bootstrap DNS (`77.88.8.8`, fallback `8.8.8.8`) и `curl --resolve`;
4. следует HTTPS redirect до release asset без зависимости от router-local DNS;
5. проверяет `SHA256SUMS` каждого устанавливаемого файла;
6. создаёт timestamped backup FreeNet-файлов, subscription URL и crontab;
7. сохраняет контрольные SHA Xray `02/03/04/05`;
8. только после этого меняет FreeNet-файлы и собственный cron-блок.

Если установка, runtime acceptance или проверка SHA завершается ошибкой, installer возвращает свои файлы и crontab из backup. После начала mutation `SIGINT`/`Ctrl-C`, `SIGHUP` и `SIGTERM` также запускают rollback.

## Что умеет FreeNet UI

Текущая UI-версия:

- показывает текущую страну/город и endpoint;
- показывает Xray, XKeen UI, `dns-out`, updater state;
- переключает Германия / Frankfurt / Extra;
- Польша / Warsaw / Extra;
- Финляндия / Helsinki / Extra;
- Нидерланды / Amsterdam / Extra;
- обновляет текущий endpoint из subscription;
- блокирует параллельные операции;
- восстанавливает UI state по `/api/status`, даже если браузерный POST оборвался во время restart VPN;
- работает локально на LAN и на loopback для CrazeDNS proxy, не используя wildcard `0.0.0.0:1001`.

Следующий provider layer будет получать **все доступные Extra profiles динамически**, а не ограничиваться четырьмя hardcoded кнопками.

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

## Автоматизация

Локальные несекретные настройки:

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

Существующие одиночные строки `/opt/sbin/xkeen -ug` и `/opt/bin/blanc_xkeen_update_outbounds.sh` мигрируются внутрь этого блока после backup. Остальные cron-задачи сохраняются.

## Локальные файлы

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

Subscription URL хранится только на конкретном роутере и не входит в repo/release.

## Безопасность

Публичный репозиторий не должен содержать:

- URL/токен VPN-подписки;
- VLESS UUID;
- Reality public/private keys;
- shortId;
- credential-bearing `04_outbounds.json`;
- пароли роутера/FreeNet;
- частные runtime dumps.

Web UI принимает только фиксированные действия. Произвольного shell execution через HTTP нет.

План безопасности из Roadmap #5: опциональный пароль FreeNet, hash-only storage, sessions, rate limit, `freenet reset-password`, CrazeDNS warning и LAN-only Entware SSH `root:222`.

## Надёжность updater

`blanc_xkeen_update_outbounds.sh`:

- сериализует запуски через lock;
- использует уникальный temp dir;
- умеет bootstrap DNS через внешний DNS + `curl --resolve`;
- меняет только `vless-reality`;
- сохраняет `direct`, `dns-out` и остальные non-VLESS outbounds;
- валидирует candidate полным `xray run -test` до live replace;
- применяет файл атомарно;
- не перезапускает Xray при семантически неизменившемся config;
- делает rolling backup;
- пытается восстановить предыдущий outbound при failed restart/validation;
- не печатает UUID/PBK/SID/URL subscription в штатный лог.

## Upstream / reference

FreeNet не копирует целиком сторонние инструкции и не хранит чужие секреты. Для развития bootstrap используются публичные upstream/reference:

- XKeen — upstream installer/release;
- XKeen UI — upstream release/UI;
- BlancVPN — официальный пользовательский flow и provider semantics.

BlancVPN будет первым fully-supported provider preset; Generic VLESS/Reality предусмотрен архитектурой Roadmap #5.

## Обновление

После выхода новой версии:

```sh
freenet update
```

или повторно выполните bootstrap-команду установки.

## Граница ответственности v0.2.2

v0.2.2 пока **не устанавливает XKeen/Xray на Entware-only роутер** и не переписывает произвольно Split DNS/firewall/routing. Это сознательная граница до P1 clean-room bootstrap.

После установки проверяется, что контрольные SHA Xray `02_dns.json`, `03_inbounds.json`, `04_outbounds.json` и `05_routing.json` installer-ом не изменились.
