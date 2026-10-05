# FreeNet Router

**FreeNet Router** — локальный Control Center для Keenetic/Netcraze с Entware, XKeen и Xray. Он управляет VPN, DNS, маршрутизацией, автоматическим восстановлением VPN, подпиской и обновлениями через браузер, а SSH оставляет только для первичной установки Entware и аварийной диагностики.

Целевой пользовательский путь:

```text
Keenetic / Netcraze
        ↓
USB-накопитель с EXT4
        ↓
Entware / OPKG
        ↓
ОДНА команда FreeNet bootstrap
        ↓
FreeNet сам определяет состояние роутера
        ├─ чистый Entware → ставит pinned XKeen + Xray + XKeen UI
        ├─ рабочий XKeen/Xray → сохраняет существующий стек
        └─ partial/unknown → STOP без догадок
        ↓
FreeNet Control Center в браузере
        ↓
Subscription → VPN → DNS → Routing → Automation → Acceptance
```

> **На USB не ставится отдельная операционная система.** Роутер продолжает работать под KeeneticOS/Netcraze OS. Накопитель используется для Entware и каталога `/opt`, где затем находятся Xray/XKeen/FreeNet и их локальные данные.

---

## 1. Что именно устанавливается

### FreeNet

FreeNet — верхний уровень управления. Он отвечает за:

- Browser Setup и ежедневную эксплуатацию;
- хранение VPN subscription локально на роутере;
- выбор VPN-профиля;
- `Refresh endpoint`, ручной выбор и AUTO VPN;
- canonical VPN-ping и Best Server;
- DNS mode;
- явные routing rules `DIRECT / VPN / BLOCK`;
- автоматические проверки и расписания;
- безопасный plan/apply/post-check/rollback;
- обновление самого FreeNet;
- единый Journal с временной последовательностью событий.

### Xray

[Xray-core](https://github.com/XTLS/Xray-core) — непосредственно VPN/proxy engine. Он обрабатывает VLESS/Reality, outbounds, DNS-out и routing-конфигурацию.

FreeNet использует pinned-версию Xray и проверяет загружаемый upstream-артефакт по SHA-256. Актуальный pin находится в [`config/upstream-pins.env`](config/upstream-pins.env).

### XKeen

[XKeen](https://github.com/jameszeroX/XKeen) интегрирует Xray с Keenetic/Netcraze: сервис, запуск, DNS/proxy integration и окружение роутера.

На чистой установке FreeNet регистрирует XKeen в безопасном pre-setup состоянии: autostart и proxy-DNS не включаются до того, как Browser Setup примет соответствующую сетевую политику.

### XKeen UI

[XKeen UI](https://github.com/zxc-rv/XKeen-UI) — upstream-интерфейс XKeen. FreeNet устанавливает его как часть поддерживаемого core stack. Основной эксплуатационный интерфейс проекта — **FreeNet Control Center**, а не XKeen UI.

Типичные локальные порты после установки:

- XKeen UI: `http://<LAN-IP>:1000/`;
- FreeNet Control Center: `http://<LAN-IP>:1001/`.

FreeNet Control Center слушает LAN-адрес, а не wildcard `0.0.0.0`.

---

# Установка с нуля

## 2. Что понадобится

Нужны:

- совместимый Keenetic или Netcraze с USB;
- USB-флешка, SSD или другой надёжный USB-накопитель;
- доступ к веб-интерфейсу роутера;
- один SSH-сеанс для установки Entware и запуска FreeNet bootstrap;
- интернет на роутере.

FreeNet сейчас собирается для Entware-архитектур:

- AArch64 → `arm64-v8a`;
- MIPSel → `mips32le`;
- MIPS → `mips32`.

Архитектуру **не надо угадывать**: Entware ставится по официальной инструкции именно для вашей модели, а FreeNet затем сам читает `opkg print-architecture`.

---

## 3. Подготовка USB

### Рекомендуемая файловая система — EXT4

Для Entware используйте отдельный раздел EXT4. Keenetic и Netcraze в своих инструкциях также рекомендуют EXT4 для OPKG/Entware.

**Форматирование уничтожит данные на выбранном разделе.** Если на накопителе что-то нужно сохранить — сначала сделайте копию.

### Вариант A — форматирование прямо на роутере

На современных версиях KeeneticOS, где доступен раздел **Storage & Devices / Накопители и принтеры**, накопитель можно инициализировать/форматировать в EXT4 из веб-интерфейса после установки компонента поддержки EXT-файловых систем.

Официальная инструкция Keenetic:

- [Formatting and checking a file system on a USB drive](https://support.keenetic.com/titan/kn-1811/en/100924-formatting-and-checking-a-file-system-on-a-usb-drive.html)

### Вариант B — форматирование на компьютере

Удобнее всего использовать Linux/GParted или другой инструмент, умеющий корректно создавать EXT4.

Официальная инструкция Keenetic по EXT4:

- [Using the ext4 file system on USB drives](https://support.keenetic.com/hero/kn-1011/en/21024-using-the-ext4-file-system-on-usb-drives.html)

Windows штатно не работает с EXT4 как с обычным диском, поэтому если форматирование выполняется с Windows, используйте подходящий partition manager либо форматирование средствами самого роутера.

### Компоненты роутера

В компонентах KeeneticOS/Netcraze OS должны быть установлены:

1. поддержка файловой системы EXT;
2. **Open Package support / Поддержка открытых пакетов (OPKG)**.

SMB нужен только если вы копируете Entware installer на USB по сети; для работы FreeNet сам по себе SMB не требуется.

---

## 4. Установка Entware / OPKG

Entware устанавливается **до FreeNet**, потому что он создаёт `/opt` и пакетный менеджер `opkg`.

Официальные инструкции:

- Keenetic: [Installing the Entware repository on a USB drive](https://support.keenetic.com/titan/kn-1811/en/20980-installing-the-entware-repository-on-a-usb-drive.html)
- Netcraze: [Установка репозитория Entware на USB-накопитель](https://support.netcraze.ru/giga/nc-1012/ru/20980-installing-the-entware-repository-on-a-usb-drive.html)

В примерах по ссылкам указаны конкретные модели. Для своего роутера выберите инструкцию именно своей модели на сайте поддержки: архив Entware зависит от CPU (`aarch64 / mipsel / mips`).

Общая последовательность:

1. Подготовить EXT4-раздел.
2. Установить компонент **Open Package support**.
3. Скачать Entware installer, соответствующий архитектуре вашего роутера.
4. Создать на USB каталог `install` и поместить туда installer archive так, как указано в официальной инструкции.
5. В веб-интерфейсе выбрать подготовленный EXT4-накопитель для OPKG и дать нужному локальному пользователю доступ к OPKG.
6. Дождаться установки Entware по системному журналу.
7. Подключиться к Entware shell по SSH согласно инструкции вашей модели.
8. Проверить:

```sh
/opt/bin/opkg update
/opt/bin/opkg print-architecture
```

Если `/opt/bin/opkg` отсутствует или `opkg update` не работает — **FreeNet пока не запускать**. Сначала должна быть исправна сама установка Entware.

---

# 5. Установка FreeNet одной командой

После рабочего Entware вручную ставить `jq`, `unzip`, Xray, XKeen или XKeen UI **не нужно**.

Скопируйте в SSH целиком одну строку:

```sh
/opt/bin/opkg update && /opt/bin/opkg install ca-bundle curl && /opt/bin/curl -fLsS https://github.com/VoltickVL/FreeNet-Router/releases/latest/download/bootstrap.sh -o /tmp/freenet-bootstrap.sh && /opt/bin/sh /tmp/freenet-bootstrap.sh
```

Что делает эта команда:

1. обновляет список пакетов Entware;
2. гарантирует наличие HTTPS CA bundle и `curl`, необходимых для первичного получения bootstrap;
3. скачивает текущий опубликованный `bootstrap.sh` во временный файл;
4. запускает его в Entware shell.

После запуска сам FreeNet bootstrap:

- проверяет `/opt` и `opkg`;
- определяет архитектуру;
- **сам устанавливает только недостающие userland-инструменты** через targeted `opkg install`;
- никогда не делает глобальный `opkg upgrade`;
- загружает FreeNet release assets;
- проверяет их по `SHA256SUMS`;
- классифицирует существующий core stack;
- либо сохраняет исправный XKeen/Xray, либо ставит pinned core на чистый Entware;
- делает backup перед mutation;
- устанавливает FreeNet и helpers;
- проверяет запуск и LAN-only Control Center;
- при ошибке после mutation запускает rollback и отдельно сообщает его результат.

---

## 6. Как FreeNet определяет состояние роутера

### `ENTWARE_ONLY`

Есть исправный Entware, но XKeen/Xray ещё не установлены.

FreeNet:

1. ставит только необходимые Entware dependencies;
2. скачивает **pinned** XKeen, Xray и XKeen UI;
3. проверяет upstream SHA-256;
4. делает backup;
5. устанавливает core;
6. регистрирует XKeen с безопасными pre-setup defaults;
7. валидирует Xray;
8. запускает XKeen UI;
9. устанавливает FreeNet.

### `READY_EXISTING_STACK`

XKeen/Xray уже установлены и состояние выглядит полным.

FreeNet **не переустанавливает исправный core** и не должен сбрасывать:

- текущие VLESS/Reality credentials;
- subscription secret;
- Xray-конфиги;
- выбранный VPN;
- чужие cron-задачи.

Он добавляет/обновляет только собственные FreeNet-компоненты и далее передаёт работу Browser Setup.

### `NEEDS_REVIEW`

Обнаружен частичный или противоречивый stack: например, XKeen есть, а Xray нет, либо присутствуют неполные конфиги.

Это **STOP**. FreeNet не пытается угадать, что «доставить вручную», и не выполняет опасную mutation.

### `NO_ENTWARE` / `UNSUPPORTED_ARCH`

Установка не начинается.

---

# 7. Browser Setup

После успешного bootstrap в консоли будет выведен адрес вида:

```text
http://192.168.x.x:1001/
```

Дальше обычная настройка должна выполняться через браузер.

Последовательность:

1. открыть FreeNet Control Center;
2. создать локальную авторизацию FreeNet, если она ещё не настроена;
3. сохранить VPN subscription;
4. получить список VPN-профилей;
5. проверить/выбрать VPN;
6. выбрать DNS mode;
7. настроить routing rules;
8. включить нужную Automation;
9. завершить setup только после успешного read-only readiness plan.

Subscription URL и VPN credentials остаются на роутере и не должны публиковаться в GitHub, issue, PR или логах.

---

# 8. Как FreeNet работает с VPN

## Subscription

Фоновая проверка подписки по расписанию — это housekeeping и обновление last-known-good состояния.

**Критические операции не обязаны ждать следующего фонового интервала.**

Перед операциями, где актуальность каталога влияет на решение, FreeNet получает свежий snapshot subscription:

- ручной `↻ VPN-пинг`;
- `Подобрать варианты` / Best Server;
- AUTO endpoint recovery текущего логического VPN;
- AUTO Best Server fallback.

Поэтому изменение endpoint у VPN-провайдера не означает, что FreeNet обязательно будет ещё десятки минут использовать старый список.

Если fresh source недоступен, операция должна использовать только допустимый source-bound last-known-good fallback либо завершиться fail-closed — без угадывания credentials.

## Canonical VPN-ping

Current VPN, ручной selector и первый этап Best Server используют одну canonical VPN-ping метрику. Пользовательский VPN-ping не подменяется raw TCP RTT.

Отдельные последовательные замеры всё равно могут отличаться на несколько миллисекунд из-за реального состояния сети.

## Best Server

Ручной «Подобрать варианты» строит именно **Top-3 альтернатив**, потому что текущий VPN уже показан отдельно в левой части Overview и не занимает место среди вариантов замены.

Алгоритм:

```text
fresh subscription snapshot
        ↓
full quick VPN-ping sweep
        ↓
hard shortlist до 10 профилей
        ↓
кандидаты 1–5: serial deep checks
        ↓
3 Eligible уже найдены?
   ├─ да → STOP
   └─ нет
        ↓
reserve 6–10: проверять по одному
        ↓
STOP сразу после добора недостающих до 3
```

Deep check включает сравнимые application/site RTT, strict speed/throughput и остальные обязательные acceptance criteria. Резерв никогда не прогоняется целиком «на всякий случай»: если после первых пяти есть 2 подходящих, из второй пятёрки проверяется только столько профилей, сколько нужно для поиска третьего.

В UI live-progress показывает, сколько deep-кандидатов уже проверено из shortlist, а итоговая строка — сколько профилей было обнаружено, сколько попало в shortlist, сколько реально глубоко проверено и сколько Eligible найдено из трёх.

Фактическое время зависит от количества профилей, качества сети и того, насколько быстро набираются три Eligible альтернативы; фиксированное время не гарантируется.

---

# 9. AUTO VPN

AUTO VPN не должен переключать сервер только потому, что один одиночный probe был неудачным.

Поддерживаемая health cadence:

- `30s`;
- `1m` — default;
- `5m`.

Для режима `30s` long-running FreeNet service имеет внутренний scheduler; cron остаётся resilience fallback и не является владельцем 30-секундного таймера.

Аварийная схема:

```text
current VPN probe = FAIL
        ↓
fenced confirmation = FAIL
        ↓
обычный WAN жив?
   ├─ нет → STOP, VPN не трогаем
   └─ да
        ↓
same logical VPN: fresh endpoint recovery
hard budget ≤ 20 s
        ↓
VPN восстановлен?
   ├─ да → post-check, DONE
   └─ нет + rollback known-safe
        ↓
canonical Best Server
full sweep → первый fully measured Eligible replacement
        ↓
apply measured candidate
        ↓
post-check
```

Если rollback получил `FAILED` или `UNKNOWN`, следующая automatic mutation запрещается.

Quality optimization отделена от аварийного recovery. Для рабочего, но деградирующего VPN FreeNet использует stability-first оценку: strict liveness/service/stall gates обязательны, затем приоритет имеют application RTT, jitter и VPN RTT; дополнительная скорость является вторичным сигналом. Reachable degradation учитывается по severity (RTT/service quality), а не одинаковыми flat-strikes. Тяжёлая деградация может раньше запустить measured comparison, но сама по себе никогда не переключает VPN: новый профиль должен пройти full measurement, Eligibility и materially-better hysteresis.

---

# 10. DNS и маршрутизация

DNS выбирается отдельно.

Routing строится только на явных правилах:

- `DIRECT`;
- `VPN`;
- `BLOCK`.

Изменения проходят через plan → candidate validation → controlled apply → post-check → rollback.

Raw ручное редактирование `04_outbounds.json`, DNS/routing JSON или system shell не является обычным способом эксплуатации FreeNet.

---

# 11. Automation и расписания

Control Center является владельцем FreeNet-managed automation:

- AUTO VPN health/recovery;
- endpoint refresh;
- subscription refresh;
- GeoData;
- проверка обновлений FreeNet;
- backup jobs.

FreeNet изменяет только свой managed cron block и сохраняет посторонние cron-задачи.

---

# 12. Обновление FreeNet

Обновляться следует через FreeNet Control Center.

Update flow:

1. проверка доступной версии;
2. release manifest / SHA-256;
3. backup/snapshot;
4. candidate install;
5. reconnect;
6. version/health acceptance;
7. при ошибке — rollback/recovery.

FreeNet Journal фиксирует timestamp запуска и terminal result обновления, поэтому порядок `VPN event → update → AUTO event` можно восстановить по времени.

Не следует вручную заменять бинарники поверх штатного updater, если Control Center способен выполнить обновление сам.

---

# 13. Что делать при ошибке

### Bootstrap остановился на `NEEDS_REVIEW`

Не ставить вручную Xray/XKeen «для помощи». Это означает, что состояние не соответствует безопасному clean/existing сценарию.

### `ROLLBACK FAILED` или `ROLLBACK UNKNOWN`

**STOP.** Не запускать ту же mutation повторно до установления фактического состояния.

### Не скачивается release / manifest

Сначала определить PRIMARY ERROR: DNS, HTTPS, GitHub asset, manifest, SHA mismatch и т. п. Не делать blind retry как способ «починки».

### Интернет есть, VPN нет

В нормальной эксплуатации этим занимается AUTO VPN/Control Center. SSH нужен только если для следующего решения отсутствует runtime-факт, который FreeNet пока не умеет собрать сам.

### Нужна read-only диагностика

Есть `doctor.sh`:

```sh
/opt/bin/curl -fLsS https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/doctor.sh -o /tmp/freenet-doctor.sh && /opt/bin/sh /tmp/freenet-doctor.sh
```

Doctor не является обязательным preflight обычной установки.

---

# 14. После установки

Проверить в Control Center:

- FreeNet и Xray online;
- subscription настроена;
- текущий VPN определяется;
- Current VPN check завершается;
- ручной `↻` получает список и RTT;
- Best Server завершается;
- DNS mode соответствует выбранной схеме;
- routing rules соответствуют явной политике;
- Automation включена только там, где вы её действительно выбрали;
- Journal показывает события;
- update check работает.

Отдельный reboot acceptance полезен после первоначальной установки: после перезагрузки должны снова подняться Entware, XKeen/Xray и FreeNet, а выбранные VPN/DNS/routing настройки — сохраниться.

---

# 15. Что FreeNet не делает

FreeNet не должен:

- публиковать subscription URL, VLESS UUID или Reality credentials;
- переустанавливать исправный existing stack «на всякий случай»;
- угадывать partial/unknown state;
- выполнять global `opkg upgrade`;
- автоматически придумывать routing policy;
- скрывать неудачный rollback;
- считать green CI заменой real-router acceptance.

---

# 16. Структура установки

Основные файлы:

| Путь | Назначение |
| --- | --- |
| `/opt/sbin/xray` | Xray engine |
| `/opt/sbin/xkeen` | XKeen |
| `/opt/sbin/xkeen-ui` | XKeen UI |
| `/opt/sbin/freenet-ui` | FreeNet Control Center backend/UI |
| `/opt/etc/xray/configs/` | Xray config set |
| `/opt/etc/freenet/freenet.conf` | несекретные настройки FreeNet |
| `/opt/etc/xray/blanc_subscription.url` | локальный subscription secret |
| `/opt/lib/freenet/` | transactional helpers |
| `/opt/backups/` | setup/update transaction backups |

Не копируйте секретные файлы из этих каталогов в issue/PR.

---

# 17. Подробная документация

- [`docs/INSTALL-FROM-SCRATCH-RU.md`](docs/INSTALL-FROM-SCRATCH-RU.md) — clean Entware;
- [`docs/INSTALL-EXISTING-STACK-RU.md`](docs/INSTALL-EXISTING-STACK-RU.md) — существующий XKeen/Xray;
- [`docs/ARCHITECTURE-RU.md`](docs/ARCHITECTURE-RU.md) — архитектура;
- [`docs/RECOVERY-RU.md`](docs/RECOVERY-RU.md) — recovery/rollback;
- [`docs/UPSTREAM-PINS-RU.md`](docs/UPSTREAM-PINS-RU.md) — pinned upstream и SHA-256;
- [`docs/ROUTING-REFERENCE-RU.md`](docs/ROUTING-REFERENCE-RU.md) — реальные reference-сценарии Routing для Владлинк/Ростелеком без секретов;
- [`docs/providers/BLANCVPN-RU.md`](docs/providers/BLANCVPN-RU.md) — provider integration.

---

# 18. Разработка и состояние проекта

Постоянные реестры:

- [Дорожная карта — Issue #5](https://github.com/VoltickVL/FreeNet-Router/issues/5)
- [Журнал изменений — Issue #7](https://github.com/VoltickVL/FreeNet-Router/issues/7)

GitHub является source of truth для кода, PR, CI и releases. Journal хранит факты завершённых циклов, а Roadmap — незакрытые продуктовые gates и следующие этапы.
