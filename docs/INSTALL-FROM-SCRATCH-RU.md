# Установка FreeNet Router с нуля

Этот документ описывает **целевой** путь для нового Keenetic/Netcraze.

Сейчас стабильный FreeNet v0.2.x автоматизирует слой поверх уже работающего XKeen/Xray. Полный Entware-only bootstrap ведётся в Roadmap #5 и будет включён только после clean-room acceptance.

## Целевая модель

```text
USB + Entware/OPKG
        ↓
одна команда FreeNet
        ↓
проверка роутера
        ↓
установка/проверка XKeen + Xray + XKeen UI
        ↓
браузерный мастер FreeNet
        ↓
VPN provider / ключ-ссылка
        ↓
routing + DNS + automation + password
        ↓
validation + backup + ГОТОВО
```

## Шаг 1. Подготовить USB

Для OPKG/Entware нужен накопитель с файловой системой EXT. Для современных устройств рекомендуется **ext4**.

Официальная инструкция Netcraze:

- https://support.netcraze.ru/giga/nc-1012/en/20980-installing-the-entware-repository-on-a-usb-drive.html

Официальная инструкция Keenetic:

- https://support.keenetic.ru/eaeu/viva/kn-1910/en/20980-installing-the-entware-repository-on-a-usb-drive.html

Архив Entware зависит от архитектуры конкретной модели. Например, актуальная инструкция Netcraze для Giga NC-1012 указывает `aarch64-installer.tar.gz`; на части Keenetic используется `mipsel-installer.tar.gz`.

**Не выбирать архив по памяти.** Сверяйте модель/CPU с официальной инструкцией.

## Шаг 2. Включить Open Package support / OPKG

В компонентах роутера должен быть установлен компонент поддержки открытых пакетов/OPKG.

Типовой порядок из официальной документации:

1. подключить ext4 USB;
2. создать на нём каталог `install`;
3. положить туда Entware installer archive для нужной архитектуры;
4. выбрать этот накопитель в разделе OPKG/Менеджер пакетов;
5. дождаться успешной инициализации `/opt`.

## Шаг 3. Войти в Entware

На устройствах с отдельным SSH server компонентом Entware часто доступен на порту `222`:

```sh
ssh -p 222 root@<LAN-IP>
```

Если используется штатный SSH Keenetic/Netcraze на `22`, после входа можно перейти в Entware shell:

```sh
exec /opt/usr/bin/sh
```

Перед дальнейшей установкой смените дефолтный пароль Entware root, если он ещё не изменён.

## Шаг 4. FreeNet Doctor

Первая команда FreeNet всегда read-only:

```sh
curl -fLsS https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/doctor.sh | sh
```

Doctor сообщает один из режимов:

- `READY_EXISTING_STACK` — XKeen/Xray уже установлены и валидны;
- `ENTWARE_ONLY` — Entware есть, XKeen/Xray ещё нет;
- `NEEDS_REVIEW` — partial/broken state;
- `NO_ENTWARE` — OPKG/Entware не готовы;
- `UNSUPPORTED_ARCH` — архитектура не распознана.

Doctor не выполняет mutation.

## Шаг 5A. Если XKeen/Xray уже установлены

После `MODE=READY_EXISTING_STACK`:

```sh
curl -fLsS https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/install.sh | /opt/usr/bin/sh
```

Это текущий stable path.

## Шаг 5B. Если есть только Entware

После `MODE=ENTWARE_ONLY` **не устанавливать компоненты вручную вперемешку с FreeNet**.

P1 Roadmap автоматизирует:

1. обязательные Entware packages;
2. upstream XKeen stable installer;
3. Xray installation/registration;
4. GeoIP/GeoSite;
5. XKeen UI;
6. FreeNet baseline configs;
7. provider onboarding;
8. validation/rollback.

До выпуска и clean-room acceptance такого release используйте upstream XKeen инструкцию отдельно, а затем повторите `doctor.sh`.

Актуальный upstream XKeen installation reference:

- https://github.com/jameszeroX/XKeen/wiki/Порядок-установки
- https://github.com/jameszeroX/XKeen/wiki/Install-script

Upstream поддерживает автоматический выбор stable installer через `--stable`; FreeNet будет оркестрировать этот flow без `opkg upgrade` всего Entware и без blind mutation.

## Шаг 6. Provider / BlancVPN

После появления browser setup FreeNet попросит выбрать provider и ввести ключ-ссылку локально на роутере.

Для BlancVPN official reference:

- https://blancvpn.uno/ru/help/configuring-xkeen-windows

FreeNet не копирует credential-bearing конфигурацию из статьи и не хранит ключ-ссылку в GitHub.

## Шаг 7. Acceptance

Роутер не считается установленным только потому, что страница открылась.

Нужно подтвердить:

- Xray config validation PASS;
- Xray process alive;
- FreeNet health/API PASS;
- `dns-out` present;
- VPN profile/endpoint active;
- direct/VPN routing соответствует preset;
- внешний VPN IP соответствует выбранному региону;
- DNS leak test PASS;
- reboot/autostart PASS;
- FreeNet/XKeen UI доступны по выбранной LAN/CrazeDNS схеме;
- backup/rollback state известен.

## Clean-room router

Новый Keenetic Giga должен использоваться как clean-room acceptance device:

- не копировать HOME `/opt`;
- не копировать HOME `04_outbounds.json`;
- не переносить HOME secrets через GitHub/чат;
- ставить только через documented installer flow;
- фиксировать каждый недостающий prerequisite как defect installer-а или документации.
