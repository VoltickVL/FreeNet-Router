# FreeNet Router

**FreeNet Router** — локальный Control Center для Keenetic/Netcraze с Entware, XKeen и Xray. Он управляет VPN, DNS, маршрутизацией, автоматическим восстановлением VPN, подпиской и обновлениями через браузер. SSH нужен только для первичного bootstrap или аварийной диагностики.

Целевой путь для нового устройства:

~~~text
Keenetic / Netcraze
        ↓
Веб-интерфейс: EXT4, OPKG, системные компоненты
        ↓
Сначала штатно подготовить Entware /opt
        ↓
Проверить рабочий /opt/bin/opkg в Entware shell
        ↓
FreeNet bootstrap запускается ТОЛЬКО в Entware shell
        ├─ clean Entware → pinned XKeen + Xray
        ├─ existing valid XKeen/Xray → preserve
        └─ partial/unknown core → STOP без догадок
        ↓
FreeNet Control Center в браузере
        ↓
Subscription → VPN → DNS → Routing → Automation → Acceptance
~~~

> **Важно для KeeneticOS 5.x:** встроенный SSH администратора открывает конфигурационную консоль `(config)>`, а не Linux shell. Передавать туда shell-скрипты через SSH/pipe **нельзя**. На некоторых устройствах уже работает Entware SSH (часто порт 222) — это отдельный сервис. Наличие EXT4 и строки «OPKG» в веб-интерфейсе ещё не доказывает, что `/opt/bin/opkg` исправен. Неподдержанный путь установки = STOP без догадок.


> **На USB не ставится отдельная операционная система.** Роутер продолжает работать под KeeneticOS/Netcraze OS. Накопитель используется для Entware и каталога `/opt`, где затем находятся XKeen, Xray, FreeNet и их локальные данные.

---

## 1. Что именно устанавливает FreeNet

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

[Xray-core](https://github.com/XTLS/Xray-core) — VPN/proxy engine. Он обрабатывает VLESS/Reality, outbounds, DNS-out и routing-конфигурацию.

FreeNet использует pinned-версию Xray и проверяет загружаемый upstream-артефакт по SHA-256. Актуальный pin находится в [`config/upstream-pins.env`](config/upstream-pins.env).

### XKeen

[XKeen](https://github.com/jameszeroX/XKeen) интегрирует Xray с Keenetic/Netcraze: сервис, запуск, DNS/proxy integration и окружение роутера.

На clean install FreeNet регистрирует XKeen в безопасном pre-setup состоянии: autostart и proxy-DNS не включаются до того, как Browser Setup примет сетевую политику.

---

# Установка с нуля — Ultra

## 2. Модели первого acceptance

Stage-0 автоматически поддерживает:

| Модель | Архитектура Entware | Installer |
| --- | --- | --- |
| **Keenetic Ultra KN-1811** | AArch64 | `aarch64-k3.10` |
| **Netcraze Ultra NC-1812** | AArch64 | `aarch64-k3.10` |
| Keenetic Ultra KN-1810 | MIPSel | `mipselsf-k3.4` |

Для неизвестной модели Stage-0 **STOP**, а не пытается угадать архитектуру.

Online-install через `opkg disk <disk> <url>` требует актуальной KeeneticOS/Netcraze OS с CLI URL option (ветка 4.2+). На первом реальном acceptance используем текущую стабильную прошивку.

---

## 3. Что сделать в веб-интерфейсе роутера

Перед командой FreeNet:

1. Подключить USB-флешку/SSD к роутеру.
2. В **Компоненты системы** установить:
   - поддержку файловых систем EXT;
   - **Open Package support / Поддержка открытых пакетов (OPKG)**;
   - **SSH server / Сервер SSH**.
3. Если накопитель новый и пустой — штатно подготовить раздел EXT4. **Не форматировать существующий EXT4/Entware с данными**: сначала проверить состояние OPKG, чтобы не потерять рабочий стек.
4. После форматирования убедиться, что EXT4-раздел смонтирован.
5. На время первой установки желательно иметь **ровно один mounted EXT4-раздел**. Если их несколько, FreeNet Stage-0 остановится и ничего не выберет сам.
6. Убедиться, что локальная учётная запись администратора имеет доступ к CLI.

**Форматирование удаляет данные на выбранном разделе.**

Поддержка OPKG в компонентах и смонтированный EXT4 — это подготовка среды, **не подтверждение работающего Entware**. Для чистого роутера сначала требуется штатно установить/активировать Entware через поддержанный механизм KeeneticOS/Netcraze OS. Не запускайте необоснованные операции форматирования, повторной установки или смены SSH-портов.

Официальные справочные страницы:

- Netcraze Ultra NC-1812: [Entware на USB](https://support.netcraze.ru/ultra/nc-1812/en/20980-installing-the-entware-repository-on-a-usb-drive.html);
- KeeneticOS 4.2: CLI `opkg disk ... <url>` поддерживает загрузку remote OPKG installer;
- форматирование EXT4 можно выполнить штатно в интерфейсе роутера.

---

## 4. Системный CLI ≠ Entware shell

У роутера бывают **два разных SSH-сервиса**, и путать их нельзя.

**Системный SSH KeeneticOS/Netcraze OS:** локальный администратор (например, `admin`), часто порт `22`. Его приглашение:

~~~text
(config)>
~~~

Это **конфигурационный CLI, не POSIX shell**. Он не умеет выполнять `curl`, `sh`, `exec /bin/sh` и конвейеры shell. Наша прежняя инструкция `curl ... | ssh -tt ... 'exec /bin/sh'` здесь была ошибочной: на KeeneticOS 5.01 текст скрипта попадал в CLI, после чего появлялись `incorrect request` и `ndm: failed to initialize`. **Не повторяйте эту команду** ни с Mac, ни с Windows. Порт 23 (Telnet) также не является Linux shell и не должен использоваться для установки.

**Entware SSH:** отдельная сессия Linux/BusyBox, как правило `root`, порт `222`, если сервис установлен и запущен. Точный порт зависит от конфигурации и не гарантирован. Не подбирайте пароли, не открывайте SSH в интернет и не отключайте проверку SSH host key. Удалённая консоль только для первичной установки/аварийной диагностики.

## 5. Установка FreeNet, когда Entware **уже работает**

Сначала подтвердите фактическое состояние **без каких-либо изменений**. Из терминала компьютера, подключённого к локальной сети нового роутера (пример для типичного Entware SSH, а **не** системного SSH):

~~~sh
ssh -p 222 root@192.168.1.1
~~~

В появившемся **Entware** shell (например, `~ #`, не `(config)>`) выполните:

~~~sh
test -x /opt/bin/opkg && /opt/bin/opkg print-architecture
~~~

Команда должна завершиться успешно и показать архитектуру Entware. Если порт 222 не отвечает, `/opt/bin/opkg` отсутствует, команда выдаёт ошибку или вы видите `(config)>` — **STOP**: никакой переустановки Entware и запуска bootstrap наугад.

Только после успешного read-only preflight и при доступном с роутера GitHub/Entware repository запускайте в **Entware shell**:

~~~sh
/opt/bin/opkg update && /opt/bin/opkg install ca-bundle curl && /opt/bin/curl -fLsS https://github.com/VoltickVL/FreeNet-Router/releases/latest/download/bootstrap.sh -o /tmp/freenet-bootstrap.sh && /opt/bin/sh /tmp/freenet-bootstrap.sh
~~~

Этот сценарий не переустанавливает работоспособный Entware. Продуктовый bootstrap проверяет XKeen/Xray и сохраняет целый существующий стек. Partial/unknown core = STOP. Не вводите команду в системном `(config)>`.

### Если загрузка `bootstrap.sh` с роутера не работает, а с Mac работает

Можно скачать **только точку входа** на macOS, затем передать её по **Entware SSH** после успешного read-only preflight:

~~~sh
curl -fsSL -o "$HOME/Downloads/freenet-bootstrap.sh" https://github.com/VoltickVL/FreeNet-Router/releases/latest/download/bootstrap.sh
test -s "$HOME/Downloads/freenet-bootstrap.sh" && ssh -p 222 root@192.168.1.1 '/opt/bin/opkg print-architecture >/dev/null && /opt/bin/sh -s' < "$HOME/Downloads/freenet-bootstrap.sh"
~~~

Вторая команда запускает installer **только в подтверждённом Entware shell**. Это не offline-install: далее FreeNet всё равно скачивает свои release-артефакты и целевые Entware-пакеты **с роутера**. Если там `Could not resolve host: github.com` или нет доступа к Entware feeds, **STOP**: сначала штатно исправьте подключение WAN/DNS в веб-интерфейсе; не отключайте HTTPS и не повторяйте mutation вслепую.

Для Windows не переносите Linux-команды в `(config)>`: подключитесь к существующему Entware SSH и выполните показанный выше Entware-level bootstrap уже в его shell.

## 6. Чистый роутер, Entware **ещё нет** — STOP до подготовки /opt

1. Через веб-интерфейс убедитесь, что установлены компоненты EXT/OPKG, накопитель смонтирован и интернет/DNS исправны.
2. Используйте штатную процедуру KeeneticOS/Netcraze OS для установки/активации Entware на выбранном EXT4; не форматируйте носитель с существующими данными.
3. Когда Entware shell доступен, проверьте `/opt/bin/opkg print-architecture` и переходите к **разделу 5**.
4. Пока доступен только системный `(config)>`, у FreeNet **нет подтверждённой однокомандной установки из этого CLI**. Не подменяйте её запуском `bootstrap_router_ultra.sh` по `ssh -tt`. Автоматический browser-first clean-install остаётся отдельной продуктовой задачей.

Скрипт `scripts/bootstrap_router_ultra.sh` — shell-helper для среды, в которой **реально существует POSIX shell**; это не команда для системного SSH KeeneticOS. В нём автоматически сопоставлены только Ultra **KN-1811 / NC-1812 / KN-1810**. **Giga и другие модели не входят в Ultra-only auto mapping**: определять для них архитектуру по догадке нельзя. При существующем и исправном Entware используется обычный bootstrap из раздела 5.

**Безопасность:** не публикуйте пароли, subscription URL, UUID и Reality-ключи в скриншотах терминала и GitHub. При `ROLLBACK FAILED/UNKNOWN` — STOP и read-only диагностика.

---

## 6.1. Как FreeNet классифицирует core после Entware

### `ENTWARE_ONLY`

Есть исправный Entware, но XKeen/Xray ещё не установлены.

FreeNet:

1. ставит только необходимые Entware dependencies;
2. скачивает pinned XKeen и Xray;
3. проверяет upstream SHA-256;
4. делает backup;
5. устанавливает core;
6. регистрирует XKeen с безопасными pre-setup defaults;
7. валидирует Xray;
8. повторно делает read-only classification;
9. только после `READY_EXISTING_STACK` устанавливает FreeNet app/helper layer.


### `READY_EXISTING_STACK`

Есть зарегистрированный XKeen, Xray, Xray configs и successful `xray run -test`.


FreeNet не должен сбрасывать:

- текущие VLESS/Reality credentials;
- subscription secret;
- Xray-конфиги;
- выбранный VPN;
- посторонние cron-задачи.

### `NEEDS_REVIEW`

Обнаружен реальный partial/contradictory **core**: например, нет XKeen init, отсутствует Xray или его configs не проходят validation.

Это **STOP**. FreeNet не пытается «доделать на глаз».

### `NO_ENTWARE` / `UNSUPPORTED_ARCH`

Обычный Entware-level bootstrap не начинается. Если Entware отсутствует, сначала используйте штатную подготовку OPKG в веб-интерфейсе и подтвердите исправную Entware shell согласно разделу 6. Stock Keenetic CLI не исполняет Stage-0 shell-скрипт.

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

## Canonical VPN-ping и приоритет качества

FreeNet использует единый смысл latency-метрик во всех пользовательских и automatic flow.

- **VPN-пинг** — fixed-IP HTTPS RTT через фактический VPN-тракт без DNS. Это не ICMP echo непосредственно до IP VPN-сервера. Метрика показывает базовую задержку работающего туннеля + exit path.
- **Отклик сайтов** — median HTTPS response latency именованных origin через VPN. Это наиболее прямой показатель того, насколько отзывчиво ощущаются сайты и приложения.
- **Стабильность** — spread/jitter серии application RTT samples: разница между самым быстрым и самым медленным валидным sample. Чем меньше, тем ровнее VPN.
- **Скорость VPN** — измеренная throughput/capacity. После прохождения strict minimum она имеет меньший приоритет, чем задержка и стабильность. Throughput plateau адаптивный: baseline = 4×40 MB (160 MB aggregate); если baseline показывает >=180 Mbps, FreeNet повторяет финальный speed measurement на 4×80 MB (320 MB aggregate). Поэтому 300 Mbps-class path получает примерно вдвое более длинное финальное окно, а 100 Mbps-class path не тратит время на ненужный второй проход.

Canonical priority:

`liveness / services / no stalls → отклик сайтов → стабильность → VPN-пинг → скорость`.

Broad full-pool discovery остаётся быстрым: каждый logical profile сначала получает один одинаковый fixed-IP HTTPS sample. Competitive Top-10 затем получает canonical confirmed measurement: bounded warm-up + 5 samples, median и spread. Current VPN, deep Best Server и AUTO VPN используют тот же confirmed VPN RTT method; deep application RTT также использует warm-up + 5 samples.

Небольшая разница RTT сама по себе не считается преимуществом. Если delta укладывается в measured spread/noise двух сравниваемых VPN, она обнуляется как measurement noise и не может самостоятельно вызвать AUTO switch или Best Server recommendation.

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

Quality optimization отделена от аварийного recovery. Для рабочего, но деградирующего VPN FreeNet использует stability-first оценку: strict liveness/service/stall gates обязательны; затем веса materially-better comparison распределены как application RTT **35%**, stability spread **30%**, confirmed VPN RTT **25%**, throughput **10%**. Latency gain внутри measured noise floor не учитывается. Reachable degradation учитывается по severity (RTT/service quality), а не одинаковыми flat-strikes. Тяжёлая деградация может раньше запустить measured comparison, но сама по себе никогда не переключает VPN: новый профиль должен пройти full measurement, Eligibility и materially-better hysteresis.

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

### Journal для долговременного анализа

Journal хранит bounded canonical history, а не бесконечный лог:

- до **15 000 canonical событий** в пользовательской выборке;
- исходные history-файлы строго ограничены **20 000 строками**;
- UI по умолчанию показывает 100 строк и позволяет выбрать **50 / 100 / 200 / 500**;
- доступны страницы, поиск, фильтры по событию/результату и периоды **Сегодня / 24 часа / 7 дней / произвольный интервал**;
- summary/statistics считаются по **всей отфильтрованной выборке**, а не только по текущей странице;
- отображаются границы retained archive;
- выбранный диапазон можно выгрузить как **UTF-8 CSV** для внешнего анализа;
- export использует тот же canonical read-only Journal и не выполняет runtime mutation.

Journal предназначен в том числе для сравнения natural AUTO VPN behavior между длительными интервалами и разными роутерами без обязательного SSH. Он не является местом для subscription URL, UUID, Reality credentials, паролей или иных секретов.

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
