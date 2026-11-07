# AGENTS.md — текущие инструкции для работы с проектом (с 08.11.2026)

## Project Lock / решение Management

**FreeNet Router — закрытый исторический продукт.** Не разрабатывать, не исправлять, не устанавливать и не выпускать FreeNet 0.8, 0.9 и другие версии. Не возвращаться к старой цели `v09/RELEASE_STRATEGY.md`: она **устарела**, независимо от того, что написано в исторической ветке и архивных issue. Никаких собственных Split DNS, AutoVPN, веб-панелей, live pool, обновляющих демонов и cron.

**Текущая работа:** обслуживать и переносить **штатный XKeen + Xray + [Xkeen UI umarcheh001](https://github.com/umarcheh001/Xkeen-UI)** на Keenetic/Netcraze с Entware. Источники истины: **действующая конфигурация роутера**, документация точных установленных версий, [проверенный runbook](docs/XKEEN_UI_MIGRATION_RUNBOOK.md) и [активная тема #1409](https://github.com/VoltickVL/FreeNet-Router/issues/1409).

**Роли:** Management принимает продуктовые решения, ChatGPT самостоятельно выполняет доступные административные операции и QA через подключённые браузер, GitHub и Remote Desktop Commander. Не просить Management выполнять SSH-диагностику вместо ассистента, когда удалённые инструменты доступны.

## Проверенная архитектура HOME / WORK (08.11.2026)

- Оба: XKeen **2.1 Stable**, Xray **26.10.31**, Xkeen UI **3.0.2**, штатный **Hybrid TCP Redirect + UDP TProxy порт 61219**. Старый WORK custom-порт **5000** заменён штатным пресетом, не возвращаться к нему.
- BlancVPN: штатная подписка hourly, сейчас 50 TCP/REALITY и 6 WS отфильтрованы `^tcp$`, `leastPing` + Observatory 60 секунд; fallback существующий VLESS **не DIRECT**. Старую `04_outbounds.json` с VLESS не удалять.
- DNS: только штатный DNS-over-VLESS Xkeen UI, корректный inbound **`dns-in`**, `opkg dns-override`; DIRECT — базовые **77.88.8.8:53 и 77.88.8.1:53**, VPN upstream через VLESS, домашние имена через Keenetic, MX/TXT/SRV через VLESS, `dns.hosts` выключен. DNS capture **HOME 36/36**, **WORK 45/45** по последнему контролю. После изменения прошивки обязательно сверять *фактический* PREROUTING / `client_report`: на HOME Keenetic UPnP/Policy0 иногда перестраивает цепочки, штатный сторож UI их восстанавливает.
- HOME: провайдер Владлинк, **YouTube DIRECT**. WORK: Ростелеком, **YouTube VPN**, есть используемая `zkeenip.dat` с `ext:zkeenip.dat:youtube`, **не удалять**. АльянсТелеком по продуктовой модели — YouTube DIRECT; другие политики — только после диагностики собственного провайдера.
- GeoDAT: одно штатное задание каждого root cron **`10 4 * * 1 /opt/sbin/xkeen -ug`**, `crond` работает; в `/opt/etc/xkeen/xkeen.json` официальное описание `geosite.dat` и `geoip.dat` RunetFreedom. Нет активного FreeNet cron/демонов. Недельный результат 09.11 04:10 ещё требует фактической проверки.
- Оба FreeNet 0.8 и старая UI удалены из активных установок. Исторические архивы/предмиграционные файлы хранятся только для точечного отката. Панель не публиковать без безопасного доступа.
- **Не утверждать**, что WebRTC IPv4 leak на HOME исправлен; прежний опыт со STUN портами не сработал и отменён. Upstream [Xkeen UI issue #43](https://github.com/umarcheh001/Xkeen-UI/issues/43) про hourly restart REALITY остаётся открытым; никаких самодельных обходов.

## Следующие задачи

1. **PORTABLE — Keenetic Giga с 512 МБ RAM**, подключение к WORK для администрирования. Не предполагать точный KN-номер, CPU/MIPS/ARM и постоянного провайдера из факта подключения к WORK. Вначале read-only аудит текущих XKeen/Xray/Entware/подписок/политик/памяти и сохранение рабочей конфигурации.
2. **Мамин Keenetic Ultra с 512 МБ RAM** — отдельная самостоятельная миграция после PORTABLE.
3. На обоих 512 МБ проверить `MemAvailable`, RSS Xray/Python, нагрузку CPU, OOM/swap и реальные задержки **до и после** штатного DNS-over-VLESS. Процент RAM в UI не равен обязательному RSS: HOME измерялся как MemTotal 991 MiB, MemAvailable 468 MiB, Xray RSS 105 MiB, Python RSS ~90 MiB. Если 512 МБ не выдерживает, рассмотреть **официальный** `xray-minimal` профиль и убрать ненужные официальные модули; если DNS всё равно нестабилен, сохранить рабочий DNS Keenetic — не строить свой Split DNS.
4. После миграции каждого роутера проверять Xray `Configuration OK`, реальный Hybrid firewall/порт, DNS A/AAAA/MX/TXT и локальные имена, HTTPS и задержки, DIRECT/VPN egress, DNS capture на устройствах, отсутствие утечек и зависаний, автозапуск, reboot и GeoDAT. Только после приёмки удалять FreeNet и старые панели.

**Не копировать** на новые роутеры чужие MAC/IP, WAN DNS, interfaces, subscription URL, VLESS UUID/REALITY ключи, приватные Xray JSON или решения о DNS capture без чтения живой конфигурации. Секреты не записывать в GitHub.

**Отчёт:** «выявлено → исправлено → проверено → осталось». Новые факты размещать в **[#1409](https://github.com/VoltickVL/FreeNet-Router/issues/1409)**. Исторические FreeNet issues [#5](https://github.com/VoltickVL/FreeNet-Router/issues/5), [#7](https://github.com/VoltickVL/FreeNet-Router/issues/7), [#1408](https://github.com/VoltickVL/FreeNet-Router/issues/1408) закрыты, не возобновлять.
