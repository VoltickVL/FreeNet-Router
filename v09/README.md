# FreeNet 0.9 — лёгкий Control Center над оригинальными XKeen/Xray

**Продуктовое решение 07.11.2026:** FreeNet — **только оболочка и управление** штатными XKeen/Xray, проверка результата и **добавление собственной VPN-подписки**. Собственные AutoVPN, Split DNS proxy, GeoData cron, backup/restore/recovery center и прочие дублирующие сервисы **не нужны**. [Продуктовый контракт](PRODUCT_CONTRACT.md) и [обязательные инструкции](../AGENTS.md) имеют приоритет перед устаревшими идеями.

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
- Реальное read-only состояние из стандартных `04_outbounds.json`, `05_routing.json`, `03_dns.json`, каталога GeoData и конфигурации XKeen/cron.
- Версии официальных бинарников: `GET /api/native/versions`, только `/opt/sbin/xkeen -v` и `/opt/sbin/xray version`, ограничение времени/вывода, без shell.
- Действующая кнопка «Проверить Xray»: `GET /api/native/xray/validate` вызывает официальный `xray run -test -confdir` (проверяет конфигурацию, **не** запускает ещё один VPN).
- Статусы `CONFIG_ONLY`, `NOT_VERIFIED`, `MISSING`, `STOP` не подменяются ложным статусом VPN. Полные конфиги/UUID/Reality/subscription URL не выдаются.
- Изолированный `v09/installer/stage.sh` (plan/apply/status) проверяет exact version, SHA256SUMS, архитектуру, native paths, `-selftest`; `prepare.sh` предназначен для загрузки **только точного официального `v0.9.0`**. Пока release не опубликован, production auto-stage не включается.
- Preview слушает **только loopback** `127.0.0.1:1009`, не доступен как авторизованная iPhone-панель и не управляет сетевыми службами. Для LAN/iPhone обязательны auth/HTTPS.

**Подписка: выполнен первый безопасный браузерный шаг (НЕ применение).** На локальной странице «Подписка» доступен HTTPS-ввод и реальный `POST /api/subscription/preview`: загрузка по TLS, блокировка private/loopback/CGNAT/redirect/нестандартных портов, закрепление проверенных публичных DNS IP, жёсткий лимит размера и времени. Base64 и обычные share-links VLESS (Reality/TLS) и Trojan (TLS) превращаются в **обычные Xray outbounds**. Неизвестные параметры или неподдерживаемый формат = STOP всего импорта, без частичного списка. В браузер возвращаются только метаданные серверов, без subscription URL, UUID, Reality, credentials или JSON-кандидата. Ссылка не хранится и native конфигурация не меняется. Парсер следует проверенному подходу [XKeen Control Panel](https://github.com/shisui1511/xkeen-control-panel) (BSD-3-Clause) и [XKeen-UI](https://github.com/zxc-rv/XKeen-UI) (MIT); не копирует их VPN-алгоритмы или сетевые механизмы.

**Реальное native apply/refresh (реализовано в коде, аппаратно ЕЩЁ не принято):**
- Только в **авторизованном HTTPS Control Center**, защищённый `POST /api/subscription/apply` с Origin/CSRF и serial gate. Веб-панель даёт «Применить просмотренную» и «Обновить сохранённую»; ссылка не попадает в публичные API и не сохраняется в браузере.
- Private root-only файл `/opt/etc/freenet09/subscription-state.json` (`0600`, directory `0700`) содержит URL подписки и SHA256 управляемого фрагмента; никакого Backup Center/архива.
- Тонкий adapter строит **отдельный** native fragment `/opt/etc/xray/configs/04_zz_freenet09_subscription.json` (имя совместимо со stop-list XKeen 2.1), сохраняет оригинальные конфиги Xray/XKeen/UI как есть. Перед изменением сверяет фактические native configs и защищённую ownership receipt, проверяет **полный кандидат в отдельном приватном каталоге** оригинальной командой `xray run -test -confdir`. Неизвестные/изменённые/partial/symlink configs = STOP; присутствие legacy FreeNet 0.8 init = STOP.
- После проверки атомарно меняет **только собственный фрагмент**, вызывает **официальный XKeen `-restart`** с `XKEEN_FOREGROUND=1` для синхронного результата, затем штатный `-status` и подтверждение процесса Xray. При ошибке пытается вернуть только прежний managed fragment и штатно перезапустить XKeen; `ROLLBACK_FAILED` = STOP, никаких следующих операций. Это минимальная страховка одной транзакции, **не** отдельная подсистема резервных копий. Если подписка не изменилась, XKeen не перезапускается.
- Результат `CONFIG_APPLIED_NOT_EGRESS_VERIFIED` **не означает здоровый клиентский VPN**: полноценная проверка ISP/DNS/HTTPS маршрутa и hardware canary остаётся обязательной до релиза. На HOME/WORK/MOM ничего не применялось.

**Ещё НЕ реализовано:** автоматический Browser Setup/доверенный iPhone TLS, импорт дополнительных форматов провайдера (например VMess/Clash), управление всеми native функциями из браузера, FreeNet update/install cutover, подтверждённый клиентский VPN/DNS/HTTPS egress, аппаратная canary и чистое выключение FreeNet 0.8. До этих gate `v0.9.0` не готов.

## Защищённый браузерный вход (внутренний кандидат)

- Обычный `127.0.0.1:1009` по-прежнему **только read-only preview**. На LAN без проверки credentials FreeNet не выходит.
- Единственный администратор: по **явной операции установщика** `-init-admin` генерируется случайный 256-битный код. На Entware в `/opt/etc/freenet09/admin.sha256` (0600, родитель 0700) хранится **только SHA-256** этого высокоэнтропийного кода. Код показывается один раз **в локальном bootstrap**, не публикуется в issues/PR/логах/диагностике. Хеш случайного кода — не хеш пользовательского пароля.
- Для LAN выбирается **явный** `-https-listen <IP:port>` с готовым доверенным `-tls-cert` и `-tls-key`. Если сертификат/ключ или admin enrollment отсутствуют, secure listener **не стартует**, HTTP fallback в LAN отсутствует. Никакого wildcard bind.
- Веб-вход по HTTPS выдаёт только `Secure+HttpOnly+SameSite=Strict` cookie на 12 часов; проверки origin/custom action header и ограничение неверных попыток. Native API, dashboard, подписка и diagnostics не доступны до входа. На выходе сессия отзывается.
- На iPhone standalone PWA действительно станет доступной **только с доверенным iOS TLS-сертификатом**: самоподписанный сертификат без trust не выдавать за готовое решение. Автоматическое получение/привязка сертификата и первый браузерный мастер ещё обязательны до релиза.
- Это всё ещё **не** разрешение на изменение native Xray/XKeen. Применение конфигураций, подписка (persist/apply) и обновления — только после отдельных проверок и canary.

## Переход с FreeNet 0.8 без комбайна

- Новый FreeNet 0.9 сначала готовится отдельно, **не трогая действующий XKeen/Xray**, имеющийся XKeen UI, `/opt/etc/xkeen/`, `/opt/etc/xray/configs/`, подписки, списки, GeoData и credentials.
- Работающая FreeNet 0.8 остаётся активна, пока новая панель не проверена; **двух управляющих FreeNet одновременно нет**.
- После проверки управления и фактического VPN/DNS/HTTPS результата переключаем только FreeNet UI/службу/managed cron, не обновляя ядро в той же операции.
- После подтверждённой приёмки удаляем **только legacy FreeNet 0.8**, а native XKeen/Xray и их данные остаются.
- Нет отдельной программы backup/restore; если состояние partial/unknown, обновление или переключение не выполняем. Перед изменением штатных конфигов — native candidate validation и проверка результата.

**HOME/MOM не использовать для испытаний.** Canary — WORK лишь после готовности к безопасному переключению. Не давать пользователю SSH-команды вместо реализуемого Browser Setup.

## Внутренние проверки

- `cd v09 && go test -race ./...`
- `sh v09/tests/test_stage.sh`
- `sh v09/tests/test_prepare.sh`
- `sh v09/tests/test_architecture.sh --selftest`
- `.github/workflows/v09-isolated.yml`: профильный CI/ARM64/MIPS, без выпуска публичного `v0.9.0-dev`.

**Контроль:** [Roadmap #5](https://github.com/VoltickVL/FreeNet-Router/issues/5), [Journal #7](https://github.com/VoltickVL/FreeNet-Router/issues/7). Каждый PR начинается с проверки [PRODUCT_CONTRACT.md](PRODUCT_CONTRACT.md) и актуальных исходников официальных XKeen/Xray.
