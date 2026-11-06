# FreeNet Router: Upstream First (XKeen 2.1 / Xray-core 26.10.31)

**Решение Management от 06.11.2026.** Продукт FreeNet — Control Center и безопасная оркестрация поверх установленного стабильного XKeen/Xray, **не конкурирующий VPN/маршрутизатор**. Эта запись применима к текущей beta.87, не обещает автоматическую миграцию или изменение роутеров.

## Источник истины и границы ответственности

| Слой | Ответственный runtime | Роль FreeNet |
| --- | --- | --- |
| Ядро и outbound/балансировка | Xray-core 26.10.31; нативный `RoutingService` | Читать состояние, принимать продуктовые решения, вызывать только проверенные штатные операции |
| Установка ядра, служба, netfilter, ipset, routing policy | XKeen 2.1 Stable и Keenetic/Netcraze | Browser Setup, preflight, plan, guard, snapshot/rollback и объяснение результата; не дублировать hooks/iptables |
| Выбор скорости/переключение | XKeen 2.1 `xkeen -sb` и `-sbt`, Xray `bo` | Настройка и наблюдение через безопасный UI; не создавать второй независимый balancer |
| Config/GeoData | Native XKeen/Xray с существующими версиями и конфигами | Безопасный editor/backup/validation; применять через подтверждённый штатный lifecycle |

**Установленный baseline подтверждён Management:** XKeen 2.1 Stable, Xray 26.10.31 Stable. FreeNet не устанавливает и не заменяет эти компоненты в beta.87.

## Совместимость `-sb` с установленной XKeen 2.1

В **релизном теге 2.1** в `scripts/_xkeen/04_tools/08_tools_balancer/02_balancer_control.sh` есть `on|off|status`, а `-sbt` вызывает speed-balancer tick. **Команды `list|N|next|prev|auto` относятся к 2.1.1 Beta, поэтому не входят в текущий UI.** Нативный механизм измеряет ноды через временный Xray routing rule и переключает с гистерезисом через `xray api bo`, не меняя правила клиента на каждом измерении.

**Опасная деталь:** `xkeen -sb status` в `scripts/xkeen` релиза 2.1 перед вызовом `sb_status` проходит `_ensure_installed_packages`; операция может выполнять self-heal Entware. Поэтому read-only статус FreeNet читает только `xkeen.json` и verified installed metadata, не запускает `xkeen -sb status`, `-sbt` или `-sb on`. Статус `configured-enabled` не доказывает работающий Xray API или фактический egress клиента.

Нативное включение `-sb on` может изменять `xkeen.json`, Xray API/probe конфигурацию и вызывать `xkeen -restart`. Его нельзя включать при просмотре статуса, без подтверждённого плана, snapshot, Xray validation, postcheck и rollback. Unknown/failed rollback = **STOP**.

## Legacy FreeNet Live Pool

Предыдущий `freenet-auto-vpn` создаёт **собственный** Xray balancer/API/probe и контролируемо перезапускает ядро при первой активации; это не то же самое, что штатная `-sb` конфигурация. С beta.87 **новая активация FreeNet Live Pool прекращена на API и в UI**. Existing active pool, его snapshot, profile tags и рабочий VPN никогда не удаляются автоматически; остаются status, guarded switch и controlled restore/disable. Миграция к native владельцу — отдельная транзакция после фактической инвентаризации runtime, с одним владельцем Xray RoutingService.

## Partner-first работа

Перед каждой новой FreeNet подсистемой: (1) exact installed version и upstream code/CLI; (2) публичные изменения XKeen и Xray-core; (3) не менее двух активно поддерживаемых companion-проектов XKeen UI/Control Panel; (4) проверить реальный API/файловый lifecycle, безопасность, license и тесты; (5) выбрать native adapter или документированное дополнение; (6) только затем писать FreeNet код и scoped PR.

Проверенные ориентиры:
- [XKeen Stable 2.1](https://github.com/jameszeroX/XKeen/tree/2.1) — нативные CLI/netfilter/balancer и информация о self-heal.
- [Xray-core](https://github.com/XTLS/Xray-core) — RoutingService/Balancer (установлена версия 26.10.31).
- [zxc-rv/XKeen-UI](https://github.com/zxc-rv/XKeen-UI) и [shisui1511/xkeen-control-panel](https://github.com/shisui1511/xkeen-control-panel) — проверяемые принципы UI, состояние применения и rollback.
- [umarcheh001/Xkeen-UI](https://github.com/umarcheh001/Xkeen-UI), [fan92rus/xkeen-ui](https://github.com/fan92rus/xkeen-ui) — reference для штатного редактирования/рестарта; отсутствие явной лицензии запрещает слепое копирование исходников.

**Не следует выдавать CI за аппаратную приёмку, проверку клиентского HTTPS egress или доказательство отсутствия лагов.** Изменения реальной VPN/DNS/routing выполняются только через отдельный подтверждённый продуктовый apply с rollback, без SSH-preflight в обычном сценарии.
