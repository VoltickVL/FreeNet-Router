# Установка FreeNet Router на чистый Ultra

Главная пользовательская инструкция находится в корневом [README.md](../README.md). Этот файл фиксирует технический clean-install contract.

## Целевой путь

Для Keenetic Ultra / Netcraze Ultra обычная установка должна выглядеть так:

```text
веб-интерфейс роутера
  → системные компоненты EXT + OPKG + SSH server
  → один USB-раздел, штатно отформатированный в EXT4
  → одна команда с компьютера через stock SSH
  → FreeNet Stage-0
  → Entware
  → FreeNet bootstrap
  → Browser Setup
```

Ручное скачивание Entware archive, каталог `install`, SMB и второй SSH-сеанс не являются целевым normal flow.

## Поддержанные Ultra mapping

- Keenetic Ultra **KN-1811** → AArch64 → `aarch64-k3.10`;
- Netcraze Ultra **NC-1812** → AArch64 → `aarch64-k3.10`;
- legacy Keenetic Ultra **KN-1810** → MIPSel → `mipselsf-k3.4`.

Unknown model = STOP.

## До команды

В веб-интерфейсе:

1. установить поддержку EXT filesystem;
2. установить **Open Package support / OPKG**;
3. установить **SSH server**;
4. штатно отформатировать один USB-раздел в EXT4;
5. убедиться, что раздел mounted;
6. оставить на время первого install ровно один mounted EXT4, чтобы Stage-0 не выбирал диск по догадке.

Online `opkg disk <disk> <url>` flow рассчитан на актуальную KeeneticOS/Netcraze OS с URL option из ветки 4.2+.

## Stock SSH credentials

Первое подключение — к SSH самого роутера:

- обычно `192.168.1.1:22`;
- login — локальный administrator account роутера, например `admin`;
- password — пароль этой учётной записи, заданный пользователем.

Это **не** `root/keenetic`.

## Одна команда

Windows PowerShell / Windows Terminal:

```powershell
curl.exe -fsSL https://github.com/VoltickVL/FreeNet-Router/releases/latest/download/bootstrap_router_ultra.sh | ssh -tt admin@192.168.1.1 "exec /bin/sh"
```

macOS / Linux:

```sh
curl -fsSL https://github.com/VoltickVL/FreeNet-Router/releases/latest/download/bootstrap_router_ultra.sh | ssh -tt admin@192.168.1.1 'exec /bin/sh'
```

При другом IP/login/stock SSH port значения меняются в команде.

## Stage-0 contract

`scripts/bootstrap_router_ultra.sh`:

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

Для normal install он не нужен.

Если после установки пользователь сознательно открывает Entware shell вручную, официальная схема обычно использует login `root`, initial password `keenetic`; при занятом stock SSH порту 22 Entware shell обычно находится на 222. Initial password следует сменить через `passwd`.

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
