# FreeNet Router

FreeNet Router — система управления VPN, DNS и маршрутизацией для Keenetic/Netcraze с Entware, XKeen и Xray.

Цель проекта — убрать ручную сборку конфигурации через SSH. FreeNet сам классифицирует окружение, сохраняет рабочий существующий стек, устанавливает недостающие компоненты для поддерживаемого сценария, управляет VPN-профилями и сетевой политикой через браузерный Control Center, а изменения выполняет с проверкой и откатом.

## Основной сценарий

```text
Keenetic / Netcraze
        ↓
USB + Entware
        ↓
FreeNet bootstrap
        ↓
XKeen + Xray + XKeen UI + FreeNet
        ↓
Browser Setup / Control Center
        ↓
VPN + ISP/DNS + routing + acceptance
```

Поддерживаются два основных варианта:

- **Новая установка** — Entware уже подготовлен, FreeNet устанавливает поддерживаемый стек и передаёт дальнейшую настройку в браузер.
- **Существующий стек** — уже работающие XKeen/Xray сохраняются, FreeNet устанавливается поверх них без переписывания VPN credentials и конфигурации «с нуля».

## Что делает FreeNet

- безопасно классифицирует окружение перед изменениями;
- проверяет release/upstream-артефакты по SHA-256;
- предоставляет локальный Control Center;
- хранит VPN subscription только на роутере;
- применяет VPN-профиль с Xray validation и rollback;
- разделяет Refresh, Rotate и Failover;
- отдельно управляет интернет-провайдером и DNS mode;
- поддерживает штатный DNS роутера и явный Split DNS через XKeen/Xray;
- управляет собственным cron-блоком без удаления чужих заданий;
- проверяет финальную готовность и автозапуск.

## Профили интернет-провайдеров

Базовая routing policy определяется провайдером, а не названием физической площадки:

- **Владлинк** — YouTube → `DIRECT`;
- **АльянсТелеком** — YouTube → `DIRECT`;
- **Ростелеком** — YouTube → `VPN`;
- **Подряд** — отдельный профиль, правила требуют собственного подтверждения.

Отдельные сервисные исключения добавляются только после подтверждённого требования и acceptance.

## Безопасность

Основные правила проекта:

- сначала факты, затем изменение;
- существующий рабочий стек не перестраивается без необходимости;
- частичное или противоречивое состояние означает STOP, а не попытку «доделать» его догадками;
- subscription URL, UUID, Reality keys, shortId и пароли не публикуются в GitHub и не возвращаются через публичные API;
- изменения VPN/DNS/routing выполняются через контролируемый plan/apply;
- rollback проверяется отдельно от основной операции;
- CI и release не заменяют runtime acceptance;
- после неизвестного или неуспешного rollback запрещён blind retry.

## Быстрый старт

После подготовки USB и Entware войдите в Entware shell:

```sh
exec /opt/bin/sh
```

Установка текущего опубликованного релиза:

```sh
curl -fLsS https://github.com/VoltickVL/FreeNet-Router/releases/latest/download/bootstrap.sh | /opt/bin/sh
```

Bootstrap сам определяет поддерживаемый сценарий. Отдельный `doctor.sh` остаётся read-only диагностическим инструментом для разбора нестандартного состояния и ошибок.

После bootstrap адрес FreeNet Control Center выводится в консоль. По умолчанию интерфейс работает только в LAN на порту `1001`.

## Документация

- [`docs/INSTALL-EXISTING-STACK-RU.md`](docs/INSTALL-EXISTING-STACK-RU.md) — установка поверх существующего XKeen/Xray;
- [`docs/INSTALL-FROM-SCRATCH-RU.md`](docs/INSTALL-FROM-SCRATCH-RU.md) — установка с чистого Entware;
- [`docs/ISP-PRESETS-RU.md`](docs/ISP-PRESETS-RU.md) — ISP/DNS/routing policy;
- [`docs/ARCHITECTURE-RU.md`](docs/ARCHITECTURE-RU.md) — архитектура и границы ответственности;
- [`docs/RECOVERY-RU.md`](docs/RECOVERY-RU.md) — восстановление и rollback;
- [`docs/UPSTREAM-PINS-RU.md`](docs/UPSTREAM-PINS-RU.md) — upstream-артефакты и SHA-256;
- [`docs/providers/BLANCVPN-RU.md`](docs/providers/BLANCVPN-RU.md) — интеграция VPN-провайдера.

## Развитие проекта

Постоянные реестры:

- [Дорожная карта — Issue #5](https://github.com/VoltickVL/FreeNet-Router/issues/5);
- [Журнал изменений — Issue #7](https://github.com/VoltickVL/FreeNet-Router/issues/7).

Дорожная карта описывает продукт, провайдерские профили и следующие этапы. Журнал хранит факты PR, CI, release, runtime acceptance и rollback. Конкретные названия физических площадок не являются частью архитектуры продукта.
