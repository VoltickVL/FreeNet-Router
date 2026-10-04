# Reference-сценарии Routing FreeNet

Этот документ фиксирует **реальные несекретные reference snapshots** маршрутизации, собранные на FreeNet v0.4.90.

Это **не ISP presets** и не правило «выбран провайдер → автоматически применить этот JSON». Один и тот же ISP может использовать разные DNS и routing policy. Примеры нужны как проверенная точка отсчёта для диагностики, восстановления и будущего развития Control Center.

## Общий принцип порядка правил

Xray применяет routing rules сверху вниз, поэтому порядок здесь является частью политики:

1. служебные DNS rules, если они нужны в конкретном сценарии;
2. точечные DIRECT-исключения и private networks;
3. специальные VPN IP rules;
4. специальные VPN domain rules;
5. широкие DIRECT domain rules;
6. широкие DIRECT GeoIP rules;
7. финальный fallback.

Специальные VPN-категории `instagram/meta/facebook/ru-blocked/re-filter` должны стоять **выше** широкого `geoip:ru -> DIRECT`. Иначе совпавший RU IP может уйти DIRECT раньше специального VPN-правила.

Во всех трёх reference-конфигах `domainStrategy` остаётся `AsIs`.

## Почему здесь нет `category-ads-all -> BLOCK`

В этих reference snapshots отдельное правило

```json
{
  "type": "field",
  "domain": [
    "ext:geosite.dat:category-ads-all"
  ],
  "outboundTag": "block"
}
```

намеренно отсутствует.

Xray routing BLOCK — не полноценный браузерный/DNS ad blocker. Он блокирует только трафик, который Xray может сопоставить с соответствующим destination domain. First-party реклама, баннеры с основного сайта, общие CDN и другие неразделимые источники этим правилом могут не отфильтровываться. В текущих реальных сценариях правило не дало ожидаемого пользовательского эффекта, поэтому оно не входит в reference policy.

---

# 1. Владлинк — DNS через VPN

Контекст: Владлинк, DNS policy через VPN/split-DNS service rules. Instagram/Meta и blocked-категории идут через VLESS раньше общего RU DIRECT.

```json
{
  "routing": {
    "domainStrategy": "AsIs",
    "rules": [
      {
        "type": "field",
        "inboundTag": [
          "dns-vless"
        ],
        "outboundTag": "vless-reality"
      },
      {
        "type": "field",
        "inboundTag": [
          "dns-direct"
        ],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "port": 53,
        "outboundTag": "dns-out"
      },
      {
        "type": "field",
        "ip": [
          "ext:geoip.dat:private"
        ],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "domain": [
          "ext:geosite.dat:instagram",
          "ext:geosite.dat:meta",
          "ext:geosite.dat:ru-blocked"
        ],
        "outboundTag": "vless-reality"
      },
      {
        "type": "field",
        "ip": [
          "ext:geoip.dat:facebook",
          "ext:geoip.dat:ru-blocked",
          "ext:geoip.dat:ru-blocked-community",
          "ext:geoip.dat:re-filter"
        ],
        "outboundTag": "vless-reality"
      },
      {
        "type": "field",
        "domain": [
          "ext:geosite.dat:category-ru",
          "ext:geosite.dat:ru-available-only-inside",
          "ext:geosite.dat:category-remote-control",
          "ext:geosite.dat:alibaba",
          "ext:geosite.dat:tencent",
          "ext:geosite.dat:apple",
          "ext:geosite.dat:microsoft",
          "ext:geosite.dat:google",
          "ext:geosite.dat:steam",
          "ext:geosite.dat:ea",
          "ext:geosite.dat:github",
          "ext:geosite.dat:xiaomi",
          "ext:geosite.dat:nvidia",
          "ext:geosite.dat:airchina",
          "ext:geosite.dat:ctrip",
          "ext:geosite.dat:private",
          "domain:blncaccount.com",
          "domain:petkit.com",
          "domain:netcraze.pro"
        ],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "ip": [
          "ext:geoip.dat:google",
          "ext:geoip.dat:ru"
        ],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "network": "tcp,udp",
        "outboundTag": "vless-reality"
      }
    ]
  }
}
```

Ключевая policy: Instagram/Meta и соответствующие IP/block lists принудительно VLESS; обычные RU/Google и перечисленные сервисы — DIRECT; неизвестный трафик — VLESS.

---

# 2. Владлинк — DNS DIRECT на Keenetic

Контекст: Владлинк, DNS обслуживается DIRECT на стороне текущей схемы Keenetic. Поэтому в этом `05_routing` нет split-DNS inbound rules из первого сценария.

```json
{
  "routing": {
    "domainStrategy": "AsIs",
    "rules": [
      {
        "type": "field",
        "ip": [
          "ext:geoip.dat:private"
        ],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "domain": [
          "ext:geosite.dat:instagram",
          "ext:geosite.dat:meta",
          "ext:geosite.dat:ru-blocked"
        ],
        "outboundTag": "vless-reality"
      },
      {
        "type": "field",
        "ip": [
          "ext:geoip.dat:facebook",
          "ext:geoip.dat:ru-blocked",
          "ext:geoip.dat:ru-blocked-community",
          "ext:geoip.dat:re-filter"
        ],
        "outboundTag": "vless-reality"
      },
      {
        "type": "field",
        "domain": [
          "ext:geosite.dat:category-ru",
          "ext:geosite.dat:ru-available-only-inside",
          "ext:geosite.dat:category-remote-control",
          "ext:geosite.dat:alibaba",
          "ext:geosite.dat:tencent",
          "ext:geosite.dat:apple",
          "ext:geosite.dat:microsoft",
          "ext:geosite.dat:google",
          "ext:geosite.dat:steam",
          "ext:geosite.dat:ea",
          "ext:geosite.dat:github",
          "ext:geosite.dat:xiaomi",
          "ext:geosite.dat:nvidia",
          "ext:geosite.dat:airchina",
          "ext:geosite.dat:ctrip",
          "ext:geosite.dat:private",
          "domain:blncaccount.com",
          "domain:petkit.com",
          "domain:netcraze.pro"
        ],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "ip": [
          "ext:geoip.dat:google",
          "ext:geoip.dat:ru"
        ],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "network": "tcp,udp",
        "outboundTag": "vless-reality"
      }
    ]
  }
}
```

Ключевая policy совпадает с первым сценарием по Instagram/Meta, но DNS context другой и потому служебные DNS routing rules здесь отсутствуют.

---

# 3. Ростелеком — рабочая routing policy

Контекст: Ростелеком. Существующая рабочая политика сохраняет YouTube, Xbox, EA и `appstorrent.ru` через VLESS; `89.108.108.28/32` является **явным runtime-specific DIRECT exception** и не должен автоматически переноситься в другие установки Ростелеком.

```json
{
  "routing": {
    "domainStrategy": "AsIs",
    "rules": [
      {
        "type": "field",
        "inboundTag": [
          "dns-vless"
        ],
        "outboundTag": "vless-reality"
      },
      {
        "type": "field",
        "inboundTag": [
          "dns-direct"
        ],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "port": 53,
        "outboundTag": "dns-out"
      },
      {
        "type": "field",
        "ip": [
          "89.108.108.28/32",
          "ext:geoip.dat:private"
        ],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "ip": [
          "ext:zkeenip.dat:youtube",
          "ext:geoip.dat:facebook",
          "ext:geoip.dat:ru-blocked",
          "ext:geoip.dat:ru-blocked-community",
          "ext:geoip.dat:re-filter"
        ],
        "outboundTag": "vless-reality"
      },
      {
        "type": "field",
        "domain": [
          "ext:geosite.dat:youtube",
          "ext:geosite.dat:xbox",
          "ext:geosite.dat:ea",
          "domain:appstorrent.ru",
          "ext:geosite.dat:instagram",
          "ext:geosite.dat:meta",
          "ext:geosite.dat:ru-blocked"
        ],
        "outboundTag": "vless-reality"
      },
      {
        "type": "field",
        "domain": [
          "ext:geosite.dat:category-ru",
          "ext:geosite.dat:ru-available-only-inside",
          "ext:geosite.dat:category-remote-control",
          "ext:geosite.dat:alibaba",
          "ext:geosite.dat:tencent",
          "ext:geosite.dat:apple",
          "ext:geosite.dat:microsoft",
          "ext:geosite.dat:google",
          "ext:geosite.dat:steam",
          "ext:geosite.dat:github",
          "ext:geosite.dat:xiaomi",
          "ext:geosite.dat:nvidia",
          "ext:geosite.dat:airchina",
          "ext:geosite.dat:ctrip",
          "ext:geosite.dat:private",
          "domain:blncaccount.com",
          "domain:petkit.com",
          "domain:netcraze.pro",
          "domain:plati.market"
        ],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "ip": [
          "ext:geoip.dat:google",
          "ext:geoip.dat:ru"
        ],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "network": "tcp,udp",
        "outboundTag": "vless-reality"
      }
    ]
  }
}
```

Ключевая policy:
- YouTube IP из `zkeenip.dat` и YouTube/Xbox/EA domains идут VLESS;
- Instagram/Meta/Facebook и blocked lists идут VLESS до broad RU DIRECT;
- `89.108.108.28/32` остаётся точечным DIRECT exception;
- `plati.market` остаётся DIRECT;
- неизвестный TCP/UDP трафик — VLESS.

## Использование этих примеров

Эти snapshots предназначены для:
- восстановления известной рабочей логики;
- сравнения при диагностике;
- regression/reference при изменениях Rules/Config Studio;
- понимания правильного порядка special rules и broad rules.

Они **не должны**:
- автоматически применяться только по имени ISP;
- заменять Browser Setup/Rules/Config Studio;
- переносить runtime-specific исключения между площадками без подтверждения;
- содержать secrets.

При изменении реальной политики сначала подтверждается runtime-факт, затем reference snapshot обновляется отдельным контролируемым изменением.
