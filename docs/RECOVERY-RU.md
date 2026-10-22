# FreeNet Router — восстановление и аварийные действия

Этот документ предназначен для случаев, когда installer/update/UI завершились ошибкой.

## Главное правило

После failed install/update **не повторять mutation вслепую**.

Сначала определить:

1. primary error;
2. текущий process/listener state;
3. изменённые файлы;
4. rollback result;
5. Xray validity;
6. фактический VPN endpoint/profile.

## Backup

Installer создаёт timestamped каталоги:

```text
/opt/backups/freenet-install-YYYYMMDD-HHMMSS
```

Backup содержит только то, что нужно для rollback FreeNet-owned state и контрольных проверок. Credential-bearing данные не должны выводиться в публичные логи.

## Installer rollback

Если ошибка происходит после начала mutation, installer пытается восстановить:

- `freenet-ui`;
- init script;
- FreeNet manager;
- `vpn` helper;
- updater;
- local FreeNet config;
- subscription URL file;
- crontab.

Xray routing/DNS installer v0.2.x не должен переписывать, поэтому rollback этих файлов обычно не требуется; вместо этого installer сравнивает их SHA до/после.

## После `ROLLBACK ERROR`

Не запускать install/update ещё раз.

Нужно read-only проверить:

```sh
ps w | grep -E '[x]ray run|[x]keen-ui|[f]reenet-ui'
netstat -lntp | grep -E ':1000 |:1001 |:222 '
/opt/sbin/xray run -test -confdir /opt/etc/xray/configs
```

и только затем решать, что восстанавливать.

## FreeNet UI не отвечает

Проверить:

```sh
pidof freenet-ui
netstat -lntp | grep ':1001 '
curl -fsS http://<LAN-IP>:1001/healthz
curl -fsS http://127.0.0.1:1001/healthz
```

Ожидаемая схема v0.2.1+:

- `<LAN-IP>:1001` — LAN access;
- `127.0.0.1:1001` — local proxy/CrazeDNS;
- нет `0.0.0.0:1001` и `:::1001`.

## UI завис в состоянии «Переключаем VPN»

Начиная с v0.2.2 UI должен восстанавливать состояние через polling `/api/status`, даже если HTTP POST оборвался при restart Xray/XKeen.

Фактическая проверка:

```sh
curl -fsS http://<LAN-IP>:1001/api/status | jq .
```

Если `busy=false`, updater lock отсутствует, профиль/endpoint уже изменились, а браузер всё ещё показывает busy — это UI defect, а не повод повторно переключать VPN.

## Xray invalid

Не restart/reinstall вслепую.

Проверить полный config:

```sh
XRAY_LOCATION_ASSET=/opt/etc/xray/dat \
/opt/sbin/xray run -test -confdir /opt/etc/xray/configs
```

Нужен точный config error до любого apply.

## Subscription

Файл subscription:

```text
/opt/etc/xray/blanc_subscription.url
```

Не выводить его содержимое в чат, GitHub Issue или diagnostic bundle.

Если ключ-ссылка была раскрыта, замените/перевыпустите её у provider и затем обновите локально через FreeNet settings.

## Пароль FreeNet

Password auth и команда `freenet reset-password` находятся в Roadmap #5 и пока не считаются реализованными в v0.2.x.

После реализации recovery должен позволять сбросить только FreeNet auth без удаления VPN/Xray.

## Переустановка

Переустановка допустима только когда:

- runtime state известен;
- предыдущий rollback successful либо delta понятен;
- Xray config валиден;
- новая версия действительно исправляет подтверждённую причину.

Сам факт, что интернет или web UI открывается, не доказывает корректный rollback.

## Подтверждение уже восстановленного приложения

Начиная с `v0.8.0-beta.21`, проверенный `self_update.sh` поддерживает отдельную процедуру для состояния `ROLLBACK_FAILED / FAILED_UNKNOWN`. Она применима, когда файлы предыдущего выпуска уже восстановлены и его веб-панель работает, но старый обновлятор не смог подтвердить результат. Эта процедура не восстанавливает файлы и не заменяет диагностику неисправного приложения.

Используйте обновлятор из точного официального выпуска, предварительно проверив его SHA-256 по `SHA256SUMS` того же выпуска. Запущенная старая копия обновлятора не получает новый алгоритм, когда обновление заменяет файл на диске. Не редактируйте установленный скрипт, квитанцию операции или блокировки вручную.

Сначала выполните проверку без изменения постоянного состояния. Здесь `VERIFIED_UPDATER` — абсолютный путь к отдельно загруженному и проверенному `self_update.sh`:

```sh
FREENET_UPDATE_BETA_OPT_IN=yes /bin/sh "$VERIFIED_UPDATER" reconcile-rollback-plan
```

План требует одновременно:

- точную исходную квитанцию `FAILED_UNKNOWN` и относящийся к ней каталог резервной копии;
- совпадение всех файлов приложения с резервной копией и SHA-256 точного официального выпуска `FROM_VERSION`;
- сохранность защищённых настроек и конфигурации Xray, отсутствие незавершённого VPN checkpoint;
- работающую веб-панель исходного выпуска с проверенным процессом, исполняемым файлом, портом, `/healthz` и `/versionz`;
- неизменную идентичность работающего Xray в течение проверки;
- обе принадлежащие завершившемуся обновлению блокировки, отсутствие их активного владельца и другого обновлятора.

Успешный план выдаёт `SUCCESS=yes`, `READY=yes`, `MUTATION=NONE` и `RECEIPT_SHA256`. Подставьте ровно полученный хеш в следующую команду:

```sh
FREENET_UPDATE_BETA_OPT_IN=yes /bin/sh "$VERIFIED_UPDATER" reconcile-rollback "$RECEIPT_SHA256"
```

Команда заново проверяет доказательства, сохраняет исходную квитанцию, манифест, результаты проверки и подготовленную конечную квитанцию в приватном каталоге `/opt/var/lib/freenet/update-rollback-reconciliation/`. Только после повторного подтверждения она атомарно записывает `STATE=FAILED` и `ROLLBACK_STATE=SUCCESS`. Исходные `FROM_VERSION`, `TARGET_VERSION`, `PRIMARY_ERROR` и `BACKUP_DIR` сохраняются: обновление осталось неудачным, подтверждён только возврат приложения к предыдущему выпуску.

Обе блокировки остаются на месте. После успешного завершения команды проверьте сохранённый результат, затем используйте штатное снятие зависшей блокировки в панели FreeNet. При отказе проверки исходная квитанция `FAILED_UNKNOWN` и блокировки сохраняются; повторная установка или ручное удаление блокировок не являются следующим шагом.

После штатного снятия блокировки выполняйте новый `plan` и `apply` тем же SHA-256-проверенным обновлятором целевого выпуска. Проверяйте работу VPN и свежие измерения отдельно: подтверждение восстановления веб-панели само по себе не подтверждает качество интернета.
