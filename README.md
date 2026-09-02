# FreeNet Router

FreeNet Router — переносимый слой управления VPN для Keenetic/Netcraze с Entware, XKeen и Xray.

Цель проекта: после ручной подготовки USB + Entware не собирать VPN-схему вручную из SSH-команд. Пользователь запускает один bootstrap, затем завершает настройку в браузере. FreeNet проверяет окружение, устанавливает или сохраняет XKeen/Xray/XKeen UI, ставит собственный Control Center, безопасно применяет VPN-профиль и ISP/DNS, а финальный шаг включает автозапуск только после полного acceptance.

Полный план развития: **GitHub Issue #5 — ROADMAP: FreeNet Router — от Entware до готового VPN через один мастер**.

## Текущий release: v0.2.6

`v0.2.6` — первый release-кандидат для полного clean-room сценария:

```text
USB + Entware/OPKG
        ↓
read-only doctor
        ↓
один bootstrap.sh
        ↓
XKeen + Xray + XKeen UI + FreeNet
        ↓
Browser Setup
        ↓
subscription → Extra VPN → ISP/DNS
        ↓
Проверить готовность → Завершить настройку
        ↓
reboot/autostart acceptance
```

Что уже реализовано в `v0.2.6`:

- ARM64 / MIPS32LE / MIPS32 release-бинарники и `SHA256SUMS`;
- pinned XKeen `2.0`, Xray `v26.7.28`, XKeen UI `v1.1.3`;
- классификация `ENTWARE_ONLY`, `READY_EXISTING_STACK`, `NEEDS_REVIEW`, `NO_ENTWARE`, `UNSUPPORTED_ARCH`;
- targeted установка недостающих Entware dependencies без глобального `opkg upgrade`;
- transactional установка core stack для чистого `ENTWARE_ONLY`;
- сохранение полного существующего XKeen/Xray stack без перестройки;
- STOP на частичном/непонятном stack вместо догадок;
- backup + rollback core и FreeNet app-фазы;
- одна точка входа `bootstrap.sh`;
- локальный FreeNet Control Center на LAN-порту `1001`;
- безопасное хранение subscription key-link только на роутере;
- fresh discovery всех активных BlancVPN `Extra` профилей;
- проверка Xray-кандидата до применения VPN-профиля;
- transactional применение выбранного `vless-reality`;
- ISP/DNS `plan → подтверждение → apply`;
- clean-router сценарий Ростелеком с transactional Split DNS;
- финальный `plan → apply`: `SETUP_COMPLETE=yes`, `xkeen -auto on`, управляемый cron и rollback config/cron/autostart;
- русские пользовательские статусы основной ошибки и отката;
- запрет слепого повтора операции после потери связи.

Важно: **код и release готовы, но clean-room runtime acceptance на новом Keenetic Giga ещё не закрыт**. CI и GitHub Release не заменяют проверку реального роутера после reboot.

## Базовые правила безопасности

FreeNet придерживается следующих ограничений:

- сначала read-only факты, потом mutation;
- release/upstream assets проверяются по SHA-256 до установки;
- subscription URL, UUID, Reality keys и пароли не выводятся в GitHub/API/UI;
- существующий полный XKeen/Xray stack сохраняется;
- частичный stack = STOP/`NEEDS_REVIEW`;
- нет глобального `opkg upgrade`;
- FreeNet UI не открывается wildcard-bind на WAN;
- ISP/DNS/VPN изменения выполняются только через allowlisted операции с предварительным plan;
- PRIMARY ERROR и состояние rollback не смешиваются;
- public health не считается доказательством корректного rollback;
- при неизвестном состоянии после ошибки нельзя повторять mutation вслепую.

## Перед установкой: read-only FreeNet Doctor

На новом или существующем роутере сначала выполните read-only preflight:

```sh
curl -fLsS https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/doctor.sh | /opt/usr/bin/sh
```

`doctor.sh` ничего не устанавливает и не меняет. Он проверяет Entware, архитектуру, инструменты, XKeen/Xray, Xray configs, процессы, порты и валидность Xray.

Возможные режимы:

```text
MODE=ENTWARE_ONLY
```

Entware готов, XKeen/Xray ещё нет. Это штатный вход для нового clean-room роутера; `bootstrap.sh` умеет установить pinned core stack автоматически.

```text
MODE=READY_EXISTING_STACK
```

Полный существующий XKeen/Xray stack найден. `bootstrap.sh` сохраняет его и устанавливает FreeNet поверх существующего core.

```text
MODE=NEEDS_REVIEW
```

Найден частичный или противоречивый stack. Установка должна остановиться — не достраивайте его вручную вперемешку с FreeNet.

Также возможны `NO_ENTWARE` и `UNSUPPORTED_ARCH`.

## Быстрый старт

### 1. Подготовьте USB + Entware

Используйте официальную инструкцию для конкретной модели Keenetic/Netcraze и её архитектуры. После установки Entware должен существовать `/opt`, а `opkg` должен работать.

### 2. Войдите в Entware shell

Из штатного SSH/CLI роутера:

```sh
exec /opt/usr/bin/sh
```

Если настроен отдельный Entware SSH, можно войти непосредственно в него.

### 3. Выполните read-only doctor

```sh
curl -fLsS https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/doctor.sh | /opt/usr/bin/sh
```

Продолжайте только для ожидаемого `ENTWARE_ONLY` или подтверждённого `READY_EXISTING_STACK`.

### 4. Запустите FreeNet bootstrap

Для обычной установки текущего опубликованного release:

```sh
curl -fLsS https://github.com/VoltickVL/FreeNet-Router/releases/latest/download/bootstrap.sh | /opt/usr/bin/sh
```

Для **clean-room acceptance v0.2.6** release и все внутренние assets должны быть закреплены на одной версии:

```sh
RELEASE=v0.2.6
curl -fLsS "https://github.com/VoltickVL/FreeNet-Router/releases/download/$RELEASE/bootstrap.sh" | FREENET_RELEASE_BASE="https://github.com/VoltickVL/FreeNet-Router/releases/download/$RELEASE" /opt/usr/bin/sh
```

Это исключает случайный переход на более новый `latest` во время контрольного clean-room теста.

`bootstrap.sh` сам:

1. проверяет Entware и архитектуру;
2. скачивает `SHA256SUMS` и release assets;
3. проверяет SHA-256;
4. выполняет read-only core plan;
5. для `ENTWARE_ONLY` ставит pinned XKeen/Xray/XKeen UI transactionally;
6. для `READY_EXISTING_STACK` сохраняет существующий core;
7. делает backup FreeNet app-файлов и cron;
8. ставит FreeNet UI/manager и transactional helpers;
9. проверяет, что app-фаза не переписала Xray JSON;
10. запускает FreeNet UI и печатает `PANEL=http://<LAN-IP>:1001/`.

## Browser Setup

Откройте адрес, который напечатал bootstrap:

```text
http://<LAN-IP-роутера>:1001/
```

Для BlancVPN текущий штатный сценарий:

1. вставить HTTPS key-link subscription и сохранить;
2. обновить список `Extra` профилей;
3. выбрать нужный профиль;
4. дождаться проверки Xray-кандидата;
5. явно применить выбранный VPN-профиль;
6. выбрать фактический интернет-провайдер и DNS mode;
7. сохранить сетевой профиль;
8. нажать **«Проверить план и состояние»**;
9. применить ISP/DNS только если plan разрешает mutation;
10. нажать **«Проверить готовность»**;
11. убедиться, что subscription, preferred profile, `vless-reality`, `dns-out`, Xray и ISP/DNS приняты;
12. нажать **«Завершить настройку»**.

Финальный apply:

- повторяет fresh read-only plan;
- включает XKeen autostart только штатной командой `xkeen -auto on`;
- пишет `SETUP_COMPLETE=yes`;
- пересобирает только управляемый блок `# BEGIN FREENET ... # END FREENET`;
- сохраняет чужие cron-задачи;
- при ошибке возвращает FreeNet config, cron и исходное состояние XKeen autostart.

### ISP presets

UI содержит отдельные ISP preset IDs. На текущем этапе автоматический clean-router runtime apply подтверждён для **Ростелеком**. Владлинк / АльянсТелеком / Подряд остаются отдельными профилями, но автоматическая mutation для них должна быть заблокирована до собственного runtime acceptance.

Если UI сообщает, что preset не поддержан или plan не готов, это нормальный safety gate — не обходите его ручным редактированием Xray.

## Что проверять после завершения мастера

Наличие зелёной страницы ещё не означает полный acceptance. Для clean-room нужно подтвердить:

- `SETUP_COMPLETE=yes`;
- XKeen autostart `on`;
- Xray config validation PASS;
- Xray process работает;
- `dns-out` присутствует;
- ровно один выбранный `vless-reality` активен;
- ISP/DNS соответствует выбранному preset;
- FreeNet UI/API доступны на LAN;
- выбранный VPN endpoint фактически используется;
- внешний IP соответствует ожидаемому VPN-региону;
- DNS test не показывает непредусмотренную утечку;
- после **реального reboot** XKeen/Xray/FreeNet поднимаются автоматически;
- после reboot VPN/DNS остаются рабочими.

До reboot acceptance чистая установка не считается полностью принятой.

## FreeNet UI

В `v0.2.6` Control Center умеет:

- показывать текущий VPN-профиль и endpoint;
- показывать Xray, XKeen UI, `dns-out`, updater state;
- безопасно хранить/заменять subscription key-link;
- динамически показывать все активные BlancVPN `Extra` профили;
- проверять VPN-кандидат до apply;
- transactional применять выбранный VPN-профиль;
- сохранять ISP/DNS выбор;
- получать read-only expected delta;
- применять поддержанный ISP/DNS preset через подтверждённую операцию;
- выполнять финальную проверку готовности;
- завершать setup transactionally;
- показывать основную ошибку и состояние отката раздельно;
- восстанавливать статус после разрыва HTTP во время VPN restart.

Legacy быстрые кнопки стран пока сохраняются; дальнейшая дорожная карта — latency/stability, preferred/fallback, automatic failover, routing UI, automation/system/backup и auth.

## Автоматизация

Локальные несекретные настройки:

```text
/opt/etc/freenet/freenet.conf
```

FreeNet управляет только своим cron-блоком:

```text
# BEGIN FREENET
...
# END FREENET
```

На setup-first установке endpoint refresh не должен активироваться до полного acceptance. Финальный completion helper сохраняет это ограничение и активирует только разрешённую настройками автоматизацию.

## Основные локальные файлы

```text
/opt/sbin/freenet-ui
/opt/etc/init.d/S99freenet-ui
/opt/bin/freenet
/opt/bin/vpn
/opt/bin/blanc_xkeen_update_outbounds.sh
/opt/lib/freenet/bootstrap_entware.sh
/opt/lib/freenet/migrate_split_dns.sh
/opt/lib/freenet/apply_network_profile.sh
/opt/lib/freenet/apply_provider_profile.sh
/opt/lib/freenet/finalize_setup.sh
/opt/etc/freenet/freenet.conf
/opt/etc/freenet/upstream-pins.env
```

Subscription key-link хранится локально отдельно и не должен попадать в GitHub, документацию или диагностические ответы.

## Recovery / rollback

- Core bootstrap и FreeNet app-фаза имеют отдельные backup/rollback границы.
- Provider apply откатывает изменяемые provider-файлы при post-apply ошибке.
- Split DNS migration откатывает Xray config при post-apply ошибке.
- Finalize apply откатывает FreeNet config, cron и XKeen autostart.
- `rollback FAILED/UNKNOWN` означает STOP: не повторяйте mutation до фактической проверки runtime.

Подробно: [`docs/RECOVERY-RU.md`](docs/RECOVERY-RU.md).

## Документация

- [`docs/INSTALL-FROM-SCRATCH-RU.md`](docs/INSTALL-FROM-SCRATCH-RU.md) — clean-room установка USB/Entware → FreeNet;
- [`docs/ARCHITECTURE-RU.md`](docs/ARCHITECTURE-RU.md) — границы владения Entware/XKeen/Xray/FreeNet;
- [`docs/providers/BLANCVPN-RU.md`](docs/providers/BLANCVPN-RU.md) — слой BlancVPN без секретов;
- [`docs/RECOVERY-RU.md`](docs/RECOVERY-RU.md) — recovery/rollback;
- [`docs/UPSTREAM-PINS-RU.md`](docs/UPSTREAM-PINS-RU.md) — pinned upstream policy;
- GitHub Issue #5 — актуальная дорожная карта;
- GitHub Issue #7 — русскоязычный журнал исполнения.

## Clean-room правило

Новый Keenetic Giga для acceptance должен начинать с собственного USB/Entware состояния. Нельзя:

- копировать HOME `/opt`;
- копировать HOME `04_outbounds.json`;
- переносить HOME secrets через GitHub/чат;
- вручную достраивать компоненты после `NEEDS_REVIEW`;
- обходить заблокированный ISP/DNS plan;
- считать CI или открывшуюся страницу доказательством reboot/autostart acceptance.

Любой недостающий prerequisite или ошибочный автоматический шаг фиксируется как дефект продукта/документации, а не компенсируется ручным «допиливанием» роутера.
