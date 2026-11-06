# FreeNet 0.9 — чистая независимая основа

**Статус:** разработка; **не** выпуск 0.9 и **не** разрешение на миграцию рабочих роутеров.

## Источник истины и архитектура

0.9 начинается **с чистого дерева `v09/`**, без импорта Go backend, UI, AutoVPN, DNS/iptables writer, cron, инсталлятора или updater из 0.8.

- [Оригинальный XKeen](https://github.com/jameszeroX/XKeen) управляет сервисом, netfilter/ipset, интеграцией с Keenetic/Netcraze и штатными Xray операциями.
- [Xray-core](https://github.com/XTLS/Xray-core) — единственный VPN/proxy/routing engine.
- FreeNet 0.9 — тонкий браузерный Control Center; сейчас реализованы только локальный read-only preview и проверяемый side-by-side staging.

**Никаких новых сетевых политик, демонов, автоматик, подписок или собственных балансировщиков в этом PR.**

## Entware: соседние версии без влияния друг на друга

| Область | FreeNet 0.8 | FreeNet 0.9 |
| --- | --- | --- |
| Приложение | `/opt/sbin/freenet-ui` | `/opt/freenet-v09/releases/v0.9.0-dev/bin/freenet09` |
| Init | `/opt/etc/init.d/S99freenet-ui` | **Не создаётся при stage** |
| Состояние/автоматика | `/opt/etc/freenet/`, `/opt/lib/freenet/` и managed cron | **Ничего из этого не меняется** |
| Сетевой runtime | исходные `/opt/sbin/xkeen`, `/opt/sbin/xray`, `/opt/etc/xray/configs` | **Только читает наличие**, не устанавливает и не копирует |

### Staging contract

`v09/installer/stage.sh` принимает **локальный** набор: `SHA256SUMS` и точный архитектурный `freenet-v09-arm64-v8a`, `freenet-v09-mips32le` или `freenet-v09-mips32`. Этот набор позже будет загружать штатный Browser Setup, а не пользователь вручную через SSH.

- `plan` проверяет исходный стек, архитектуру Entware, SHA-256, `-version`, `-selftest` на временной копии кандидата. **Никаких записей в постоянный `/opt`.**
- `apply` переносит проверенный бинарник **только** в собственный каталог 0.9, атомарным rename. Повтор того же пакета идемпотентен. Конфликт имени/SHA, symlink, partial stack, invalid architecture = STOP.
- `status` сообщает только о наличии слота, **не** о работоспособности VPN.
- На этом этапе не устанавливаются init, cron, listener, default route, Xray/XKeen, updater или управляющие hooks. Никакая версия не активируется/выключается.

Сборка `freenet09 -selftest` не меняет окружение; `-version` выдаёт `v0.9.0-dev`. Сервер предпросмотра по умолчанию слушает **только** `127.0.0.1:1009`; запуск на LAN/wildcard запрещён до отдельной реализации аутентификации. `GET /api/overview` показывает `PRESENT/MISSING/UNKNOWN` по наличию штатных файлов, но **не** действующий runtime или HTTPS egress. Содержимое Xray config никогда не читается и не возвращается.

## Принятие A/B переключения — следующая отдельная стадия

До изменения active version необходимо:

1. Убедиться, что версия 0.8 и её управляемые cron/jobs известны; отсутствие сетевых rollback locks/checkpoints. Проверить владельца Xray RoutingService, валидность и hashes native configs. UNKNOWN = STOP.
2. Сохранить точные service/init/cron/app state 0.8 и проверить возможность возврата. Не трогать subscription/Reality/VLESS credentials или штатные XKeen/Xray.
3. Активировать **один** Controller: отключить старый writer и его managed cron, только затем разрешить управляемое состояние новому UI. Не выполнять обновление XKeen/Xray в той же транзакции.
4. Проверить process/listener/version/health/auth и фактический client HTTPS egress, DNS/routing и reboot selection.
5. Любой failed/unknown rollback = STOP. Удаление 0.8 — только после продолжительной приёмки и отдельного решения Management.

**HOME/MOM** не участвуют в развёртывании и остаются frozen. Canary — WORK после аппаратной проверки планирования и возврата. CI не заменяет испытания реального Keenetic/Netcraze.

## Тесты

- `cd v09 && go test -race ./...`
- `sh v09/tests/test_stage.sh`
- `.github/workflows/v09-isolated.yml`: CI и отдельные Linux/ARM64/MIPS сборки **без публикации релиза**.

Контекст: [Roadmap #5](https://github.com/VoltickVL/FreeNet-Router/issues/5), [Journal #7](https://github.com/VoltickVL/FreeNet-Router/issues/7), execution [#1370](https://github.com/VoltickVL/FreeNet-Router/issues/1370).
