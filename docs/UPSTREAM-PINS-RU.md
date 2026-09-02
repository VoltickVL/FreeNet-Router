# Политика pinned upstream для FreeNet Router

FreeNet Router не должен строить bootstrap на непроверенном `latest`. Для компонентов, которые устанавливаются из сторонних GitHub-репозиториев, версия и SHA-256 должны быть заранее зафиксированы в `config/upstream-pins.env`.

## Текущие pinned upstream

### XKeen

- Репозиторий: `jameszeroX/XKeen`
- Версия: `2.0`
- Asset: `xkeen.tar.gz`
- SHA-256: хранится в `config/upstream-pins.env`

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

## Xray

На текущем этапе P1 Xray остаётся компонентом, которым управляет XKeen. FreeNet ещё не фиксирует отдельный Xray binary release в этом manifest. Это отдельный будущий scope: сначала нужно выбрать стабильную политику совместимости XKeen/Xray и подтвердить её на clean-room роутере.

## Обновление pin

Перед изменением manifest необходимо:

1. прочитать официальный upstream release;
2. подтвердить точный tag, asset name и SHA-256 digest;
3. обновить `config/upstream-pins.env` одним PR;
4. дождаться exact-head CI SUCCESS;
5. только после этого использовать новый pin в bootstrap-коде.

Эта политика нужна для того, чтобы установка HOME / WORK / MOM и clean-router давала воспроизводимый результат, а не зависела от того, что upstream опубликовал в момент запуска команды.
