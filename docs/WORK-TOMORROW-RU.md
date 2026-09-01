# FreeNet на рабочем роутере — порядок на завтра

Цель — повторить рабочую HOME-схему без догадок и без ручного редактирования Xray.

## 1. Войти в Entware shell

Если на роутере уже работает Entware Dropbear на `222`:

```sh
ssh -p 222 root@<LAN-IP-роутера>
```

Если прямой Entware SSH не настроен, используйте штатный SSH Keenetic/Netcraze и затем:

```sh
exec /opt/usr/bin/sh
```

## 2. Сначала только read-only preflight

```sh
curl -fLsS https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/doctor.sh | sh
```

`doctor.sh` ничего не устанавливает, не перезапускает и не меняет cron/config/firewall.

### Если результат

```text
MODE=READY_EXISTING_STACK
```

На роутере уже есть валидные Entware + XKeen + Xray. Можно переходить к текущей установке FreeNet без переустановки XKeen/Xray.

### Если результат

```text
MODE=ENTWARE_ONLY
```

Есть Entware, но XKeen/Xray нет. **Не запускать current FreeNet installer вслепую.** Для такого состояния предназначен следующий слой P1 bootstrap из Roadmap #5.

### Если `NEEDS_REVIEW`, `NO_ENTWARE`, `UNSUPPORTED_ARCH`

Остановиться и разобрать факты до mutation.

## 3. Установка поверх уже готового XKeen/Xray

Только после `MODE=READY_EXISTING_STACK`:

```sh
curl -fLsS https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/install.sh | /opt/usr/bin/sh
```

Если router-local DNS не работает, используется bootstrap-вариант с внешним DNS из README.

Установщик:

- определяет архитектуру;
- скачивает direct GitHub Release assets;
- сверяет SHA-256;
- делает backup;
- сохраняет существующую subscription URL;
- ставит FreeNet UI/manager/updater;
- мигрирует только собственные cron-задачи;
- валидирует Xray;
- проверяет, что Xray `02/03/04/05` installer-ом не переписаны;
- при ошибке запускает rollback.

## 4. Acceptance после установки

Нужно подтвердить:

```text
FreeNet UI health/API: OK
LAN-only listener: OK
Xray config не изменён installer-ом: OK
```

После этого:

1. открыть `http://<LAN-IP>:1001/`;
2. проверить текущий профиль и endpoint;
3. сделать одно controlled переключение страны;
4. убедиться, что UI вернулся из busy-state;
5. проверить `xray run -test`;
6. проверить внешний VPN IP и отсутствие DNS leak;
7. только после этого считать WORK установленным.

## 5. Что завтра не делать

- не переустанавливать XKeen/Xray, если `doctor` показывает уже рабочий stack;
- не открывать `root:222`, FreeNet `1001` или XKeen UI `1000` напрямую в WAN;
- не копировать HOME `04_outbounds.json` целиком;
- не переносить HOME subscription URL через GitHub/чат;
- не делать повторный install после FAIL без read-only проверки rollback/current state.

## Roadmap

Полный путь `Entware-only -> XKeen/Xray/XKeen UI -> browser setup -> provider -> routing -> FreeNet` ведётся в GitHub Issue #5.
