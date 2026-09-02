# Установка FreeNet Router v0.2.6 с нуля

Этот документ описывает фактический clean-room путь для нового Keenetic/Netcraze на release `v0.2.6`.

Кодовый сценарий уже реализован. Clean-room runtime acceptance на новом Keenetic Giga остаётся отдельным обязательным gate: до его прохождения нельзя считать установку полностью принятой только по CI или факту публикации release.

## Итоговая модель

```text
USB + Entware/OPKG
        ↓
read-only FreeNet Doctor
        ↓
bootstrap.sh v0.2.6
        ↓
pinned XKeen + Xray + XKeen UI + FreeNet
        ↓
Browser Setup
        ↓
subscription → Extra VPN → ISP/DNS
        ↓
Проверить готовность → Завершить настройку
        ↓
reboot/autostart acceptance
```

## Главное правило clean-room

Новый роутер не должен наследовать состояние HOME:

- не копировать HOME `/opt`;
- не копировать HOME Xray JSON;
- не переносить HOME subscription/UUID/Reality keys через GitHub или чат;
- не устанавливать вручную XKeen/Xray «в помощь» bootstrap;
- не обходить `NEEDS_REVIEW` или заблокированный ISP/DNS plan;
- любой недостающий prerequisite фиксировать как дефект installer-а или документации.

## Шаг 1. Подготовить USB

Для Entware/OPKG нужен накопитель с поддерживаемой роутером EXT-файловой системой; для современных устройств обычно используется **ext4**.

Официальные reference-инструкции:

- Netcraze: https://support.netcraze.ru/giga/nc-1012/en/20980-installing-the-entware-repository-on-a-usb-drive.html
- Keenetic: https://support.keenetic.ru/eaeu/viva/kn-1910/en/20980-installing-the-entware-repository-on-a-usb-drive.html

Архив Entware зависит от конкретной модели и архитектуры. Например, для части новых ARM64-моделей используется `aarch64-installer.tar.gz`, а для ряда Keenetic MIPS — `mipsel-installer.tar.gz`.

**Не выбирайте архитектуру по памяти.** Сверяйте модель с официальной документацией.

## Шаг 2. Включить OPKG / Open Package support

Типовой порядок:

1. установить компонент поддержки открытых пакетов/OPKG в firmware;
2. подключить подготовленный USB;
3. создать каталог `install` согласно официальной инструкции;
4. положить в него правильный Entware installer archive;
5. выбрать накопитель в настройках OPKG/Менеджера пакетов;
6. дождаться успешного появления `/opt` и рабочего `opkg`.

Это ручная предпосылка FreeNet. Всё после готового Entware должен выполнять FreeNet.

## Шаг 3. Войти в Entware shell

Если используется штатный SSH/CLI Keenetic/Netcraze:

```sh
exec /opt/usr/bin/sh
```

Если отдельно настроен Entware SSH, можно войти непосредственно в него, например:

```sh
ssh -p 222 root@<LAN-IP>
```

Если используется дефолтный пароль Entware root, смените его до дальнейшей эксплуатации.

## Шаг 4. Выполнить read-only FreeNet Doctor

До любой FreeNet mutation первая команда должна быть read-only:

```sh
curl -fLsS https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/doctor.sh | /opt/usr/bin/sh
```

Doctor проверяет Entware, архитектуру, инструменты, XKeen/Xray, процессы, Xray configs и порты. Он не устанавливает пакеты и не меняет конфигурацию.

Ожидаемые режимы:

### `MODE=ENTWARE_ONLY`

Нормальное состояние нового clean-room роутера: Entware есть, XKeen/Xray ещё нет.

`v0.2.6` **уже умеет** автоматически установить pinned XKeen/Xray/XKeen UI для этого режима. Не устанавливайте их вручную перед bootstrap.

### `MODE=READY_EXISTING_STACK`

Полный XKeen/Xray stack уже существует и валиден. `bootstrap.sh` должен сохранить его и перейти к FreeNet app-фазе без перестройки core.

### `MODE=NEEDS_REVIEW`

Найден частичный, противоречивый или неподдержанный stack. Это STOP.

Не пытайтесь «доустановить недостающее» вручную. Сначала нужно установить фактическую причину.

### `MODE=NO_ENTWARE` / `MODE=UNSUPPORTED_ARCH`

Установка FreeNet не начинается. Исправляется prerequisite или добавляется поддержка архитектуры отдельной задачей.

## Шаг 5. Запустить bootstrap

### Обычная установка текущего release

```sh
curl -fLsS https://github.com/VoltickVL/FreeNet-Router/releases/latest/download/bootstrap.sh | /opt/usr/bin/sh
```

### Clean-room acceptance именно v0.2.6

Для контрольного теста нужно исключить переход на будущий `latest`. Поэтому закрепите и сам bootstrap, и его внутреннюю базу assets на `v0.2.6`:

```sh
RELEASE=v0.2.6
curl -fLsS "https://github.com/VoltickVL/FreeNet-Router/releases/download/$RELEASE/bootstrap.sh" | FREENET_RELEASE_BASE="https://github.com/VoltickVL/FreeNet-Router/releases/download/$RELEASE" /opt/usr/bin/sh
```

Ожидаемый принцип работы `bootstrap.sh`:

1. проверить `/opt`, `opkg`, архитектуру и обязательные инструменты;
2. скачать release `SHA256SUMS`;
3. скачать только текущие FreeNet assets;
4. проверить SHA-256 каждого asset до установки;
5. выполнить read-only `bootstrap_entware.sh plan`;
6. при `ENTWARE_ONLY` установить targeted dependencies и pinned core transactionally;
7. при `READY_EXISTING_STACK` сохранить существующий core;
8. при `NEEDS_REVIEW` остановиться до app mutation;
9. создать отдельный backup FreeNet app-файлов и crontab;
10. установить FreeNet UI/manager и helpers;
11. проверить точную неизменность Xray JSON в app-фазе;
12. запустить FreeNet UI только на LAN;
13. проверить health/API/listener;
14. вывести `PANEL=http://<LAN-IP>:1001/`.

### Что устанавливается из release

`v0.2.6` публикует и покрывает `SHA256SUMS`, в том числе:

- `bootstrap.sh`;
- `bootstrap_entware.sh`;
- `freenet-ui-arm64-v8a`;
- `freenet-ui-mips32le`;
- `freenet-ui-mips32`;
- `freenet`;
- `vpn`;
- `blanc_xkeen_update_outbounds.sh`;
- `migrate_split_dns.sh`;
- `apply_network_profile.sh`;
- `apply_provider_profile.sh`;
- `finalize_setup.sh`;
- `upstream-pins.env`;
- `freenet.conf.example`.

## Шаг 6. Открыть Browser Setup

После успешного bootstrap откройте адрес `PANEL`, например:

```text
http://<LAN-IP-роутера>:1001/
```

Конкретный LAN IP не зашит в FreeNet; bootstrap определяет IPv4 интерфейса `br0`.

## Шаг 7. Настроить VPN-провайдера

Для текущего полностью реализованного BlancVPN flow:

1. ввести HTTPS key-link subscription;
2. нажать сохранение;
3. убедиться, что поле очищено после сохранения;
4. нажать **«Обновить список Extra-профилей»**;
5. выбрать нужный `Extra` профиль;
6. дождаться provider plan;
7. убедиться, что кандидат Xray валиден и `MUTATION=NONE`;
8. нажать **«Применить выбранный VPN-профиль»**;
9. дождаться фактического post-apply acceptance.

FreeNet не должен возвращать в браузер:

- subscription URL;
- UUID;
- VLESS URI/query;
- Reality public key/shortId;
- credential-bearing outbound JSON.

Если subscription или кандидат не проходят проверку, не вставляйте VLESS вручную в `04_outbounds.json`.

## Шаг 8. Выбрать интернет-провайдера / DNS

VPN-провайдер и интернет-провайдер — разные слои.

1. выбрать фактический ISP;
2. выбрать DNS mode;
3. сохранить профиль;
4. нажать **«Проверить план и состояние»**;
5. прочитать фактический expected delta;
6. применять только если план разрешён.

На текущем этапе автоматический clean-router runtime apply подтверждён для **Ростелеком**.

Для Владлинк / АльянсТелеком / Подряд preset IDs существуют, но автоматическая mutation должна оставаться заблокированной до собственного runtime acceptance каждого профиля.

Если clean-room Giga подключён к ещё не принятому ISP, это не повод обходить блокировку. Такой результат фиксируется как отдельный следующий runtime scope.

## Шаг 9. Проверить готовность

После принятого VPN и ISP/DNS нажать:

**«Проверить готовность»**.

Этот шаг read-only. Он должен подтвердить:

- subscription настроена;
- preferred Extra profile сохранён;
- присутствует ровно один `vless-reality`;
- присутствует `dns-out`;
- Xray запущен;
- полная Xray validation проходит;
- ISP/DNS plan остаётся `SUPPORTED=yes` и `MUTATION=NONE`;
- состояние XKeen autostart известно.

Пока `READY=no`, кнопка завершения не должна выполнять mutation.

## Шаг 10. Завершить настройку

После успешного read-only plan нажать:

**«Завершить настройку»**.

Перед mutation FreeNet обязан повторить fresh plan.

Успешный apply:

- включает XKeen autostart штатной командой `xkeen -auto on`;
- записывает `SETUP_COMPLETE=yes`;
- пересобирает только управляемый FreeNet cron block;
- сохраняет посторонние cron-задачи;
- проверяет Xray/ISP/DNS/autostart/cron после изменения.

При post-mutation ошибке helper должен восстановить:

- предыдущий FreeNet config;
- предыдущий crontab;
- исходное состояние XKeen autostart.

UI показывает **ОСНОВНУЮ ОШИБКУ** и **ОТКАТ** отдельно.

`ROLLBACK FAILED/UNKNOWN` = STOP. Не запускайте повторный apply без фактической read-only проверки состояния.

## Шаг 11. Acceptance до reboot

До перезагрузки проверить:

- `SETUP_COMPLETE=yes`;
- XKeen autostart = `on`;
- Xray config validation PASS;
- Xray process alive;
- FreeNet health/API PASS;
- `dns-out` присутствует;
- выбранный `vless-reality` соответствует preferred profile;
- ISP/DNS соответствует принятому plan;
- выбранный VPN endpoint активен;
- внешний IP соответствует ожидаемому VPN-региону;
- DNS test не показывает непредусмотренную утечку;
- FreeNet UI и XKeen UI доступны по ожидаемой LAN-схеме.

## Шаг 12. Reboot acceptance

Это обязательная часть clean-room, а не дополнительная проверка.

После контролируемой перезагрузки нужно заново подтвердить:

- Entware `/opt` поднялся;
- XKeen поднялся автоматически;
- Xray поднялся и config valid;
- FreeNet UI поднялся автоматически;
- `SETUP_COMPLETE=yes` сохранился;
- `dns-out` сохранился;
- preferred VPN profile/endpoint сохранился;
- фактический VPN IP корректен;
- DNS acceptance PASS;
- управляемый cron block присутствует;
- чужие cron-задачи не потеряны.

Только после этого Roadmap P1/P2 clean-router/reboot gate может быть отмечен выполненным.

## Что делать при ошибке

### Ошибка до mutation

Если doctor/bootstrap plan сообщает неподдерживаемое состояние, ничего не исправляйте «по месту». Сохраните вывод и установите причину.

### Ошибка core/app bootstrap

Разделяйте:

- PRIMARY ERROR;
- rollback state/error;
- фактическое состояние `/opt` после ошибки.

Если rollback неизвестен — никаких повторных mutation.

### Ошибка provider/ISP/finalize

UI уже разделяет основную ошибку и откат. После разрыва HTTP не нажимайте кнопку повторно вслепую — сначала обновите read-only status/plan.

## BlancVPN reference

Официальная справка BlancVPN:

- https://blancvpn.uno/ru/help/configuring-xkeen-windows

FreeNet использует её как reference по provider semantics, но не копирует credential-bearing конфигурацию и не хранит ключ-ссылку в репозитории.

## После clean-room PASS

Только после полного acceptance `v0.2.6` на новом Giga имеет смысл:

1. отметить runtime/reboot пункты Roadmap #5;
2. рассматривать обновление HOME через отдельный preflight;
3. рассматривать WORK только через его собственный read-only preflight/migration path;
4. переходить к следующему продуктового слою: endpoint quality/fallback/failover, routing UI, automation/system/backup, auth и lifecycle.
