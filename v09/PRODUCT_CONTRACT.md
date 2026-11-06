# FreeNet 0.9 — продуктовый контракт / источник истины

**Статус: IMPLEMENTATION, НЕ RELEASE READY.** Эта спецификация обязательна для любой разработки 0.9. Её задача — исключить уход в рефакторинг 0.8 и создание дубликатов штатных механизмов. Конечная цель: **одна стабильная `v0.9.0`**, а не серия пользовательских патч-релизов. Исходники 0.9 живут отдельно в `v09/` и `experiment/v0.9-clean`, но остаются в том же GitHub-репозитории ради общего Roadmap #5 и Journal #7.

## 1. Что является продуктом

FreeNet 0.9 — самостоятельный браузерный **Control Center для Keenetic/Netcraze с Entware**, работающий над установленными **официальными XKeen + Xray-core**. Через браузер пользователь должен уметь завершить setup, выбрать ISP и политики, настроить VPN/DNS/routing, управлять штатным XKeen/Xray, обновлять FreeNet/XKeen/Xray/GeoData, смотреть проверенный статус и выполнять восстановление. Не должен работать второй VPN-движок или второй управляющий балансировщик.

### Матрица: что уже даёт upstream

| Функция | Настоящий исполнитель | Что делает FreeNet |
| --- | --- | --- |
| VLESS/Reality, inbound/outbound, proxy | **Xray-core** | Создать/обновить совместимый native config, проверить `xray run -test`, показать состояние; не собственный proxy |
| Выбор и балансировка VPN | **Xray Observatory/балансировщики**, опционально **XKeen `-sb`** | Включение/статус **одного выбранного native режима**, без своего AutoVPN, Live Pool или параллельного override |
| Переключение/автовосстановление нод | Штатные Xray balancer / XKeen `-sb` на поддерживаемой версии | Только native lifecycle и диагностика; подписка с истёкшими credentials не исправляется алгоритмом failover — отдельное пользовательское действие |
| DNS, доменные правила и условный DIRECT/VPN | **Xray DNS, dns outbound, routing**, интеграция **XKeen** | ISP-пресеты и браузерный редактор **стандартной** конфигурации, без отдельного DNS-proxy или своих netfilter chains |
| TProxy, iptables, IPSET, firmware policy | **XKeen + Keenetic** | Вызов **поддерживаемой upstream операции**, валидация и rollback; никакого второго writer |
| GeoSite, GeoIP, GeoIPSET, автозагрузка | **XKeen** `-ug`, `-ugc`, штатный crond; альтернатива: Xray native geodata для соответствующих `.dat` | Показать дату и расписание, включить/выключить штатный режим; **не создавать параллельный cron** или собственные загрузчики |
| Старт/стоп/рестарт core | **XKeen init/CLI** `-start`, `-stop`, `-restart` | Аутентифицированные Browser actions с plan/postcheck, без raw-shell terminal |
| Обновление XKeen и Xray | **XKeen native updater** (точная команда — по установленной версии) | Plan, pin/канал/версия, snapshot и validation, native update с проверкой и rollback; XKeen Stable → Beta не автоматически |
| Обновление FreeNet и откат | **FreeNet A/B slots** (только приложения) | Download exact release/SHA, verified stage, one active controller, postcheck и возврат 0.8 |
| Редактирование списков/подписок | Штатные XKeen lists и Xray configs | Browser UX: backup, compare-and-swap, candidate validation, apply через XKeen; не раздавать секреты в API/log |

**Факт, который нельзя путать:** конфигурация Xray содержит правила, но её успешное чтение или `healthz` не доказывает фактический клиентский HTTPS egress. Отдельно проверяются Xray runtime, выбранный outbound и маршрут конечного клиента.

## 2. Модель настроек: ISP → routing policy

- **Владлинк:** YouTube → DIRECT.
- **АльянсТелеком:** YouTube → DIRECT.
- **Ростелеком:** YouTube → VPN.
- **Подряд:** собственный отдельный ISP profile, никаких предположений о политике другого провайдера.
- **Custom:** экспертный режим.
- Дополнительные правила (например, Bosch) только по установленной конфигурации или подтверждённому факту.

Политики выражать **нативными правилами Xray + XKeen**, без второго независимо исполняемого routing engine. При обязательном VPN недоступность всех серверов **не должна** автоматически превращать требуемый VPN-трафик в DIRECT; политика fail-closed подлежит аппаратным тестам.

## 3. Пользовательский путь (без обязательного SSH preflight)

**Новый роутер:** Keenetic/Netcraze → USB/Entware → один bootstrap → Browser Setup → определение стека → только недостающие upstream компоненты → VPN → ISP/DNS/routing → проверки → рабочая панель.

**Роутер с существующим стеком:** Entware + рабочий XKeen/Xray → bootstrap → распознавание exact versions, init/cron, конфигураций и credentials без раскрытия → **не переустанавливать исправные компоненты** → A/B stage FreeNet → Browser Setup → приемка → переключение владельца.

**Partial/unknown:** STOP с причиной, без эвристических reinstall/restart. User выполняет только физически необходимые действия на роутере.

## 4. Временное сосуществование 0.8/0.9 и возврат

- **Слот A:** действующий FreeNet 0.8 (`/opt/sbin/freenet-ui`, init, app settings, managed cron), пока не принята 0.9. Не переписывать.
- **Слот B:** чистый FreeNet 0.9 под `/opt/freenet-v09/` со своими auth/app state/release.
- **Общий и единственный:** штатный XKeen + Xray и их исходные конфигурации.
- На этапе stage можно только получать и валидировать пакет. Preview без защищённого браузерного доступа не слушает LAN, не запускает фоновые задачи и **не претендует на управление XKeen**.
- При активации — snapshot A/init/cron, подтверждение сетевых locks/checkpoints, доказанное выключение старых FreeNet writers/cron, затем включение B и проверка. **Никаких двух active controllers.**
- При неуспехе — вернуть точно прежние файлы A, init, managed cron, процесс/порт, подтвердить native Xray/XKeen, DNS и реальный HTTPS клиента. `ROLLBACK_FAILED` или `UNKNOWN` → STOP; не пытаться повторно.
- Удаление A и legacy хвостов **только после подтверждённой длительной приёмки B**, отдельной задачей. Нельзя удалять Xray configs, subscription/UUID/Reality keys.

## 5. Что пользователь видит в финальном интерфейсе

| Экран | Минимальная функциональность при выпуске |
| --- | --- |
| **Обзор** | Версии FreeNet/XKeen/Xray, active FreeNet slot, состояние служб, ISP, VPN/DIRECT, честная диагностика |
| **Первоначальная настройка** | Подписка и профили, ISP, native DNS/routing, безопасная проверка и сохранение без ручного SSH |
| **VPN и серверы** | Штатные outbounds, баланcировщик/Observatory/XKeen `-sb`, status, Refresh/Rotate/Failover только если поддерживается upstream |
| **DNS и маршрутизация** | Native DNS, DIRECT/VPN/BLOCK и ISP policy, редактирование XKeen списков, safe config apply |
| **XKeen / Xray** | Запуск/стоп/перезапуск, версии, native config validation, статусы ошибок |
| **Обновления** | FreeNet A/B, XKeen/Xray через upstream, GeoData/GeoIP/GeoIPSET и штатное расписание |
| **Диагностика / восстановление** | Route и egress, primary error, backup/restore, apply receipt, rollback result, безопасная browser recovery |

Экраны, которые ещё не работают, **не выдавать за реализованные**. Различать `CONFIG_ONLY`/`NOT_VERIFIED` и фактически подтверждённый runtime. Любые действия с сетью доступны только после authentication/authorization.

## 6. Критерии единственного рабочего релиза v0.9.0

Ниже — **блокирующие release gates**, а не план дополнительных публичных patch-билдов. CI отдельных PR — внутренняя разработка; runtime canary и rollout не заменяются GitHub CI.

- [ ] Сборка и воспроизводимый exact release/SHA на Entware AArch64 и поддерживаемых MIPS.
- [ ] Один bootstrap и **полный браузерный мастер** на новом и существующем стеке; никакой обязательной ручной SSH-проверки.
- [ ] Секреты сохраняются, не попадают в PR/логи/UI; безопасная аутентификация и ограничения доступа.
- [ ] Работают native XKeen/Xray start/stop/restart/status и подтверждённые версии/команды.
- [ ] Работающий VPN/выбор серверов — **только native Xray/XKeen**, без собственного AutoVPN или конкурирующего balancer.
- [ ] ISP/DNS/routing/GeoData/cron — только штатные механизмы; корректный DNS для A/AAAA/TXT/SRV и нужный HTTPS egress.
- [ ] Обновления FreeNet/XKeen/Xray/GeoData через браузер, с планом/проверками/бэкапом/откатом и без неразрешённого Beta upgrade.
- [ ] Установка 0.9 параллельно 0.8, безопасная активация **одного** владельца, возврат 0.8, корректный reboot, отсутствие legacy cron writers.
- [ ] Offline unit/integration/negative tests + CI terminal SUCCESS; canary WORK с отказами и восстановлением; HOME/MOM только по отдельному решению.
- [ ] Release notes, SHA manifest, main SHA, инструкции без секретов; issue #7 и #5 актуальны.

**Пока все пункты не приняты, `v0.9.0` не выпускать и не называть готовым рабочим билдом.** Фактическая разработка идёт под `v0.9.0-dev` без публикации публичных patch-релизов.

## 7. Технологические ограничения и порядок выполнения

1. Перед каждой подсистемой проверить точную версию и исходники установленного XKeen/Xray, штатный API/CLI, companion UI reference, licensing. Если upstream уже умеет — FreeNet даёт только браузерную обёртку, status и safe apply.
2. Одна техническая задача → один PR в `experiment/v0.9-clean`. Не смешивать разработку будущего UI, зависимости, обновление core и соседние баги. Тесты и scope обязательны.
3. Сначала staged install, затем authenticated Browser Setup, native control/updates, потом транзакционный A/B cutover, egress acceptance и единый релиз.
4. Описание решений и результаты по-русски; GitHub как source of truth; Issue #7 после каждого завершённого PR, Issue #5 при изменении стадии. Следующую задачу начинать только после закрытия предыдущей.

Источники: [XKeen официальный репозиторий](https://github.com/jameszeroX/XKeen), [Xray-core официальный репозиторий](https://github.com/XTLS/Xray-core), [Roadmap #5](https://github.com/VoltickVL/FreeNet-Router/issues/5), [Journal #7](https://github.com/VoltickVL/FreeNet-Router/issues/7).
