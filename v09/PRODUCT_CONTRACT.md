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

## 4. Миграция 0.8 → 0.9: временная страховка и полная замена

**Итоговая продуктовая модель:** на роутере после приёмки работает **только FreeNet 0.9**. 0.8 — не постоянный параллельный продукт, а **временный rollback-слот**. Нельзя просто удалить рабочую 0.8 до запуска новой веб-панели: при проблеме доступа пользователь останется без управления, а recovery превратится в ручной SSH. Нельзя одновременно включать два управляющих FreeNet.

### Какие файлы сохраняются без миграции и переписывания

- Оригинальные бинарники, init и конфигурация XKeen, включая `/opt/sbin/xkeen`, `/opt/etc/xkeen/`, `xkeen.json`, `ip_exclude.lst`, `port_exclude.lst`, `port_proxying.lst` и индивидуальные XKeen lists.
- Оригинальный `/opt/sbin/xray`, `/opt/etc/xray/configs/`, GeoSite/GeoIP `.dat` и фактические subscriptions/credentials/Reality/VLESS в их текущих штатных путях, без экспорта секретов в UI/PR/log.
- **Существующий XKeen UI / его web-интерфейс**, если установлен: не удалять и не смешивать с прежним `FreeNet 0.8` UI. При включении 0.9 новая веб-панель FreeNet заменяет старую веб-панель FreeNet; XKeen UI сохраняется как самостоятельный инструмент.
- Любые нестандартные пути и custom lists выявлять автоматическим inventory, не гадать по имени. Partial/unknown = STOP.

### Временные слоты на Entware

| Фаза | FreeNet 0.8 | FreeNet 0.9 | XKeen / Xray |
| --- | --- | --- | --- |
| **Исходное состояние** | Работает по своему текущему адресу | Нет | Один рабочий стек |
| **Stage** | Продолжает работать | Файлы в `/opt/freenet-v09/`; preview только на защищённом loopback (или авторизованном отдельном порту после реализации) **без сетевых mutation** | **Не меняются** |
| **Переключение** | Service и только его managed cron остановлены, точная копия сохранена | Единственная активная панель на штатном FreeNet UI-порту (текущий baseline 1001), получает права управления **через штатные XKeen/Xray** | Работают без переустановки |
| **Проверка** | Выключен, доступен для rollback | Health, auth, restart/reboot, ISP/DNS/VPN, реальный HTTPS egress проверены | Одна политика и один owner |
| **Уборка** | Старые бинарник/веб-панель/init/managed cron/legacy app-файлы удалены из активной системы; запечатанный rollback-архив хранится временно отдельно | Работает как единственная FreeNet | **Не затронуты** |

**Важно:** архив 0.8 не запускается как вторая служба, не создаёт cron и не влияет на маршруты; это резервная копия для восстановления. Удалять его можно лишь после отдельного подтверждённого решения, не автоматически при активации 0.9.

### Один управляющий процесс и автоматический возврат

1. FreeNet 0.9 выполняет **read-only inventory** фактических путей/состояний 0.8, XKeen, Xray, существующего XKeen UI, конфигураций и источников cron. Инвентаризирует все старые FreeNet writers и managed jobs, чтобы не оставить «хвост». Неопознанные файлы/правила/locks — STOP.
2. Самостоятельно готовит `plan`: точные версии/SHA, список **только FreeNet-файлов** для замены/удаления, план порта/службы, expected delta, безопасный disk budget; подтверждает кандидата, native API/config compatibility и recovery mechanism.
3. Делает **защищённый snapshot** 0.8 (binary, init, все legacy application files и настройки, cron с точным перечнем managed jobs) плюс неизменяемые хэши native XKeen/Xray. Проверяет, что archive может восстановить точное прежнее состояние. Не копирует приватные конфигурации в публичный каталог и не пишет секреты в журнал.
4. Готовит **независимый однократный recovery-guard** перед сменой контроллера. Если новая UI/auth/health/egress проверка не прошла, guard возвращает точный снимок 0.8 автоматически даже при недоступности веб-панели; **это не второй постоянно работающий VPN-контроллер и не вторая система автоматизации**. Недоказанный recovery-guard = STOP до переключения.
5. Останавливает **только старые FreeNet** service/writers/jobs, затем включает FreeNet 0.9 как единственный управляющий UI. **Не перезапускает и не обновляет XKeen/Xray в этой транзакции.** Никаких конфликтов DNS/netfilter и незаметного VPN→DIRECT.
6. После активации выполняет проверку auth/UI/version, native Xray/XKeen, WAN/DNS/маршрутизации, обязательного VPN-HTTPS egress, повторный boot и повторную проверку one-owner/cron. Любая ошибка запускает rollback; его фактический результат фиксируется отдельно. `ROLLBACK_FAILED` / `UNKNOWN` = STOP, без следующей mutation.
7. Только после **успешной canary и стабильной приёмки** штатно удаляет из **активной** системы бинарники/службы/old cron/helpers/legacy FreeNet 0.8; **никогда не удаляет** native XKeen, Xray, XKeen UI, geo, списки или credentials. При необходимости selectively imports только подтверждённые продуктовые ISP preferences из 0.8 в **стандартные** Xray/XKeen settings; не копирует legacy algorithm/state/scheduler.

**Важные границы:** если старый FreeNet сам создал несущую рабочую VPN/DNS политику, нельзя удалять её вслепую как «хвост»: сначала показать exact native delta и испытать, что нужный маршрут сохраняется. Canary на WORK, HOME/MOM остаются frozen до отдельного решения.


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
- [ ] Временный stage рядом с 0.8 → **одна активная 0.9** → проверенный автоматический rollback без работающего UI → очистка **только legacy FreeNet 0.8** после приёмки; сохранность native XKeen/Xray/GeoData/lists/XKeen UI и корректный reboot/cron.
- [ ] Offline unit/integration/negative tests + CI terminal SUCCESS; canary WORK с отказами и восстановлением; HOME/MOM только по отдельному решению.
- [ ] Release notes, SHA manifest, main SHA, инструкции без секретов; issue #7 и #5 актуальны.

**Пока все пункты не приняты, `v0.9.0` не выпускать и не называть готовым рабочим билдом.** Фактическая разработка идёт под `v0.9.0-dev` без публикации публичных patch-релизов.

## 7. Технологические ограничения и порядок выполнения

1. Перед каждой подсистемой проверить точную версию и исходники установленного XKeen/Xray, штатный API/CLI, companion UI reference, licensing. Если upstream уже умеет — FreeNet даёт только браузерную обёртку, status и safe apply.
2. Одна техническая задача → один PR в `experiment/v0.9-clean`. Не смешивать разработку будущего UI, зависимости, обновление core и соседние баги. Тесты и scope обязательны.
3. Сначала staged install, затем authenticated Browser Setup, native control/updates, потом транзакционный A/B cutover, egress acceptance и единый релиз.
4. Описание решений и результаты по-русски; GitHub как source of truth; Issue #7 после каждого завершённого PR, Issue #5 при изменении стадии. Следующую задачу начинать только после закрытия предыдущей.

Источники: [XKeen официальный репозиторий](https://github.com/jameszeroX/XKeen), [Xray-core официальный репозиторий](https://github.com/XTLS/Xray-core), [Roadmap #5](https://github.com/VoltickVL/FreeNet-Router/issues/5), [Journal #7](https://github.com/VoltickVL/FreeNet-Router/issues/7).
