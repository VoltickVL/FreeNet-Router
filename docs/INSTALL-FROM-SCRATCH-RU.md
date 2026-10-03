# Установка FreeNet Router с чистого Entware

Главная пошаговая инструкция теперь находится в корневом [README.md](../README.md). Этот файл фиксирует технический clean-install contract.

## До FreeNet

На роутере должны быть готовы только:

- USB-раздел EXT4;
- компонент Open Package support / OPKG;
- установленный Entware;
- рабочие `/opt` и `/opt/bin/opkg`.

Отдельная ОС на USB не ставится.

Официальные инструкции:

- Keenetic: https://support.keenetic.com/titan/kn-1811/en/20980-installing-the-entware-repository-on-a-usb-drive.html
- Netcraze: https://support.netcraze.ru/giga/nc-1012/ru/20980-installing-the-entware-repository-on-a-usb-drive.html
- EXT4: https://support.keenetic.com/hero/kn-1011/en/21024-using-the-ext4-file-system-on-usb-drives.html

Архив Entware выбирается по официальной инструкции конкретной модели, а не по памяти.

## Одна команда после Entware

```sh
/opt/bin/opkg update && /opt/bin/opkg install ca-bundle curl && /opt/bin/curl -fLsS https://github.com/VoltickVL/FreeNet-Router/releases/latest/download/bootstrap.sh -o /tmp/freenet-bootstrap.sh && /opt/bin/sh /tmp/freenet-bootstrap.sh
```

Bootstrap сам доставляет недостающие userland tools через targeted `opkg install`. Global `opkg upgrade` запрещён.

## Clean-install contract

Ожидаемый режим:

```text
MODE=ENTWARE_ONLY
```

FreeNet:

1. определяет архитектуру;
2. проверяет release SHA-256;
3. ставит только недостающие Entware dependencies;
4. скачивает pinned XKeen/Xray/XKeen UI и проверяет upstream SHA-256;
5. делает backup;
6. устанавливает core;
7. регистрирует XKeen с autostart/proxy-DNS off до Browser Setup;
8. валидирует Xray;
9. устанавливает FreeNet и transactional helpers;
10. проверяет LAN-only Control Center;
11. выводит адрес `http://<LAN-IP>:1001/`.

Если обнаружен partial/contradictory stack, normal install не пытается его «доделать»:

```text
MODE=NEEDS_REVIEW
```

Это STOP до read-only разбора состояния.

## Browser Setup

Дальше через Control Center:

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

После первоначального setup полезна контролируемая перезагрузка. Должны автоматически вернуться Entware, XKeen/Xray и FreeNet, сохранив принятые VPN/DNS/routing настройки.

При FAIL не запускать mutation повторно вслепую. `ROLLBACK FAILED/UNKNOWN` = STOP.
