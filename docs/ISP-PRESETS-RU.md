# ISP / DNS presets FreeNet Router

## Цель

FreeNet не должен считать, что у всех Keenetic/Netcraze одинаковая DNS-топология. На известных роутерах Management уже подтверждены как минимум два разных runtime-профиля:

- HOME/MOM: Владлинк / АльянсТелеком — существующая HOME/MOM DNS-схема должна сохраняться до отдельного подтверждённого migration-профиля.
- WORK: Ростелеком — firmware `ndnproxy` владеет TCP/UDP `:53`, `/etc/resolv.conf` использует `127.0.0.1`, XKeen `proxy_dns=on`, Xray работает с GID `11111`.

## UX

В Browser Setup Wizard и FreeNet Control Center должен быть параметр «Интернет-провайдер / DNS-профиль»:

- `auto` — определить безопасный runtime-профиль по фактам текущего роутера;
- `vladlink` — Владлинк;
- `alliancetelecom` — АльянсТелеком;
- `rostelecom` — Ростелеком;
- `custom` — ручной/экспертный профиль.

Выбор ISP не должен сам по себе менять Xray. Перед применением FreeNet показывает expected delta, делает backup и выполняет полную candidate validation. При ошибке после apply выполняется rollback.

## Известный WORK preset

Для Ростелекома на текущем WORK подтверждено:

- firmware `ndnproxy` остаётся владельцем `:53`;
- отдельный Xray DNS listener `:53` не создаётся;
- Xray built-in DNS использует `dns-direct` и `dns-vless`;
- `dns-out` добавляется в outbounds;
- DNS routing добавляется перед существующими non-DNS rules;
- существующие VLESS credentials и non-DNS routing сохраняются.

## HOME/MOM preset

Vladlink/AllianceTelecom нельзя генерировать по догадке. До direct read-only acceptance текущих HOME/MOM DNS facts installer должен либо:

1. сохранить уже работающий HOME/MOM DNS layer без перестройки, либо
2. остановить clean-room migration с понятным сообщением, если соответствующий validated preset ещё не доступен.

Полный HOME/MOM preset считается готовым только после read-only runtime snapshot + controlled candidate test + functional DNS/VPN acceptance.

## Security

ISP/DNS preset — несекретная настройка. Subscription URL, UUID, Reality keys/shortId и пароли в этот слой не входят и не попадают в GitHub/API/logs.
