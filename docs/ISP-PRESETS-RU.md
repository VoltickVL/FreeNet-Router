# ISP / DNS presets FreeNet Router

## Цель

FreeNet не должен считать, что у всех Keenetic/Netcraze одинаковая DNS-топология. Каждый интернет-провайдер хранится отдельным versioned preset ID. Даже если два оператора сегодня используют одинаковое поведение, будущая правка одного оператора не должна автоматически менять другой.

## Отдельные ISP ID

- `auto` — определить/рекомендовать без безусловного изменения работающей конфигурации;
- `vladlink` — Владлинк;
- `alliancetelecom` — АльянсТелеком;
- `rostelecom` — Ростелеком;
- `podryad` — Подряд;
- `custom` — ручной/экспертный профиль.

Source of truth для preset metadata: `config/isp-presets.json`.

## Подтверждённая текущая группировка поведения

- Владлинк: HOME/MOM. Текущая рабочая схема сохраняется до отдельного clean-room DNS acceptance; YouTube сейчас идёт напрямую.
- АльянсТелеком: Management ожидает тот же принцип, что Владлинк, но preset самостоятельный.
- Ростелеком: WORK. Firmware `ndnproxy` владеет TCP/UDP `:53`, `/etc/resolv.conf` использует `127.0.0.1`, XKeen `proxy_dns=on`, Xray GID `11111`; YouTube/часть трафика идёт через VPN.
- Подряд: Management ожидает тот же принцип, что Ростелеком, но preset самостоятельный и требует отдельного runtime acceptance.

Общие helper-функции генерации/validation допустимы. Общая mutable preset-запись для двух операторов — нет.

## UX

В Browser Setup Wizard и FreeNet Control Center должны быть две связанные, но отдельные настройки:

1. «Интернет-провайдер» — Auto / Владлинк / АльянсТелеком / Ростелеком / Подряд / Свой.
2. «Режим DNS» — Auto / штатный DNS роутера / XKeen-Xray DNS / Custom.

Выбор ISP или DNS mode сам по себе не должен менять Xray. До apply FreeNet показывает текущий профиль, recommended preset, владельца `:53`, expected delta и rollback plan.

## Transactional apply

1. read-only preflight;
2. expected delta;
3. backup;
4. candidate configs;
5. полный `xray run -test` до apply;
6. atomic apply;
7. restart;
8. runtime acceptance;
9. rollback при любой ошибке после mutation.

## Ростелеком / текущий WORK preset

Подтверждено:

- firmware `ndnproxy` остаётся владельцем `:53`;
- отдельный Xray DNS listener `:53` не создаётся;
- Xray built-in DNS использует `dns-direct` и `dns-vless`;
- `dns-out` добавляется в outbounds;
- DNS routing добавляется перед существующими non-DNS rules;
- существующие VLESS credentials и non-DNS routing сохраняются.

## Владлинк / АльянсТелеком

Clean-room DNS-generation нельзя строить по догадке. До direct read-only acceptance HOME/MOM installer либо сохраняет уже работающий DNS layer, либо останавливает clean-room migration понятным сообщением.

## Security

ISP/DNS preset — несекретная настройка. Subscription URL, UUID, Reality keys/shortId и пароли в этот слой не входят и не попадают в GitHub/API/logs.
