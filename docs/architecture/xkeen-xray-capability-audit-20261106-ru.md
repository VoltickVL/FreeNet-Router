# Аудит функций XKeen/Xray для FreeNet — 06.11.2026

**Статус:** статический аудит официальных исходников, документации и тестов; **не** аппаратная приёмка и **не** разрешение на автоматическую установку Beta. Вначале изучен официальный XKeen 2.1, затем актуальный на 06.11 [XKeen 2.2 Beta](https://github.com/jameszeroX/XKeen/releases/tag/2.2_Beta), также [Xray-core 26.10.31](https://github.com/XTLS/Xray-core/releases/tag/v26.10.31) и companion UIs. Исходный установленный baseline со слов Management: **XKeen 2.1 Stable, Xray 26.10.31 Stable**, сохраняется без mutation.

## A. Функциональная карта и границы собственных механизмов

| Направление | Что уже делает upstream | Решение FreeNet |
|---|---|---|
| VPN-ядро, VLESS/Reality, transport | Xray-core inbound/outbound и валидация конфигурации `run -test` | Только UI/профили, транзакционная запись и native-validation; не собственный proxy engine |
| Выбор outbound | Xray `routing.balancers`, `leastPing` + Observatory и `RoutingService` (`bi`, `bo`) | Одна политика/один владелец. Не создавать ещё один `freenet-auto-vpn` поверх native |
| Выбор по пропускной способности | XKeen `-sb`: через локальный Xray HTTP probe замеряет outbound, `bo` и гистерезис | Показать настройку, состояние и управляемый native lifecycle; не дублировать собственным Live Pool |
| Выбор узла вручную | Только 2.1.1 Beta / 2.2 Beta: `xkeen -sb list|N|next|prev|auto`, pin (при перезапуске runtime override теряется) | После безопасного canary — точный UI для штатных команд, не самописный `bo` |
| Transparent proxy | XKeen TProxy (TCP+UDP), Hybrid (TCP Redirect, UDP TProxy), Redirect (TCP), netfilter/iptables, IPSET, locks | Не переписывать вручную цепочки/хуки, а читать native runtime и отдавать объяснение |
| Маршрутизация клиентов | XKeen firmware policies, DSCP, `xkeen_full`, PBR провайдера; Xray routing`/domainStrategy` | Product ISP presets и формирование **стандартных** правил, не второй policy engine |
| DNS | XKeen DNS integration; Xray `dns`, `dns` outbound, `domainStrategy`, qType-specific `direct/hijack/drop/return` | Предпочесть штатный DNS outbound и правила для non-A/AAAA, затем проверить клиентские TXT/SRV; без двойных DNS цепочек |
| GeoSite/GeoIP/GeoIPSET | XKeen установка, обновление, atomic/snapshot, watchdog и штатные `geo_*` ipsets | UI, проверка результата/актуальности; не параллельный download+replace при штатном сценарии |
| IP/port исключения | XKeen `ip_exclude.lst`, `port_exclude.lst`, `port_proxying.lst`, init/netfilter lifecycle | Browser-редактор с backup/CAS и штатное применение; **не** второй kernel ipset writer |
| Kill-switch | XKeen 2.1 `-killswitch` + 2.2 Beta удержание при рестарте | UI state и настройка по native; не собственные аварийные iptables DROP |
| Xray API/diagnostics | Xray RoutingService, StatsService, LoggerService, HandlerService, Observatory | Опрашивать в пределах бюджета, не пересоздавать API/probe и не подменять его состояние скоростью страницы |
| Мониторинг/Failover | XKeen -sb + Xray; FreeNet собственные best-server probes/QoE | Лёгкий native статус/observability — регулярно; тяжёлый Best Server — ручная углублённая диагностика и независимая клиентская acceptance, не конкурент переключения |
| Установка/backup/update | XKeen native install/staging/SHA/backup; Xray core distributions | Browser Setup, safe staged updater, rollback orchestration, UI; сохранять рабочий existing stack |

## B. Стабильная XKeen 2.1: что уже улучшено upstream

По [официальному release 2.1](https://github.com/jameszeroX/XKeen/releases/tag/2.1): RCI token для KeeneticOS 5.2, auto-install, DSCP 61 и `xkeen_full`, PBR, geo-файлы/geoipset, netfilter lock/atomic hook/cache/skip на DHCP renew без реального изменения WAN, обработка iptables-restore failures, Kill-switch (default OFF), защищённый /tmp/.xkeen (0700), SHA-256 для загрузок, staging+rollback. Уже есть native `-sb on|off|status` и `-sbt`: штатный выбор по реальной скорости поверх Xray API.

**Ограничение 2.1:** нет native `list|N|next|prev|auto`; старые комментарии или UI от newer main нельзя использовать как контракт Stable.

## C. XKeen 2.2 Beta (вышла 06.11) — что даёт и что проверять

По [release 2.2 Beta](https://github.com/jameszeroX/XKeen/releases/tag/2.2_Beta): (1) ручной pin/cycle на outbound и аппаратная кнопка; (2) корректные теги с пробелами и prefix-selector semantics балансировщика; (3) исправление JSONC парсинга в настройке -sb и раздельная проверка gRPC API/HTTP probe; (4) TProxy divert, заявленное разработчиками повышение пропускной способности **примерно на 10% в конкретном CPU-bound тесте**, не гарантия для любого роутера; (5) Kill-switch удерживается при `-restart`; (6) защита архивов от публикации частичных копий при ENOSPC, безопасный install rollback; (7) корректный статус Xray (CLI `xray api`/test не демон); (8) восстановление ipset deny-MAC без гонки; (9) правильная передача портовых списков в Entware; (10) исправления PBR xkeen_full, DNS/RCI/Entware-диагностики и редактируемые имена политик.

**Upstream CI:** [Spec tests](https://github.com/jameszeroX/XKeen/actions/runs/42166023869) completed SUCCESS на source main `2647b1f6...`, [Create Release](https://github.com/jameszeroX/XKeen/actions/runs/42166298200) SUCCESS на tag commit `bc9b59b...`. Это не подтверждает нашей архитектуры Keenetic/Entware/USB и клиентский HTTPS; от `2.1` до `2.2_Beta` 116 upstream commits и изменение системного netfilter/init.

**Риск обновления:** смена XKeen Stable→Beta может изменить системные хуки/маршруты, switch канала может изменить стратегию последующих `-uk`; резервная копия, точная версия/source SHA, контролируемое восстановление Stable, acceptance обязательны. Не обновлять рабочие площадки одним действием и не полагаться на exit 0.

## D. Xray-core 26.10.31 — что использовать, а не воспроизводить

[Routing](https://xtls.github.io/en/config/routing.html) поддерживает domain/IP/geo/rule-driven решение, `balancers` и first-match; [API](https://xtls.github.io/en/config/api.html) — native управление rule/balancer, мониторинг/логи/статистика. [DNS](https://xtls.github.io/en/config/dns.html) содержит кеширование, domainStrategy, DoH/DoT/DoQ, параллельные запросы и условные правила; [DNS outbound](https://xtls.github.io/en/config/outbounds/dns.html) — `qType` и `direct/hijack/drop/return` (особенно важно для наших TXT/SRV-ограничений).

**Безопасность 26.10.31:** официальный emergency release отключил IPv4 MPTCP по умолчанию для снижения риска опубликованной уязвимости ядра Linux. Xray 26.10.31 у нас установлен; не откатывать его ради перехода XKeen Beta. Аппаратные ресурсы (MIPS/ARM) и конфигурацию проверять в целевом runtime, не подменять общими предположениями.

## E. Выводы для beta.87 (по приоритету)

1. **Сейчас, без router mutation:** прекратить создание второго FreeNet Live Pool; поддержать read-only detection возможностей XKeen 2.1 Stable / 2.1.1 Beta / 2.2 Beta с fail-closed unknown. **Существующий legacy pool не удалять, не переписывать и не рестартовать.** Это PR #1367.
2. **Отдельный native-integration PR:** read-only выявление реальной topology Xray (outbounds, balancer selector, RoutingService, settings, schedule, mode/locking) с redaction; выявить конфликт владельцев и конфиг старого пула. Без `xkeen -sb status` в GET, т.к. CLI в 2.1 может вызывать self-heal Entware.
3. **Продуктовое canary XKeen 2.2 Beta:** план обновления WORK на текущем runtime после доказанного rollback/STOP reconciliation, staging + exact SHA + snapshot XKeen/Xray/UI/cron/netfilter, candidate validation и контролируемый apply + post-apply WAN/VPN/DNS/routing/kill-switch/IPv6. HOME/MOM сохранить на stable без изменений. Отсутствие подтверждённого runtime-факта = STOP; не требовать ручной SSH routine.
4. **Нативный выбор серверов:** использовать существующие eligible outbounds, а не создавать дублирующиеся резервные конфигурации. Добавление недостающих профилей только по явному выбору Management и безопасному native config plan, затем `-sb` по возможностям установленной версии.
5. **Убрать конкурирующие/тяжёлые механизмы:** отделить лёгкие native health и периодический -sb от Best Server deep throughput. Стандартный AutoVPN scheduler не должен независимо принимать несовместимое с -sb решение.
6. **После отдельной приёмки:** выключать/удалять наследуемый FreeNet Pool только транзакционно и с сохранением действующих настроек. Не объединять эту операцию с beta upgrade, изменением DNS или GeoData.

## F. Партнёры, лицензии и рабочие reference patterns

- [zxc-rv/XKeen-UI](https://github.com/zxc-rv/XKeen-UI): native list editing, save→restart workflow.
- [umarcheh001/Xkeen-UI](https://github.com/umarcheh001/Xkeen-UI): три .lst, job/status на restart (исходник reference-only без подтверждённой лицензии).
- [fan92rus/xkeen-ui](https://github.com/fan92rus/xkeen-ui): портовые списки, Xray-validation + backup (reference-only).
- [shisui1511/xkeen-control-panel](https://github.com/shisui1511/xkeen-control-panel): draft/diff/progress + rollback UX.
- [Razlo-coder/xkeen-autofailover](https://github.com/Razlo-coder/xkeen-autofailover): failover и validation patterns (reference-only).

**Правило:** сначала искать native API и партнёрский проверенный механизм, потом минимальная адаптация; не копировать исходники без совместимой лицензии, не заменять native shell/raw-терминалом, не считать read-only/CI runtime acceptance. Этот аудит должен регулярно пересматриваться при новых XKeen/Xray releases.
