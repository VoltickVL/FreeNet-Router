# BlancVPN в FreeNet Router

BlancVPN — первый provider preset FreeNet. Это не означает жёсткую привязку ядра FreeNet к одному провайдеру.

## Official references

- Личный кабинет / установка на роутер: используйте интерфейс BlancVPN account.
- Инструкция по XKeen: https://blancvpn.uno/ru/help/configuring-xkeen-windows

FreeNet хранит здесь только нашу архитектуру интеграции и ссылки на официальные источники. Текст сторонней статьи целиком не копируется.

## Что вводит пользователь

На этапе onboarding пользователь должен вставить **ключ-ссылку/subscription URL** локально в FreeNet setup wizard.

Subscription URL:

- не отображается обратно целиком после сохранения;
- не пишется в GitHub/CI;
- не попадает в diagnostic bundle;
- хранится только на роутере;
- может быть заменён/сброшен из настроек.

## Профили

Текущий v0.2.x использует четыре проверенных filter preset:

- Germany / Frankfurt / Extra;
- Poland / Warsaw / Extra;
- Finland / Helsinki / Extra;
- Netherlands / Amsterdam / Extra.

Roadmap P2/P3 должен отказаться от hardcoded набора как от ограничения UI.

После загрузки subscription FreeNet должен:

1. распарсить все поддерживаемые VLESS/Reality profiles;
2. определить country/city/profile label;
3. выделить категорию `Extra`, если она присутствует в названии/metadata;
4. показать **все найденные Extra profiles**;
5. позволить пользователю выбрать preferred/fallback profiles;
6. тестировать availability/latency;
7. не выдавать credential fields через status API.

## Рекомендации сервера

FreeNet не должен считать один город лучшим для всех пользователей.

Рекомендация строится на runtime measurements:

- TCP reachability;
- время установления соединения;
- несколько проб для стабильности;
- Xray validation;
- при возможности фактический egress check;
- DNS acceptance.

Ручной выбор всегда остаётся доступен.

## Endpoint updater

Текущий hardened updater:

- получает subscription;
- выбирает profile по локальному filter;
- заменяет только `vless-reality` outbound;
- сохраняет `direct`, `dns-out` и остальные non-VLESS outbounds;
- валидирует candidate полным Xray test;
- не печатает provider secrets;
- использует lock/backup/rollback.

Provider layer должен сохранить эти свойства.

## Generic providers

Следующий adapter — Generic VLESS/Reality.

Минимальные требования:

- VLESS Reality/XTLS profiles;
- нормализованный profile metadata;
- ручной filter/mapping, если provider naming нельзя распознать;
- те же validation/secret rules, что для BlancVPN.

## Компрометация ключ-ссылки

Если subscription URL случайно попал в скриншот, публичный лог или чужой доступ, его следует считать потенциально раскрытым и перевыпустить/заменить у provider, если provider поддерживает такую операцию.