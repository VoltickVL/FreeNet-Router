# Установка FreeNet Router на чистый Ultra

Главная пользовательская инструкция находится в корневом [README.md](../README.md). Этот файл фиксирует технический clean-install contract.

## Текущий безопасный путь

На KeeneticOS 5.01 **системный SSH администратора открывает `(config)>`, а не Linux/POSIX shell**. Команда `curl ... | ssh -tt admin@... 'exec /bin/sh'` из предыдущей редакции README была ошибочной: текст Stage-0 отправлялся в конфигурационный CLI и приводил к `incorrect request` / `ndm: failed to initialize`. Никогда не используйте этот способ установки.

~~~text
веб-интерфейс роутера
  → EXT + OPKG + смонтированный EXT4 (существующие данные не форматировать)
  → штатно установить/подтвердить Entware
  → доступ к отдельному Entware SSH / Linux shell
  → read-only /opt/bin/opkg print-architecture
  → FreeNet bootstrap в подтверждённом Entware shell
  → Browser Setup
~~~

Без Entware shell или при частичном/неизвестном состоянии — **STOP**. Чистая установка одной командой через stock SSH в текущей версии **не поддерживается**; будущий browser-first путь фиксируется в Roadmap #5.


## Поддержанные Ultra mapping

- Keenetic Ultra **KN-1811** → AArch64 → `aarch64-k3.10`;
- Netcraze Ultra **NC-1812** → AArch64 → `aarch64-k3.10`;
- legacy Keenetic Ultra **KN-1810** → MIPSel → `mipselsf-k3.4`.

Unknown model = STOP для Stage-0 Ultra mapping. **Giga и другие не перечисленные модели не определяются автоматически через этот Ultra-only helper**; готовый Entware на них может использовать обычный FreeNet bootstrap после проверки фактической архитектуры.

## До команды

В веб-интерфейсе:

1. установить поддержку EXT filesystem;
2. установить **Open Package support / OPKG**;
3. установить **SSH server**;
4. если носитель пустой — штатно подготовить EXT4; **если Entware/данные уже есть — ничего не форматировать**;
5. убедиться, что раздел mounted;
6. оставить на время первого install ровно один mounted EXT4, чтобы Stage-0 не выбирал диск по догадке.

Online `opkg disk <disk> <url>` flow рассчитан на актуальную KeeneticOS/Netcraze OS с URL option из ветки 4.2+.

## Разные SSH и запуск из действующего Entware

- **Stock SSH**: локальный администратор, обычно `192.168.1.1:22`, приглашение `(config)>`. Это системный конфигурационный CLI, не Linux shell. Ни `curl`, ни `sh`, ни shell-пайпы в нём не работают. Telnet на порту 23 также не подходит.
- **Entware SSH**: отдельный Linux/BusyBox shell, часто `root@192.168.1.1:222`, если он настроен и запущен. Это не гарантированное значение порта/учётной записи.

Пример **read-only проверки** с компьютера, подключённого к локальному Wi-Fi нового роутера:

~~~sh
ssh -p 222 root@192.168.1.1
~~~

В **Entware shell** (не `(config)>`):

~~~sh
test -x /opt/bin/opkg && /opt/bin/opkg print-architecture
~~~

Пока OPKG не подтвердил работоспособность, **не запускать установку**. Системная страница «OPKG» и каталоги `bin/etc` на EXT4 сами по себе не доказывают исправность.

При исправном OPKG и работающих с роутера WAN/DNS и HTTPS/GitHub:

~~~sh
/opt/bin/opkg update && /opt/bin/opkg install ca-bundle curl && /opt/bin/curl -fLsS https://github.com/VoltickVL/FreeNet-Router/releases/latest/download/bootstrap.sh -o /tmp/freenet-bootstrap.sh && /opt/bin/sh /tmp/freenet-bootstrap.sh
~~~

Если роутер не разрешает `github.com`, остановиться и исправить WAN/DNS штатно, без ослабления TLS и повторных mutation. Дополнительный способ доставки `bootstrap.sh` с Mac через **Entware SSH** описан в [README.md](../README.md#5-установка-freenet-когда-entware-уже-работает); последующие release assets всё равно скачиваются с роутера.

Для чистой установки **сначала подготовьте Entware штатным способом KeeneticOS/Netcraze**, затем повторите read-only проверку. Не используйте системный SSH как вход в `/bin/sh`.


## Stage-0 contract

`scripts/bootstrap_router_ultra.sh` — shell-helper **только для среды с реально доступным POSIX shell**; запуск через системный SSH `(config)>` не поддерживается. Его внутренний контракт (не инструкция для пользователя):

1. при существующем `/opt/bin/opkg` не переустанавливает Entware;
2. иначе читает router model;
3. выбирает только известный Ultra architecture mapping;
4. читает `show media`;
5. требует ровно один mounted EXT4;
6. вызывает штатный `opkg disk <UUID>:/ <Entware URL>`;
7. ждёт не просто файл, а реально работающий `opkg print-architecture`;
8. проверяет architecture;
9. ставит только `ca-bundle curl` для handoff;
10. скачивает опубликованный `bootstrap.sh`;
11. передаёт управление product bootstrap.

Global `opkg upgrade` запрещён.

## FreeNet core contract после Entware

`ENTWARE_ONLY`:

1. targeted Entware dependencies;
2. pinned XKeen + Xray;
3. upstream SHA-256 validation;
4. backup;
5. XKeen registration с autostart/proxy-DNS off до Browser Setup;
6. Xray validation;
7. post-apply read-only classification;
8. app-фаза только после `READY_EXISTING_STACK`;
9. FreeNet + transactional helpers;
10. LAN-only Control Center на `http://<LAN-IP>:1001/`.

**XKeen UI = optional/unmanaged.**

- clean install его не скачивает и не ставит;
- отсутствие XKeen UI не влияет на readiness;
- уже существующий XKeen UI сохраняется as-is.

Partial/contradictory core = `NEEDS_REVIEW` → STOP.

## Entware SSH

Для текущей версии Entware shell является **точкой запуска FreeNet bootstrap**, если автоматическая native clean-install процедура в продукте ещё не реализована. Типичный порт `222` — не гарантия. Начальные пароли не размещайте в журнале/скриншотах; заданные заводские/простые пароли нужно сменить. Нет shell или OPKG = STOP без предположений.

## Browser Setup

Дальше только через Control Center:

1. локальная авторизация;
2. VPN subscription;
3. свежий список профилей;
4. выбор/проверка VPN;
5. DNS;
6. routing DIRECT/VPN/BLOCK;
7. Automation;
8. final readiness acceptance.

Subscription URL, VLESS UUID и Reality credentials не публикуются и не копируются между роутерами через GitHub/чат.

## Reboot acceptance

После первоначального setup выполняется контролируемая перезагрузка. Должны автоматически вернуться Entware, XKeen/Xray и FreeNet, сохранив принятые VPN/DNS/routing настройки.

При FAIL не запускать mutation повторно вслепую. `ROLLBACK FAILED/UNKNOWN` = STOP.
