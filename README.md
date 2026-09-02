# FreeNet Router

FreeNet Router — система управления VPN и сетевой конфигурацией для роутеров Keenetic/Netcraze с Entware, XKeen и Xray.

Проект нужен для того, чтобы не собирать и не обслуживать VPN-схему вручную через набор SSH-команд. FreeNet проверяет окружение, устанавливает или сохраняет существующий стек, управляет VPN-профилями и DNS, предоставляет браузерный Control Center и выполняет изменения с проверкой и откатом.

## Основной сценарий

```text
Keenetic / Netcraze
        ↓
USB + Entware
        ↓
read-only doctor
        ↓
FreeNet bootstrap
        ↓
XKeen + Xray + XKeen UI + FreeNet
        ↓
Browser Setup / Control Center
        ↓
VPN + ISP/DNS + acceptance
```

FreeNet поддерживает два основных варианта:

- **Новая установка** — Entware уже подготовлен, FreeNet устанавливает необходимый стек и передаёт настройку в браузер.
- **Действующий роутер** — существующие XKeen/Xray сохраняются, а FreeNet устанавливается поверх рабочего стека без переписывания VPN credentials и конфигурации «с нуля».

## Что делает FreeNet

- read-only диагностика роутера перед изменениями;
- установка и проверка необходимых компонентов;
- проверка release/upstream-артефактов по SHA-256;
- локальный FreeNet Control Center;
- хранение VPN subscription только на роутере;
- выбор и применение VPN-профиля с Xray validation;
- отдельный выбор интернет-провайдера и DNS mode;
- штатный DNS Keenetic или явный Split DNS через XKeen/Xray;
- backup, transactional apply и rollback;
- раздельная фиксация основной ошибки и состояния отката;
- управление собственным блоком автоматизации без перезаписи чужих cron-задач;
- финальная проверка настройки и автозапуска.

## Архитектура

FreeNet не заменяет KeeneticOS, XKeen или Xray. Он координирует их и отвечает за безопасный сценарий установки, настройки и эксплуатации.

```text
KeeneticOS
 ├─ сеть, DHCP, штатный DNS, firewall
 └─ Entware (/opt)
     ├─ XKeen
     ├─ Xray
     ├─ XKeen UI
     └─ FreeNet
         ├─ Control Center
         ├─ VPN/provider layer
         ├─ ISP/DNS controller
         ├─ bootstrap / finalize
         └─ backup / rollback / diagnostics
```

## Безопасность

Основные правила проекта:

- сначала read-only факты, затем mutation;
- существующий рабочий стек не перестраивается без необходимости;
- частичное или противоречивое состояние означает STOP, а не попытку «доделать» его догадками;
- subscription URL, UUID, Reality keys, shortId и пароли не публикуются в GitHub и не возвращаются через публичные API;
- изменения VPN/DNS выполняются только через контролируемый plan/apply;
- rollback проверяется отдельно от основной операции;
- CI и успешный release не заменяют runtime acceptance на реальном роутере;
- после неизвестного или неуспешного rollback запрещён blind retry.

## Быстрый старт

После подготовки USB и Entware войдите в Entware shell:

```sh
exec /opt/usr/bin/sh
```

Read-only диагностика:

```sh
curl -fLsS https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/doctor.sh | /opt/usr/bin/sh
```

Установка из текущего опубликованного release:

```sh
curl -fLsS https://github.com/VoltickVL/FreeNet-Router/releases/latest/download/bootstrap.sh | /opt/usr/bin/sh
```

После bootstrap адрес FreeNet Control Center выводится в консоль. По умолчанию интерфейс работает в LAN на порту `1001`.

## Документация

- [`docs/INSTALL-FROM-SCRATCH-RU.md`](docs/INSTALL-FROM-SCRATCH-RU.md) — установка с чистого Entware;
- [`docs/ARCHITECTURE-RU.md`](docs/ARCHITECTURE-RU.md) — архитектура и границы ответственности;
- [`docs/RECOVERY-RU.md`](docs/RECOVERY-RU.md) — восстановление и rollback;
- [`docs/UPSTREAM-PINS-RU.md`](docs/UPSTREAM-PINS-RU.md) — политика upstream-артефактов и SHA-256;
- [`docs/providers/BLANCVPN-RU.md`](docs/providers/BLANCVPN-RU.md) — интеграция VPN-провайдера.

## Развитие проекта

Проект ведётся через два постоянных реестра:

- [Дорожная карта — Issue #5](https://github.com/VoltickVL/FreeNet-Router/issues/5) — текущее состояние, приоритеты и следующие этапы;
- [Журнал изменений — Issue #7](https://github.com/VoltickVL/FreeNet-Router/issues/7) — PR, CI, releases, runtime acceptance, ошибки и rollback.

Актуальные сборки находятся в [GitHub Releases](https://github.com/VoltickVL/FreeNet-Router/releases). Отдельные Issues открываются только для текущей конкретной execution-задачи, а не как параллельный список всей дорожной карты.
