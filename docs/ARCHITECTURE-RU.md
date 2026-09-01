# Архитектура FreeNet Router

## Слои

```text
Keenetic / Netcraze firmware
        │
        ├─ LAN / WAN / CrazeDNS / system services
        │
        └─ USB + OPKG support
                │
                ▼
             Entware
                │
                ├─ packages / cron / init / Dropbear
                │
                ▼
              XKeen
                │
                ├─ netfilter / policy integration
                ├─ Xray lifecycle
                ├─ GeoIP / GeoSite
                │
                ├──────────────┐
                ▼              ▼
              Xray          XKeen UI
                │
                ▼
          FreeNet provider/updater
                │
                ▼
             FreeNet UI
```

## Ownership

### Firmware

FreeNet не должен самовольно менять firmware networking, WAN, CrazeDNS, firewall или system services вне явно согласованного setup step.

### Entware

FreeNet может устанавливать только подтверждённые необходимые packages. Не делать глобальный `opkg upgrade` как побочный эффект bootstrap.

### XKeen

XKeen остаётся владельцем интеграции Xray с Keenetic/Netcraze и netfilter/policy routing. FreeNet должен использовать upstream-supported commands и не форкать XKeen без необходимости.

### Xray

Xray — data plane VPN. FreeNet управляет конфигурацией через versioned templates/presets и всегда валидирует candidate до apply.

### XKeen UI

XKeen UI — low-level control/config interface. FreeNet не заменяет его полностью: продвинутый пользователь может открыть XKeen UI отдельно.

### FreeNet

FreeNet — high-level Control Center:

- provider/subscription;
- выбор профиля/страны;
- endpoint refresh/failover;
- routing presets;
- automation;
- updates;
- backup/rollback;
- setup wizard;
- security/password;
- diagnostics.

## Секреты

Секретами считаются как минимум:

- provider subscription URL/token;
- VLESS UUID;
- Reality private/public key и shortId, если они credential-bearing;
- пароли;
- session secrets.

Они хранятся только локально на router filesystem с ограниченными правами и не должны попадать в GitHub, CI logs, status API или diagnostic bundle.

## Runtime safety

Любая операция, меняющая VPN/runtime:

1. snapshot/backup;
2. candidate generation;
3. static/semantic validation;
4. `xray run -test`;
5. controlled apply;
6. restart только когда нужен;
7. runtime health;
8. rollback при failed apply;
9. primary error и rollback result отдельно.

## Network exposure

FreeNet v0.2.x использует:

- LAN-specific listener `<LAN-IP>:1001`;
- loopback `127.0.0.1:1001` для local reverse proxy/CrazeDNS;
- **не** wildcard `0.0.0.0:1001`.

Планируемая auth не является поводом автоматически открывать FreeNet напрямую в WAN.

## Provider architecture

Provider adapter должен преобразовать subscription в нормализованный список профилей:

```text
id
provider
country
city
label
tier/category (например Extra)
protocol
endpoint
capabilities
```

Credential fields не возвращаются через обычный status API.

Первый adapter — BlancVPN. Следующий — Generic VLESS/Reality.

## Source of truth

- GitHub FreeNet-Router — код, release, templates, docs.
- Upstream XKeen/XKeen UI — их binaries/install semantics.
- Router runtime — фактическое состояние процессов/config/ports.
- Provider subscription — доступные VPN profiles.

Ни README, ни слова installer-а не заменяют runtime acceptance.