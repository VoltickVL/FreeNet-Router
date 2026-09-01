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