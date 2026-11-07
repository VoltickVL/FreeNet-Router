# FreeNet 0.9 — лёгкий Control Center над оригинальными XKeen/Xray

**Продуктовое решение 07.11.2026:** FreeNet — **только оболочка и управление** штатными XKeen/Xray, проверка результата и **добавление собственной VPN-подписки**. Собственные AutoVPN, Split DNS proxy, GeoData cron, backup/restore/recovery center и прочие дублирующие сервисы **не нужны**. [Продуктовый контракт](PRODUCT_CONTRACT.md) и [обязательные инструкции](../AGENTS.md) имеют приоритет перед устаревшими идеями.

**Закреплённый upstream-ориентир (решение 07.11.2026): XKeen 2.2 Beta.** XKeen 2.1 Stable — лишь историческая база прежних проверок, не целевая платформа 0.9. До следующего native изменения сверить точный upstream 2.2 Beta tag/commit и команды; STOP при неподтверждённой совместимости. **Первый canary теперь HOME (решение Management 07.11.2026)**, затем PORTABLE, далее WORK после успешной проверки; HOME не изменять без доказанного безопасного плана и возможности вернуть рабочее состояние. Это не автоматическая команда обновлять установленный XKeen.

**HOME-readiness:** новый безопасный GET `/api/native/canary-readiness` фиксирует read-only версии, legacy/partial-совместимость и выдаёт `STAGING_ONLY` для HOME с XKeen 2.1+0.8. Не выполняет рестарт, обновление XKeen или проверку клиентского egress.

**Статус:** внутренняя разработка `v0.9.0-dev`, НЕ публичный релиз и НЕ разрешение на установку вместо рабочей 0.8. Единственная публичная цель — законченная `v0.9.0`.

## Что должна уметь готовая оболочка

1. **Подписка:** добавить и обновить личный URL, сохранить приватно, извлечь ноды только в **родной** Xray/XKeen-конфигурации.
2. **VPN и маршрутизация:** показать/выбрать штатные outbound/balancer/Observatory/XKeen `-sb`, ISP-пресеты DIRECT/VPN/BLOCK, редактировать native списки.
3. **DNS и GeoData:** только родные Xray DNS и XKeen/GeoData, штатное расписание, без второго планировщика.
4. **Службы и обновления:** XKeen/Xray start/stop/restart, официальные updater-механизмы, обновление оболочки FreeNet.
5. **Проверка:** версии и состояния, штатный Xray `run -test`, клиентский VPN/DNS/HTTPS egress, отсутствие фиктивного «работает».
6. **Веб + iPhone:** полноценная авторизованная панель и Safari Home Screen PWA с существующим визуалом.

**Не входят:** страницы резервных копий и восстановления, собственный Backup Center, автоматический архивационный процесс, второй VPN/DNS/routing/balancer engine.

## Что реализовано в отдельном коде `v09/`

- Адаптивный UI/PWA с мобильной навигацией, `manifest.webmanifest`, PNG 180/192/512, встроенный в отдельный Go бинарник без legacy backend 0.8.
- Реальное read-only состояние из стандартных `04_outbounds.json`, `05_routing.json`, `02_dns.json`, каталога GeoData и конфигурации XKeen/cron.
- Версии официальных бинарников: `GET /api/native/versions`, только `/opt/sbin/xkeen -v` и `/opt/sbin/xray version`, ограничение времени/вывода, без shell.
- Действующая кнопка «Проверить Xray»: `GET /api/native/xray/validate` вызывает официальный `xray run -test -confdir` (проверяет конфигурацию, **не** запускает ещё один VPN).
- Статусы `CONFIG_ONLY`, `NOT_VERIFIED`, `MISSING`, `STOP` не подменяются ложным статусом VPN. Полные конфиги/UUID/Reality/subscription URL не выдаются.
- Изолированный `v09/installer/stage.sh` (plan/apply/status) проверяет exact version, SHA256SUMS, архитектуру, native paths, `-selftest`; `prepare.sh` предназначен для загрузки **только точного официального `v0.9.0`**. Пока release не опубликован, production auto-stage не включается.
- Preview слушает **только loopback** `127.0.0.1:1009`, не доступен как авторизованная iPhone-панель и не управляет сетевыми службами. Для LAN/iPhone обязательны auth/HTTPS.

**Подписка: выполнен первый безопасный браузерный шаг (НЕ применение).** На локальной странице «Подписка» доступен HTTPS-ввод и реальный `POST /api/subscription/preview`: загрузка по TLS, блокировка private/loopback/CGNAT/redirect/нестандартных портов, закрепление проверенных публичных DNS IP, жёсткий лимит размера и времени. Base64 и обычные share-links VLESS (Reality/TLS) и Trojan (TLS) превращаются в **обычные Xray outbounds**. Неизвестные параметры или неподдерживаемый формат = STOP всего импорта, без частичного списка. В браузер возвращаются только метаданные серверов, без subscription URL, UUID, Reality, credentials или JSON-кандидата. Ссылка не хранится и native конфигурация не меняется. Парсер следует проверенному подходу [XKeen Control Panel](https://github.com/shisui1511/xkeen-control-panel) (BSD-3-Clause) и [XKeen-UI](https://github.com/zxc-rv/XKeen-UI) (MIT); не копирует их VPN-алгоритмы или сетевые механизмы.

**Реальное native apply/refresh (реализовано в коде, аппаратно ЕЩЁ не принято):**
- Только в **авторизованном HTTPS Control Center**, защищённый `POST /api/subscription/apply` с Origin/CSRF и serial gate. Веб-панель даёт «Применить просмотренную» и «Обновить сохранённую»; ссылка не попадает в публичные API и не сохраняется в браузере.
- Private root-only файл `/opt/etc/freenet09/subscription-state.json` (`0600`, directory `0700`) содержит URL подписки и SHA256 управляемого фрагмента; никакого Backup Center/архива.
- Тонкий adapter строит **отдельный** native fragment `/opt/etc/xray/configs/04_zz_freenet09_subscription.json` (имя проверялось против stop-list XKeen 2.1; на целевом XKeen 2.2 Beta требуется новая проверка), сохраняет оригинальные конфиги Xray/XKeen/UI как есть. Перед изменением сверяет фактические native configs и защищённую ownership receipt, проверяет **полный кандидат в отдельном приватном каталоге** оригинальной командой `xray run -test -confdir`. Неизвестные/изменённые/partial/symlink configs = STOP; присутствие legacy FreeNet 0.8 init = STOP.
- После проверки атомарно меняет **только собственный фрагмент**, вызывает **официальный XKeen `-restart`** с `XKEEN_FOREGROUND=1` для синхронного результата, затем штатный `-status` и подтверждение процесса Xray. При ошибке пытается вернуть только прежний managed fragment и штатно перезапустить XKeen; `ROLLBACK_FAILED` = STOP, никаких следующих операций. Это минимальная страховка одной транзакции, **не** отдельная подсистема резервных копий. Если подписка не изменилась, XKeen не перезапускается.
- **Критическое правило upstream Xray:** файл новых outbound должен содержать `tail` в имени (`04_zz_freenet09_subscription_tail.json`). В [официальном multi-file merge](https://xtls.github.io/ru/config/features/multiple.html) Xray добавляет outbounds **в начало**, если `tail` отсутствует — это могло бы подменить default DIRECT/VPN. С `tail` они добавляются **в конец**, не меняя действующий default. Старый неточный `04_zz_freenet09_subscription.json` = **STOP**, не автоматический перенос. При подтверждении применения повторно читается управляемый фрагмент после штатного XKeen restart; проверка init `S05xkeen status` не запускает скрытое package self-heal из команды `xkeen -status`.
- **Обязательная нативная привязка узлов:** `GET /api/subscription/targets` (только authenticated HTTPS) показывает **существующие Xray routing balancers, на которые уже ссылаются rules через `balancerTag`**, и их selector prefixes. Пользователь выбирает штатный selector, FreeNet добавляет соответствующий префикс к generated tag ноды **перед** официальным `xray run -test`. `05_routing.json` не переписывается; неоднозначность/missing/изменённый selector = STOP; private subscription receipt запоминает выбранную пару. Состояние `CONFIG_APPLIED_NOT_EGRESS_VERIFIED` не равно подтверждённому VPN/HTTPS. **XKeen `-sb` отдельно может не видеть tail fragment**, так как использует свой `outbounds_file`; поддержку speed balancer и реальный клиентский egress проверять отдельно, не обещать автоматически.

- Результат `CONFIG_APPLIED_NOT_EGRESS_VERIFIED` **не означает здоровый клиентский VPN**: полноценная проверка ISP/DNS/HTTPS маршрутa и hardware canary остаётся обязательной до релиза. На HOME/WORK/MOM ничего не применялось.

**Ещё НЕ реализовано:** автоматический Browser Setup/доверенный iPhone TLS, импорт дополнительных форматов провайдера (например VMess/Clash), управление всеми native функциями из браузера, FreeNet update/install cutover, подтверждённый клиентский VPN/DNS/HTTPS egress, аппаратная canary и чистое выключение FreeNet 0.8. До этих gate `v0.9.0` не готов.

## Управление штатной службой XKeen из защищённой панели

Кнопки `Запустить / Перезапустить / Остановить` доступны **только после HTTPS-авторизации**, с явным подтверждением действия. FreeNet не реализует собственный lifecycle/AutoVPN: вызываются **фиксированные команды установленного оригинального `/opt/etc/init.d/S05xkeen`**: `start on`, `restart on`, `stop`; `status` вызывается отдельно и не запускает скрытое Entware package self-heal в оболочке `xkeen -status` 2.1. Нет raw shell и произвольных параметров.

Применение подписки и действия службы используют **один сериализованный mutation gate**, не запускаются параллельно. При наличии legacy `S99freenet-ui` изменение блокируется. API `/api/native/xkeen/service` доступен только через защищённый Control Center с Origin/CSRF; обычный loopback preview остаётся read-only. Вывод upstream команды в UI/лог не выводится: возможны секреты. `RUNNING_PROCESS_DETECTED` или `STOPPED` — только факты native init/process, **НЕ подтверждение клиентского HTTPS/VPN/DNS egress**. Ошибка/неоднозначность = `UNKNOWN`, без автоматических повторов или сетевого fallback.

## Защищённый браузерный вход (внутренний кандидат)

- Обычный `127.0.0.1:1009` по-прежнему **только read-only preview**. На LAN без проверки credentials FreeNet не выходит.
- Единственный администратор: по **явной операции установщика** `-init-admin` генерируется случайный 256-битный код. На Entware в `/opt/etc/freenet09/admin.sha256` (0600, родитель 0700) хранится **только SHA-256** этого высокоэнтропийного кода. Код показывается один раз **в локальном bootstrap**, не публикуется в issues/PR/логах/диагностике. Хеш случайного кода — не хеш пользовательского пароля.
- Для LAN выбирается **явный** `-https-listen <IP:port>` с готовым доверенным `-tls-cert` и `-tls-key`. Если сертификат/ключ или admin enrollment отсутствуют, secure listener **не стартует**, HTTP fallback в LAN отсутствует. Никакого wildcard bind.
- Веб-вход по HTTPS выдаёт только `Secure+HttpOnly+SameSite=Strict` cookie на 12 часов; проверки origin/custom action header и ограничение неверных попыток. Native API, dashboard, подписка и diagnostics не доступны до входа. На выходе сессия отзывается.
- На iPhone standalone PWA действительно станет доступной **только с доверенным iOS TLS-сертификатом**: самоподписанный сертификат без trust не выдавать за готовое решение. Автоматическое получение/привязка сертификата и первый браузерный мастер ещё обязательны до релиза.
- Это всё ещё **не** разрешение на изменение native Xray/XKeen. Применение конфигураций, подписка (persist/apply) и обновления — только после отдельных проверок и canary.

## Чистая установка, без переноса старой DNS-схемы

- **Новый FreeNet 0.9 — с нуля.** Не мигрировать FreeNet 0.8, не брать его Split DNS, DNS-прокси, AutoVPN, cron, policy helpers, UI state или backend.
- Сохранять **пользовательскую VPN-подписку и нужные DIRECT/VPN правила по смыслу**, а не технические зависимости 0.8. На HOME ссылка хранится в `/opt/etc/xray/blanc_subscription.url` — не выводить, не удалять и не передавать в лог.
- На HOME старый FreeNet 0.8 **встроил DNS-технику в Xray JSON**: `02_dns.json` (tags `dns-direct`/`dns-vless`), `04_outbounds.json` (`dns-out`) и `05_routing.json` (DNS-specific routes); read-only native `S05xkeen` сейчас `proxy_dns=off`. Наличие этих файлов само по себе не разрешает их повторное применение. Старую DNS-схему нужно отделить от пользовательской политики после проверки зависимостей и Xray candidate validation; никакого автоматического удаления или рестарта.
- Новый `GET /api/native/preserved-sources` показывает только факт наличия источников и `NATIVE_CONFIG_REVIEW_REQUIRED` без чтения или выдачи токенов/URL/содержимого конфигурации.
- Штатный XKeen 2.1→2.2 Beta — отдельный upstream upgrade. После безопасной проверки чистой 0.9 отключить только legacy FreeNet 0.8, сохранив native XKeen/Xray, личную подписку, ISP/routing и GeoData. Единственный владелец операций.


- Новый FreeNet 0.9 сначала готовится отдельно, **не трогая действующий XKeen/Xray**, имеющийся XKeen UI, `/opt/etc/xkeen/`, `/opt/etc/xray/configs/`, подписки, списки, GeoData и credentials.
- Работающая FreeNet 0.8 остаётся активна, пока новая панель не проверена; **двух управляющих FreeNet одновременно нет**.
- После проверки управления и фактического VPN/DNS/HTTPS результата переключаем только FreeNet UI/службу/managed cron, не обновляя ядро в той же операции.
- После подтверждённой приёмки удаляем **только legacy FreeNet 0.8**, а native XKeen/Xray и их данные остаются.
- Нет отдельной программы backup/restore; если состояние partial/unknown, обновление или переключение не выполняем. Перед изменением штатных конфигов — native candidate validation и проверка результата.

**Первый canary — HOME**, затем PORTABLE и WORK после проверок; MOM не трогать. Не давать пользователю SSH-команды вместо реализуемого Browser Setup.

## Внутренние проверки

- `cd v09 && go test -race ./...`
- `sh v09/tests/test_stage.sh`
- `sh v09/tests/test_prepare.sh`
- `sh v09/tests/test_architecture.sh --selftest`
- `.github/workflows/v09-isolated.yml`: профильный CI/ARM64/MIPS, без выпуска публичного `v0.9.0-dev`.

**Контроль:** [Roadmap #5](https://github.com/VoltickVL/FreeNet-Router/issues/5), [Journal #7](https://github.com/VoltickVL/FreeNet-Router/issues/7). Каждый PR начинается с проверки [PRODUCT_CONTRACT.md](PRODUCT_CONTRACT.md) и актуальных исходников официальных XKeen/Xray.
