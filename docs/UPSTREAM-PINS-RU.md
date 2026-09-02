# Политика pinned upstream для FreeNet Router

FreeNet Router не должен строить bootstrap на непроверенном `latest`. Для компонентов, которые устанавливаются из сторонних GitHub-репозиториев, версия и SHA-256 заранее фиксируются в `config/upstream-pins.env`.

## Текущие pinned upstream

### XKeen

- Репозиторий: `jameszeroX/XKeen`
- Версия: `2.0`
- Asset: `xkeen.tar.gz`
- SHA-256: хранится в `config/upstream-pins.env`.

### Xray

- Репозиторий: `XTLS/Xray-core`
- Версия: `v26.7.28`
- Причина pin: это exact known-working версия, уже подтверждённая на текущих HOME/WORK routers.
- Отдельные official Linux assets + SHA-256 зафиксированы для `arm64-v8a`, `mips32le`, `mips32`.
- FreeNet не выполняет live lookup `latest` при bootstrap.

Важно: GitHub metadata помечает этот upstream release как prerelease. FreeNet не трактует его как «последний стабильный вообще»; это воспроизводимый known-working compatibility pin. Смена Xray version — только отдельным проверенным PR.

### XKeen UI

- Репозиторий: `zxc-rv/XKeen-UI`
- Версия: `v1.1.3`
- Отдельные assets и SHA-256 зафиксированы для `arm64-v8a`, `mips32le`, `mips32`.

## Правила bootstrap

1. Runtime-код FreeNet использует только явно указанную версию/tag из manifest.
2. Скачанный asset обязательно проверяется по SHA-256 **до** любой live mutation.
3. Несовпадение SHA-256 — terminal FAIL. Установка не продолжается.
4. Переход на новую upstream-версию делается отдельным PR после проверки release metadata и digest.
5. Нельзя автоматически заменять pin на `latest` во время установки на роутере.
6. URL подписки, UUID, Reality keys, пароли и другие секреты в manifest не попадают.
7. Полный существующий XKeen/Xray stack не перестраивается bootstrap-ом: для него используется migration/update path.
8. Частично установленный stack (`XKeen` без `Xray` или наоборот) = `NEEDS_REVIEW`, без автоматической mutation.
9. Глобальный `opkg upgrade` запрещён; dependency provisioning только targeted.

## Bootstrap staging

`scripts/bootstrap_entware.sh` сначала классифицирует router:

- `NO_ENTWARE`;
- `UNSUPPORTED_ARCH`;
- `ENTWARE_ONLY`;
- `READY_EXISTING_STACK`;
- `NEEDS_REVIEW`.

На текущем первом P1 slice команды `plan` и `fetch` не устанавливают ничего в `/opt`: `fetch` только скачивает exact pinned XKeen/Xray/XKeen UI assets во временный staging и проверяет SHA-256. Permanent apply/rollback добавляется отдельным следующим slice и не должен смешиваться с неподтверждённым runtime поведением.

## Обновление pin

Перед изменением manifest необходимо:

1. прочитать официальный upstream release;
2. подтвердить точный tag, asset name и SHA-256 digest;
3. обновить `config/upstream-pins.env` одним PR;
4. дождаться exact-head CI SUCCESS;
5. только после этого использовать новый pin в bootstrap-коде.

Эта политика нужна для того, чтобы установка HOME / WORK / MOM и clean-router давала воспроизводимый результат, а не зависела от того, что upstream опубликовал в момент запуска команды.
