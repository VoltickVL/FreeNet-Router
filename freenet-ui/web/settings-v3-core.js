;(() => {
  'use strict';
  if (window.__freenetSettingsV3Loaded) return;
  window.__freenetSettingsV3Loaded = true;

  const q = (s, r = document) => r.querySelector(s);
  const qa = (s, r = document) => Array.from(r.querySelectorAll(s));
  const state = {
    data: null, status: null, baseline: '', dirty: false, saving: false, checking: false, controlsBusy: false,
    countries: [], countryCatalog: [], countryCatalogFresh: false, countryCatalogLoading: false, countryCatalogWarning: '',
    journalFilter: 'all', journalResultFilter: 'all', journalQuery: '', journalEvents: [], journalLive: true, journalFiltersOpen: false,
    journalRefreshing: false, journalGeneratedAt: '', journalError: '', journalTimer: null,
    journalPage: 1, journalPages: 1, journalPageSize: 100, journalTotal: 0, journalFilteredTotal: 0,
    journalStats: {total:0,success:0,neutral:0,errors:0}, journalRetainedFrom: '', journalRetainedTo: '', journalHealth: null, journalDecision: null,
    journalDatePreset: '24h', journalCustomFrom: '', journalCustomTo: '', journalSearchTimer: null
  };

  const svg = (name) => {
    const paths = {
      settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6V21H10v-.1a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H3v-4h.1a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3A1.7 1.7 0 0 0 10 3h4a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.1v4H21a1.7 1.7 0 0 0-1.6 1Z"/>',
      vpn: '<circle cx="12" cy="12" r="9"/><path d="M7 12h10M9 8l-2 4 2 4M15 8l2 4-2 4"/>',
      info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>',
      check: '<path d="m5 12 4 4L19 6"/>',
      play: '<path d="m8 5 11 7-11 7Z"/>',
      globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.5 2.6 3.7 5.6 3.7 9S14.5 18.4 12 21M12 3C9.5 5.6 8.3 8.6 8.3 12S9.5 18.4 12 21"/>',
      box: '<path d="m4 7 8-4 8 4-8 4Z"/><path d="M4 7v10l8 4 8-4V7M12 11v10"/>',
      backup: '<ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v6c0 1.7 3.6 3 8 3s8-1.3 8-3V5M4 11v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6"/>',
      refresh: '<path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 5v6h-6"/>',
      clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
      signal: '<path d="M4 18v-2M8 18v-5M12 18V9M16 18V6M20 18V3"/>',
      speed: '<path d="M4 17a8 8 0 1 1 16 0"/><path d="m12 13 4-4M8 17h8"/>',
      shield: '<path d="M12 3 5 6v5c0 4.6 2.8 8 7 10 4.2-2 7-5.4 7-10V6Z"/><path d="m9 12 2 2 4-4"/>',
      copy: '<rect x="9" y="9" width="10" height="10" rx="2"/><path d="M15 9V7a2 2 0 0 0-2-2H7a2 2 0 0 0-2 2v6a2 2 0 0 0 2 2h2"/>',
      chevron: '<path d="m9 18 6-6-6-6"/>',
      save: '<path d="M5 4h12l2 2v14H5Z"/><path d="M8 4v6h8V4M8 20v-6h8v6"/>',
      download: '<path d="M12 3v12M7 10l5 5 5-5M5 20h14"/>',
      upload: '<path d="M12 21V9M7 14l5-5 5 5M5 4h14"/>',
      subscription: '<rect x="4" y="6" width="16" height="13" rx="3"/><path d="M8 3v6M16 3v6M4 11h16"/>'
    };
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths[name] || paths.info}</svg>`;
  };

  

  function ensureSettingsNav() {
    const nav = q('.sidebar .nav');
    if (!nav) return null;
    q('.nav-btn[data-page="automation"]', nav)?.remove();
    let button = q('.nav-btn[data-page="settings"]', nav);
    if (!button) {
      button = document.createElement('button');
      button.type = 'button';
      button.className = 'nav-btn';
      button.dataset.page = 'settings';
      button.innerHTML = `<span class="nav-icon">${svg('settings')}</span><span>Настройки</span>`;
      const routing = q('.nav-btn[data-page="routing"],.nav-btn[data-page="network"]', nav);
      if (routing) nav.insertBefore(button, routing);
      else nav.appendChild(button);
      button.addEventListener('click', () => {
        if (typeof window.setPage === 'function') window.setPage('settings');
      });
    }
    try { if (typeof pageLabels === 'object' && pageLabels) pageLabels.settings = 'Настройки'; } catch (_) {}
    return button;
  }

  function rewireNavigation() {
    const nav = q('.sidebar .nav');
    if (!nav) return;
    ensureSettingsNav();
    qa('.nav-btn', nav).forEach(btn => {
      const page = btn.dataset.page;
      if (page === 'system' || page === 'subscription') btn.remove();
    });
    let journal = q('.nav-btn[data-page="journal"]', nav);
    if (!journal) {
      journal = document.createElement('button');
      journal.type = 'button';
      journal.className = 'nav-btn';
      journal.dataset.page = 'journal';
      journal.innerHTML = `<span class="nav-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="4" y="4" width="16" height="16" rx="2"/><path d="M8 8h8M8 12h8M8 16h6"/></svg></span><span>Журнал</span>`;
      nav.appendChild(journal);
    }
    if (journal.dataset.freenetJournalBound !== '1') {
      journal.dataset.freenetJournalBound = '1';
      journal.addEventListener('click', () => {
        if (typeof window.setPage === 'function') window.setPage('journal');
        mountJournalPage();
      });
    }
    if (!q('[data-page-view="journal"]')) {
      const page = document.createElement('section');
      page.className = 'page fn3-journal-page';
      page.dataset.pageView = 'journal';
      const content = q('.content');
      if (content) content.appendChild(page);
    }
  }

  const formatDate = (value, compact = false) => {
    if (!value) return '—';
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return '—';
    const p = new Intl.DateTimeFormat('ru-RU', {day:'2-digit',month:'2-digit',year:'numeric',hour:'2-digit',minute:'2-digit'}).format(d).replace(',', '');
    return compact ? p : p;
  };

  function intervalLabel(value) {
    return ({'30m':'30 минут','1h':'1 час','3h':'3 часа','6h':'6 часов','12h':'12 часов','24h':'24 часа'})[value] || '6 часов';
  }

  function healthIntervalLabel(value) {
    return ({'30s':'30 секунд','1m':'1 минуту','5m':'5 минут'})[value] || '1 минуту';
  }

  function healthIntervalOptions(selected) {
    return ['30s','1m','5m'].map(v => `<option value="${v}"${v===selected?' selected':''}>каждые ${healthIntervalLabel(v)}</option>`).join('');
  }

  function nextLabel(value) {
    if (!value) return 'не запланировано';
    const ms = new Date(value).getTime() - Date.now();
    if (!Number.isFinite(ms)) return formatDate(value);
    if (ms <= 0) return 'в ближайшее время';
    if (ms < 60000) return `через ${Math.max(1, Math.ceil(ms / 1000))} сек`;
    const min = Math.max(1, Math.ceil(ms / 60000));
    if (min < 60) return `через ${min} мин`;
    const h = Math.floor(min / 60), m = min % 60;
    return `через ${h} ч${m ? ` ${m} мин` : ''}`;
  }

  function cleanProfileLabel(value) {
    const text = String(value || '').trim();
    const withoutISO = text.replace(/^[A-Za-z]{2}\s+/, '').trim();
    const chars = Array.from(withoutISO);
    if (chars.length >= 2 && chars[0].codePointAt(0) >= 0x1F1E6 && chars[0].codePointAt(0) <= 0x1F1FF && chars[1].codePointAt(0) >= 0x1F1E6 && chars[1].codePointAt(0) <= 0x1F1FF) return chars.slice(2).join('').trim();
    return withoutISO;
  }

  function setCountryFlag(node, code) {
    if (!node) return;
    const cc = String(code || '').trim().toLowerCase();
    Array.from(node.classList).filter(name => name.startsWith('flag-')).forEach(name => node.classList.remove(name));
    if (/^[a-z]{2}$/.test(cc)) {
      node.classList.add('flag-icon', `flag-${cc}`);
      node.setAttribute('aria-label', cc.toUpperCase());
      node.textContent = '';
    } else {
      node.classList.remove('flag-icon');
      node.removeAttribute('aria-label');
      node.textContent = '🌐';
    }
  }

  function countryFlagMarkup(code) {
    const cc = String(code || '').trim().toLowerCase();
    return /^[a-z]{2}$/.test(cc) ? `<span class="flag-icon flag-${cc} fn3-country-flag" aria-hidden="true"></span>` : `<span class="fn3-country-flag">🌐</span>`;
  }

  function humanResult(result, message) {
    const full = String(result || '').trim().toLowerCase();
    const raw = full.includes(':') ? full.slice(full.lastIndexOf(':') + 1) : full;
    const msg = String(message || '');
    if (full === 'selection') return ['Решение', translateMessage(msg), 'neutral'];
    if (raw === 'start' || raw === 'started') return [/(обновлен|update)/i.test(msg) ? 'Обновление' : 'Запущено', translateMessage(msg), 'neutral'];
    if (raw === 'success' || raw === 'healthy' || raw === 'switched' || raw === 'updated' || raw === 'cleared') return ['Успешно', translateMessage(msg), 'ok'];
    if (raw === 'same' || raw === 'no_new' || raw === 'candidate' || raw === 'disabled' || raw === 'blocked' || raw === 'cooldown') return ['Без изменений', translateMessage(msg), 'neutral'];
    if (raw === 'uncertain' || raw === 'busy' || raw === 'degraded' || raw === 'recovery_allowed' || raw === 'reconciled_failed') return ['Пропущено', translateMessage(msg), 'neutral'];
    if (raw === 'failed' || raw === 'critical' || raw === 'rollback_failed') return ['Ошибка', translateMessage(msg), 'bad'];
    return ['Без изменений', translateMessage(msg), 'neutral'];
  }

  function translateMessage(message) {
    const m = String(message || '').trim();
    if (!m) return 'Операция завершена.';
    const lower = m.toLowerCase();
    if (lower.includes('fresh subscription has no endpoint for the current logical profile')) return 'Для текущего VPN в подписке нет нового адреса подключения.';
    if (lower.includes('current vpn') && lower.includes('does not require')) return 'Текущий VPN работает нормально, смена не требуется.';
    if (lower.includes('automation') && lower.includes('disabled')) return 'Автоматика выключена.';
    if (lower.includes('no endpoint')) return 'Для текущего VPN нет нового адреса подключения.';
    return m.replace(/logical profile/gi, 'текущего VPN').replace(/endpoint/gi, 'адрес подключения').replace(/Eligible/gi, 'проверенный');
  }

  function pageMarkup() {
    return `
      <div class="fn3-head"><div><h1>Настройки</h1><p>Автоматизация, обновления и резервное копирование FreeNet.</p></div><button id="fn3Save" class="btn primary fn3-save" type="button" disabled>${svg('save')}<span>Сохранено</span></button></div>
      <div class="fn3-grid">
        <section id="fn3SubscriptionMount" class="fn3-card fn3-subscription-card fn3-section-collapsed">
          <div class="fn3-subscription-head" role="button" tabindex="0" aria-expanded="false" aria-label="Подписка: скрыть или раскрыть">
            <div class="fn3-subscription-title"><span class="fn3-icon">${svg('subscription')}</span><div><h2>Подписка</h2><p>Ключ BlancVPN, доступные Extra-профили и ручная проверка.</p></div></div>
          </div>
          <div class="fn3-subscription-body">
            <div class="fn3-subscription-facts">
              <div class="fn3-subscription-fact">Статус <span id="fn3SubStatusMount"></span></div>
              <div class="fn3-subscription-fact">Extra-профили <span id="fn3SubCountMount"></span></div>
              <div class="fn3-subscription-fact">Обновление <span id="fn3SubUpdateMount"></span></div>
            </div>
            <div id="fn3SubKeyMount"></div>
            <div class="fn3-subscription-footer" id="fn3SubJournalMount"></div>
          </div>
        </section>
        <div class="fn3-left">
          <section id="fn3AutoCard" class="fn3-card fn3-section-collapsed">
            <div class="fn3-card-head" role="button" tabindex="0" aria-expanded="false" aria-label="AUTO VPN: скрыть или раскрыть"><div class="fn3-title"><span class="fn3-icon">${svg('vpn')}</span><div><h2>AUTO VPN</h2><span id="fn3AutoLabel" class="fn3-enabled-badge">Выключено</span><div class="fn3-sub">FreeNet автоматически поддерживает рабочий VPN.</div></div></div><label class="fn3-master"><span class="fn3-switch"><input id="fn3AutoEnabled" type="checkbox"><span></span></span></label></div>
            <div class="fn3-info">${svg('info')}<span>Быстрый watchdog проверяет только живучесть текущего VPN. Тяжёлый подбор серверов запускается лишь после подтверждённого отказа.<br>Режим работы определяет, может ли автоматика только обновлять текущий профиль или также подбирать проверенную замену.</span></div>
            <div class="fn3-endpoint-schedule fn3-health-schedule">
              <div class="fn3-endpoint-copy"><strong>Проверка доступности VPN</strong><small>30 секунд — максимально быстро; 1 минута — рекомендуемый баланс; 5 минут — минимальная нагрузка.</small></div>
              <select id="fn3HealthInterval" aria-label="Интервал проверки доступности VPN">${healthIntervalOptions('1m')}</select>
            </div>
            <div class="fn3-section-label">Режим работы</div>
            <div class="fn3-mode-list">
              <label class="fn3-mode" data-mode-card="endpoint"><input type="radio" name="fn3Mode" value="endpoint"><span><strong>Только текущий VPN <em class="fn3-mode-badge">Минимум изменений</em></strong><small>Страна и VPN-профиль фиксированы. FreeNet обновляет только endpoint этого же профиля и никогда сам не переключается на другой.</small></span></label>
              <label class="fn3-mode" data-mode-card="best"><input type="radio" name="fn3Mode" value="best"><span><strong>Полный AUTO VPN <em class="fn3-mode-badge">Автовосстановление</em></strong><small>Восстанавливает отказавший VPN, а при повторяющейся деградации качества сравнивает проверенные варианты и может перейти на существенно лучший.</small></span></label>
            </div>
            <div id="fn3EndpointSchedule" class="fn3-endpoint-schedule" hidden>
              <div class="fn3-endpoint-copy"><strong>Плановое обновление endpoint</strong><small>Только для текущего логического VPN; неоднозначный или небезопасный результат ничего не меняет.</small></div>
              <select id="fn3EndpointInterval" aria-label="Интервал обновления endpoint">${intervalOptions('1h')}</select>
            </div>
            <div id="fn3ReplacementSettings" class="fn3-replacement">
              <div class="fn3-section-label">Где искать замену VPN</div>
              <div class="fn3-scope-list">
                <label class="fn3-scope" data-scope-card="current"><input type="radio" name="fn3Scope" value="current"><span><strong>Текущая страна</strong><small>Замена только в той же стране — другой сервер или город.</small></span><span class="fn3-chevron">${svg('chevron')}</span></label>
                <label class="fn3-scope" data-scope-card="region"><input type="radio" name="fn3Scope" value="region"><span><strong>Лучший отклик <em class="fn3-recommended">Рекомендуется</em></strong><small>FreeNet учитывает скорость, отклик и стабильность; после повторяющейся деградации или подтверждённого отказа применяет только полностью измеренный лучший вариант.</small></span><span class="fn3-chevron">${svg('chevron')}</span></label>
                <label class="fn3-scope" data-scope-card="allowlist"><input type="radio" name="fn3Scope" value="allowlist"><span><strong>Выбранные страны</strong><small>Вы сами выбираете список стран.</small></span><span class="fn3-chevron">${svg('chevron')}</span></label>
              </div>
            </div>
            <div class="fn3-safety">${svg('shield')}<span id="fn3SafetyText">Рабочий VPN не меняется без причины. Все переключения выполняются только при подтверждённом сбое, с проверкой и автоматическим возвратом при необходимости.</span></div>
            <div class="fn3-auto-actions"><button id="fn3Check" class="btn primary" type="button">${svg('play')}<span>Проверить сейчас</span></button><div class="fn3-control"><span class="fn3-control-dot"></span><div><strong id="fn3ControlTitle">Автопроверка</strong><small id="fn3ControlNext">Следующая проверка: —</small></div></div></div>
            <div class="fn3-journal-linkrow"><button id="fn3AllEvents" class="fn3-link" type="button">История проверок и переключений → Журнал</button></div>
          </section>
        </div>
        <section class="fn3-card fn3-extra fn3-section-collapsed"><div class="fn3-extra-title" role="button" tabindex="0" aria-expanded="false" aria-label="Системное обслуживание: скрыть или раскрыть"><span class="fn3-icon fn3-maintenance-icon">${svg('settings')}</span><div class="fn3-extra-summary"><h2>Системное обслуживание</h2><div class="fn3-sub">Обновления, служебные данные и резервные копии.</div></div><span class="fn3-section-chevron">${svg('chevron')}</span></div><div class="fn3-extra-grid">${extraCard('subscription','subscription','Обновление подписки','Автоматическое обновление данных подписки — списка доступных VPN.','Проверять','Проверить сейчас')}${extraCard('geodata','globe','GeoData / GeoIP','Данные геолокации, используемые для маршрутизации и фильтров.','Обновлять','Обновить сейчас')}${extraCard('freenet','box','Обновление FreeNet','Автоматическая проверка новых версий FreeNet. Установка — только после подтверждения.','Проверять','Проверить сейчас')}${backupCard()}</div><div class="fn3-extra-save-row"><button id="fn3MaintenanceSave" class="btn fn3-extra-save" type="button" disabled>${svg('save')}<span>Сохранено</span></button></div></section>
      </div>
      <div class="fn3-compat"><select id="fnDNS"><option value="firmware">Прямой</option><option value="xkeen">Раздельный</option></select><input id="fnAutoEnabled" type="checkbox"><select id="fnAutoInterval"><option value="manual">Вручную</option></select><input id="fnAutoApply" type="checkbox"><input type="radio" name="fnAutoMode" value="best"><input type="radio" name="fnAutoPolicy" value="degraded"><input type="radio" name="fnCountryScope" value="region"><input id="fnGeoDataEnabled" type="checkbox"><span id="fnGeoDataSchedule"></span><span id="fnAutoEnabledLabel"></span><span id="fnCurrentProfile"></span><span id="fnCurrentProfileSmall"></span><span id="fnCurrentEndpoint"></span><span id="fnCurrentFlag"></span><span id="fnWatchState"></span><span id="fnLastRun"></span><span id="fnLatency"></span><span id="fnSpeed"></span><span id="fnJitter"></span><span id="fnHealthBanner"></span><tbody id="fnJournalBody"></tbody><button id="fnSaveSettings"></button><button id="fnCheckNow"></button></div>
      <div id="fn3CountryPop" class="fn3-country-pop" hidden></div>`;
  }

  function intervalOptions(selected) {
    return ['30m','1h','3h','6h','12h','24h'].map(v => `<option value="${v}"${v===selected?' selected':''}>раз в ${intervalLabel(v)}</option>`).join('');
  }

  function extraCard(key, icon, title, description, verb, actionLabel) {
    return `<article class="fn3-extra-card fn3-section-collapsed"><div class="fn3-extra-head" role="button" tabindex="0" aria-expanded="false" aria-label="${title}: скрыть или раскрыть"><span class="fn3-extra-icon">${svg(icon)}</span><div><h3>${title}</h3><p>${description}</p></div><label class="fn3-switch"><input id="fn3_${key}_enabled" type="checkbox"><span></span></label></div><div class="fn3-extra-row"><label for="fn3_${key}_interval">${verb}</label><select id="fn3_${key}_interval"></select></div><div class="fn3-extra-meta"><span>Последняя проверка</span><b id="fn3_${key}_last">—</b><span>Следующая проверка</span><b id="fn3_${key}_next">—</b></div><button class="btn secondary fn3-extra-action" type="button" data-v3-action="${key}">${svg('refresh')}<span>${actionLabel}</span></button>${key === 'geodata' ? '<div id="fn3_geodata_operation" class="fn3-sub" role="status" aria-live="polite" hidden></div>' : ''}${key === 'freenet' ? xkeenUpdatePanel() : ''}</article>`;
  }

  function xkeenUpdatePanel() {
    // Reuse the existing FreeNet maintenance card, retaining the four-card
    // grid and all established folding/save geometry.
    return `<div class="fn3-xkeen-update" role="group" aria-label="Проверка обновления XKeen">
      <div class="fn3-sub"><strong>Версия XKeen</strong> · Проверка официального Stable без установки и перезапуска Xray</div>
      <div class="fn3-extra-meta"><span>Установлена</span><b id="fn3_xkeen_installed">—</b><span>Последняя Stable</span><b id="fn3_xkeen_latest">—</b></div>
      <button id="fn3_xkeen_check" class="btn secondary fn3-extra-action" type="button" data-v3-action="xkeen_check">${svg('refresh')}<span>Проверить обновление XKeen</span></button>
      <div id="fn3_xkeen_status" class="fn3-sub" role="status" aria-live="polite">Не проверено. XKeen и Xray не изменяются.</div>
    </div>`;
  }

  function backupCard() {
    return `<article class="fn3-extra-card fn3-section-collapsed"><div class="fn3-extra-head" role="button" tabindex="0" aria-expanded="false" aria-label="Резервное копирование: скрыть или раскрыть"><span class="fn3-extra-icon">${svg('backup')}</span><div><h3>Резервное копирование</h3><p>Снимки хранятся локально на роутере и используются для безопасного восстановления.</p></div><label class="fn3-switch"><input id="fn3_backup_enabled" type="checkbox"><span></span></label></div><div class="fn3-extra-row"><label for="fn3_backup_interval">Создавать</label><select id="fn3_backup_interval"></select></div><div class="fn3-extra-meta"><span>Последняя копия</span><b id="fn3_backup_last">—</b><span>Следующая копия</span><b id="fn3_backup_next">—</b></div><details class="fn3-backup-details"><summary>Пути и сведения о снимке</summary><div class="fn3-backup-storage"><span>Хранилище</span><code id="fn3_backup_root">—</code><span>Последний снимок</span><code id="fn3_backup_snapshot">пока нет</code><span>Полный путь</span><code id="fn3_backup_path">—</code></div></details><div class="fn3-backup-contents">Локальный снимок настроек FreeNet, подписки, профилей и Xray. Секретные значения не выводятся в браузере.</div><div id="fn3_backup_result" class="fn3-backup-result" hidden></div><div class="fn3-backup-actions"><button class="btn secondary" type="button" data-v3-action="backup_create" title="Создать внутренний снимок настроек на роутере">${svg('download')}Создать снимок</button><button class="btn secondary fn3-danger" type="button" data-v3-action="backup_restore" title="Восстановить последний внутренний снимок настроек на роутере">${svg('upload')}Восстановить последний</button></div></article>`;
  }

  function currentForm() {
    const mode = q('input[name="fn3Mode"]:checked')?.value === 'endpoint' ? 'endpoint' : 'best';
    const healthInterval = q('#fn3HealthInterval')?.value || '1m';
    const endpointInterval = q('#fn3EndpointInterval')?.value || '1h';
    const scope = q('input[name="fn3Scope"]:checked')?.value || 'region';
    const read = key => ({enabled: !!q(`#fn3_${key}_enabled`)?.checked, interval: q(`#fn3_${key}_interval`)?.value || ''});
    return {enabled: !!q('#fn3AutoEnabled')?.checked, mode, healthInterval, endpointInterval, scope, countries: state.countries.slice().sort(), subscription:read('subscription'), geodata:read('geodata'), freenet:read('freenet'), backup:read('backup')};
  }

  function formKey() { return JSON.stringify(currentForm()); }
  function markDirty() { state.dirty = formKey() !== state.baseline; renderSave(); renderMode(); }
  function renderSave() {
    const buttons = [q('#fn3Save'), q('#fn3MaintenanceSave')].filter(Boolean);
    if (!buttons.length) return;
    buttons.forEach(btn => {
      btn.disabled = state.saving || state.controlsBusy || !state.dirty;
      btn.classList.toggle('primary', state.dirty);
      const label = q('span', btn);
      if (label) label.textContent = state.saving ? 'Сохраняем…' : state.dirty ? 'Сохранить изменения' : 'Сохранено';
    });
  }
  function renderScope() { qa('[data-scope-card]').forEach(n => n.classList.toggle('selected', q('input',n)?.checked)); }
  function renderMode() {
    const mode = q('input[name="fn3Mode"]:checked')?.value === 'endpoint' ? 'endpoint' : 'best';
    const endpointOnly = mode === 'endpoint';
    qa('[data-mode-card]').forEach(n => n.classList.toggle('selected', q('input', n)?.checked));
    const schedule = q('#fn3EndpointSchedule');
    const replacement = q('#fn3ReplacementSettings');
    if (schedule) schedule.hidden = !endpointOnly;
    if (replacement) replacement.hidden = endpointOnly;
    qa('input[name="fn3Scope"]').forEach(input => { input.disabled = endpointOnly; });
    renderScope();

    const safety = q('#fn3SafetyText');
    if (safety) safety.textContent = endpointOnly
      ? 'Страна и VPN-профиль зафиксированы. FreeNet может изменить только endpoint текущего профиля; если безопасное восстановление не подтверждено — STOP без смены сервера.'
      : 'Рабочий VPN не меняется без причины. При подтверждённом сбое FreeNet сначала восстанавливает текущий endpoint и только затем может применить полностью проверенную замену.';

    const enabled = !!q('#fn3AutoEnabled')?.checked;
    const title = q('#fn3ControlTitle');
    const next = q('#fn3ControlNext');
    if (!enabled) {
      if (title) title.textContent = 'Автоматика выключена';
      if (next) next.textContent = 'Автоматические проверки не выполняются';
    } else if (endpointOnly) {
      if (title) title.textContent = 'Только текущий VPN';
      if (next) next.textContent = `Контроль: каждые ${healthIntervalLabel(q('#fn3HealthInterval')?.value || '1m')} · endpoint: раз в ${intervalLabel(q('#fn3EndpointInterval')?.value || '1h')}`;
    } else {
      if (title) title.textContent = 'Полный AUTO VPN';
      if (next) next.textContent = `Следующая проверка: ${nextLabel(state.data?.auto_vpn?.next_health)} · интервал ${healthIntervalLabel(q('#fn3HealthInterval')?.value || '1m')}`;
    }
  }

  async function fetchJSON(url, options) {
    const res = await fetch(url, options);
    const type = res.headers.get('content-type') || '';
    const data = type.includes('application/json') ? await res.json() : {success:false,error:`HTTP ${res.status}`};
    if (!res.ok || data.success === false) throw new Error(data.error || `HTTP ${res.status}`);
    return data;
  }

  function applySchedule(key, data) {
    const enabled = q(`#fn3_${key}_enabled`), interval = q(`#fn3_${key}_interval`);
    if (enabled) enabled.checked = !!data?.enabled;
    if (interval) { interval.innerHTML = intervalOptions(data?.interval || ({subscription:'6h',geodata:'3h',freenet:'12h',backup:'24h'})[key]); interval.value = data?.interval || ({subscription:'6h',geodata:'3h',freenet:'12h',backup:'24h'})[key]; }
    const last = q(`#fn3_${key}_last`), next = q(`#fn3_${key}_next`);
    if (last) {
      const success = key === 'backup' ? 'Снимок создан' : 'Успешно';
      last.textContent = data?.last_run ? `${formatDate(data.last_run)}${data.result ? ` · ${data.result === 'success' ? success : data.result === 'available' ? 'Доступно обновление' : 'Ошибка'}` : ''}` : '—';
    }
    if (last) last.classList.toggle('ok', data?.result === 'success' || data?.result === 'available');
    if (next) next.textContent = data?.enabled ? (data?.next_run ? formatDate(data.next_run) : 'после первого запуска') : 'выключено';
  }

  function renderBackupInfo(info) {
    const root = q('#fn3_backup_root');
    const snapshot = q('#fn3_backup_snapshot');
    const path = q('#fn3_backup_path');
    if (root) root.textContent = info?.root || '—';
    if (snapshot) snapshot.textContent = info?.latest || 'пока нет';
    if (path) path.textContent = info?.latest_path || '—';
    const restore = q('[data-v3-action="backup_restore"]');
    if (restore) {
      restore.disabled = !info?.latest;
      restore.title = info?.latest
        ? `Восстановить снимок ${info.latest}`
        : 'Сначала создайте снимок настроек FreeNet';
    }
  }

  function showBackupActionResult(data, action) {
    const host = q('#fn3_backup_result'); if (!host) return;
    const info = data?.backup_info || {};
    host.hidden = false;
    host.classList.remove('bad');
    host.textContent = '';
    const title = document.createElement('strong');
    title.textContent = action === 'backup_restore' ? 'Снимок восстановлен и проверен' : 'Снимок создан';
    host.appendChild(title);
    if (info.latest) {
      const name = document.createElement('span'); name.textContent = info.latest; host.appendChild(name);
    }
    if (info.latest_path) {
      const path = document.createElement('code'); path.textContent = info.latest_path; host.appendChild(path);
    }
    const note = document.createElement('span');
    note.textContent = action === 'backup_restore'
      ? 'FreeNet применил snapshot и проверил восстановленные файлы. При ошибке используется отдельный rollback snapshot.'
      : `Зафиксировано состояние ${info.tracked || 4} контролируемых компонентов и manifest. Этот snapshot теперь используется действием «Восстановить последний».`;
    host.appendChild(note);
  }

  function showBackupActionError(message) {
    const host = q('#fn3_backup_result'); if (!host) return;
    host.hidden = false;
    host.classList.add('bad');
    host.textContent = '';
    const title = document.createElement('strong'); title.textContent = 'Операция не выполнена';
    const note = document.createElement('span'); note.textContent = message || 'Неизвестная ошибка';
    host.append(title, note);
  }

  function applyData(data) {
    state.data = data;
    const auto = data.auto_vpn || {};
    q('#fn3AutoEnabled').checked = !!auto.enabled;
    q('#fn3AutoLabel').textContent = auto.enabled ? 'Включено' : 'Выключено';
    const mode = auto.mode === 'endpoint' ? 'endpoint' : 'best';
    const modeInput = q(`input[name="fn3Mode"][value="${mode}"]`); if (modeInput) modeInput.checked = true;
    const healthInterval = ['30s','1m','5m'].includes(auto.health_interval) ? auto.health_interval : '1m';
    const healthSelect = q('#fn3HealthInterval');
    if (healthSelect) { healthSelect.innerHTML = healthIntervalOptions(healthInterval); healthSelect.value = healthInterval; }
    const endpointInterval = ['30m','1h','3h','6h','12h','24h'].includes(auto.endpoint_interval) ? auto.endpoint_interval : '1h';
    const endpointSelect = q('#fn3EndpointInterval');
    if (endpointSelect) { endpointSelect.innerHTML = intervalOptions(endpointInterval); endpointSelect.value = endpointInterval; }
    state.countries = Array.isArray(auto.countries) ? auto.countries.slice() : [];
    const scope = ['current','region','allowlist'].includes(auto.country_scope) ? auto.country_scope : 'region';
    const scopeInput = q(`input[name="fn3Scope"][value="${scope}"]`); if (scopeInput) scopeInput.checked = true;
    renderMode();

    if (!state.journalEvents.length && Array.isArray(data.events)) state.journalEvents = data.events.slice();
    if (journalPageActive()) renderJournalPageState();
    applySchedule('subscription', data.subscription); applySchedule('geodata', data.geodata); applySchedule('freenet', data.freenet); applySchedule('backup', data.backup);
    renderBackupInfo(data.backup_info);
    state.baseline = formKey(); state.dirty = false; renderSave();
    window.__freenetSettingsV3Snapshot = data;
    document.dispatchEvent(new CustomEvent('freenet:settings-v3-updated', {detail:data}));
  }

  function journalCategory(event) {
    const kind = String(event?.kind || '').trim().toLowerCase();
    if (kind === 'vpn' || kind === 'vpn_manual') return 'vpn';
    if (kind === 'auto vpn' || kind === 'auto_vpn') return 'auto';
    if (kind === 'subscription') return 'subscription';
    return 'system';
  }

  function journalStage(event) {
    if (journalCategory(event) !== 'auto') return '';
    const raw = String(event?.result || '').trim().toLowerCase();
    if (raw === 'selection') return 'selection';
    const split = raw.indexOf(':');
    return split > 0 ? raw.slice(0, split) : '';
  }

  function journalStageLabel(stage) {
    return ({
      detect:'Проверка',
      wan:'Обычный интернет',
      confirm:'Подтверждение',
      endpoint_refresh:'Endpoint',
      candidate_selection:'Подбор',
      selection:'Решение',
      apply:'Применение',
      post_check:'Post-check',
      rollback:'Rollback',
      post_update_guard:'После обновления',
      rollback_guard:'Защита rollback'
    })[stage] || '';
  }

  function journalKind(event) {
    const kind = String(event?.kind || '').trim();
    const category = journalCategory(event);
    if (kind.toLowerCase() === 'vpn_manual') return ['Ручной', 'vpn manual'];
    if (category === 'vpn') return ['VPN', 'vpn'];
    if (category === 'auto') {
      const stage = journalStageLabel(journalStage(event));
      return [stage ? `AUTO VPN · ${stage}` : 'AUTO VPN', 'auto'];
    }
    if (category === 'subscription') return ['Подписка', 'system'];
    if (kind === 'geodata') return ['GeoData / GeoIP', 'system'];
    if (kind === 'freenet') return ['FreeNet', 'system'];
    if (kind === 'freenet_release_catalog') return ['Каталог FreeNet', 'system'];
    if (kind === 'freenet_update' || kind === 'freenet_update_recovery') return ['Обновление FreeNet', 'system'];
    if (kind === 'backup') return ['Резервная копия', 'system'];
    return [kind || 'Система', 'system'];
  }

  function journalTone(event) {
    return humanResult(event?.result, event?.message)[2];
  }

  function normalizedSearch(value) {
    return String(value || '').toLocaleLowerCase('ru-RU').replace(/\s+/g, ' ').trim();
  }

  function filteredJournalEvents(events) {
    let rows = Array.isArray(events) ? events.slice() : [];
    if (state.journalFilter !== 'all') rows = rows.filter(event => journalCategory(event) === state.journalFilter);
    if (state.journalResultFilter !== 'all') rows = rows.filter(event => journalTone(event) === state.journalResultFilter);
    const query = normalizedSearch(state.journalQuery);
    if (query) {
      rows = rows.filter(event => {
        const [result, translated] = humanResult(event?.result, event?.message);
        const [kind] = journalKind(event);
        const haystack = normalizedSearch([
          formatDate(event?.at), event?.at, event?.kind, event?.result, kind, result, translated, event?.message
        ].join(' '));
        return haystack.includes(query);
      });
    }
    return rows;
  }

  function manualJournalMarkup(message) {
    const rows = String(message || '').split(/\s+\|\s+/).map(x => x.trim()).filter(Boolean);
    if (!rows.length) return '<div class="fn3-journal-event-message">Нет подробностей.</div>';
    const rollbackLabels = {
      NOT_NEEDED:'не требуется', NOT_APPLIED:'ничего не изменено',
      ROLLED_BACK:'выполнен', ROLLBACK_SUCCESS:'выполнен',
      ROLLBACK_FAILED:'ошибка отката', ROLLBACK_UNKNOWN:'состояние неизвестно'
    };
    return '<div class="fn3-journal-manual">' + rows.map(field => {
      const split = field.indexOf(':');
      const label = split < 1 ? 'Событие' : field.slice(0, split).trim();
      const raw = split < 1 ? field : field.slice(split + 1).trim();
      const value = label === 'Откат' ? (rollbackLabels[raw] || raw) : raw;
      return '<div class="fn3-journal-manual-row"><span>' + escapeHTML(label) +
        '</span><strong>' + escapeHTML(value || '—') + '</strong></div>';
    }).join('') + '</div>';
  }

  // Friendly event headline. The complete original message stays available
  // behind disclosure, while the separate technical export preserves full stages.
  function journalResultLabel(event, fallback) {
    const value = String(event?.result || '').toLowerCase();
    if (value.includes('post_update_guard:blocked')) return 'Ожидание';
    if (value.includes('post_update_guard:recovery_allowed')) return 'Проверка разрешена';
    if (value.includes('post_update_guard:cleared')) return 'Подтверждено';
    if (value === 'uncertain') return 'Не подтверждено';
    if (value.includes('rollback') && value.includes('failed')) return 'Ошибка отката';
    if (value.includes('incident:recovered')) return 'Восстановлено';
    return fallback;
  }

  function journalBrief(event, message) {
    const result = String(event?.result || '').toLowerCase();
    const raw = String(message || '').trim();
    if (result.includes('post_update_guard:blocked')) return 'После обновления состояние VPN пока неясно — автоматические изменения удерживаются.';
    if (result.includes('post_update_guard:recovery_allowed')) return 'Текущий VPN не подтвердил доступ — разрешены штатные проверки перед восстановлением.';
    if (result.includes('post_update_guard:cleared')) return 'Текущий VPN подтвердил доступ после обновления.';
    if (result.includes('rollback') && result.includes('failed')) return 'Не удалось подтвердить безопасный откат — требуется диагностика.';
    if (result.includes('incident:recovered')) return 'AUTO VPN восстановил соединение.';
    if (result.includes('incident:start')) return 'AUTO VPN начал восстановление соединения.';
    if (result.includes('incident:failed')) return 'AUTO VPN не удалось восстановить соединение.';
    if (/качество соединения ухудшен|подтверждение деградации/i.test(raw)) return 'Качество VPN ухудшилось — AUTO VPN собирает подтверждения.';
    const first = raw.split(/[.!?]\s+/)[0] || raw;
    return first.length > 145 ? first.slice(0, 142).trimEnd() + '…' : first;
  }

  function renderJournal(events, target = '#fn3JournalFull') {
    const body = q(target); if (!body) return;
    const source = Array.isArray(events) ? events : [];
    const filtered = source.slice(0, target === '#fn3JournalFull' ? state.journalPageSize : 4);
    if (!filtered.length) {
      body.innerHTML = '<div class="fn3-journal-empty">По текущему поиску и фильтрам событий нет.</div>';
      return;
    }
    body.innerHTML = filtered.map(e => {
      const [result, msg, tone] = humanResult(e.result, e.message);
      const readableResult = journalResultLabel(e, result);
      const [kind, kindClass] = journalKind(e);
      const isManual = String(e.kind || '').trim().toLowerCase() === 'vpn_manual';
      const brief = journalBrief(e, msg);
      const expanded = msg.length > 160 || brief !== msg;
      const messageMarkup = isManual ? manualJournalMarkup(msg) :
        `<div class="fn3-journal-event-message"><div class="fn3-journal-brief">${escapeHTML(brief)}</div>${expanded ?
          `<details class="fn3-journal-event-details"><summary>Подробности события</summary><div class="fn3-journal-raw-message">${escapeHTML(msg)}</div></details>` : ''}</div>`;
      const toneClass = tone === 'ok' ? 'ok' : tone === 'bad' ? 'bad' : 'neutral';
      const resultClass = tone === 'ok' ? '' : tone === 'bad' ? 'bad' : 'neutral';
      return `<article class="fn3-journal-event ${toneClass}">
        <div class="fn3-journal-event-rail"><span class="fn3-journal-event-dot"></span></div>
        <div class="fn3-journal-event-main">
          <div class="fn3-journal-event-top">
            <div class="fn3-journal-event-tags"><span class="fn3-kind ${kindClass}">${escapeHTML(kind)}</span><span class="fn3-result ${resultClass}"><i class="fn3-dot"></i>${escapeHTML(readableResult)}</span></div>
            <time class="fn3-journal-event-time">${formatDate(e.at)}</time>
          </div>
          ${messageMarkup}
        </div>
      </article>`;
    }).join('');
  }

  function escapeHTML(value) { return String(value || '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

  async function load() {
    try {
      const data = await fetchJSON('/api/settings-v3', {cache:'no-store'});
      applyData(data);
    } catch (err) {
      console.error('Settings v3 load failed', err);
    }
  }

  async function save() {
    if (!state.dirty || state.saving) return;
    state.saving = true; renderSave();
    const form = currentForm();
    try {
      const data = await fetchJSON('/api/settings-v3', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'save',auto_vpn_enabled:form.enabled,auto_vpn_mode:form.mode,auto_vpn_health_interval:form.healthInterval,auto_vpn_endpoint_interval:form.endpointInterval,country_scope:form.scope,countries:form.countries,subscription_enabled:form.subscription.enabled,subscription_interval:form.subscription.interval,geodata_enabled:form.geodata.enabled,geodata_interval:form.geodata.interval,freenet_enabled:form.freenet.enabled,freenet_interval:form.freenet.interval,backup_enabled:form.backup.enabled,backup_interval:form.backup.interval})});
      applyData(data);
    } catch (err) { alert(`Не удалось сохранить настройки: ${err.message}`); }
    finally { state.saving = false; renderSave(); }
  }

  // GeoData is a separate, bounded background operation on the router.
  // Never await xkeen -ug on the HTTPS request used by the browser.
  let geoDataStarting = false;
  let geoDataPolling = false;
  function displayGeoDataOperation(message, busy = false, failed = false) {
    const note = q('#fn3_geodata_operation');
    const button = q('[data-v3-action="geodata"]');
    if (note) {
      note.hidden = !message;
      note.textContent = message || '';
      note.style.color = failed ? 'var(--danger, #efaaaa)' : '';
    }
    if (button) {
      button.disabled = busy || geoDataStarting;
      const label = q('span', button);
      if (label) label.textContent = busy ? 'Обновляется…' : 'Обновить сейчас';
    }
  }

  async function pollGeoDataOperation(expectedId = '') {
    if (geoDataPolling) return;
    geoDataPolling = true;
    try {
      // One server-side run is capped at 12 minutes; polling adds a bounded
      // allowance for slow browser/network responses without a second POST.
      const deadline = Date.now() + 14 * 60 * 1000;
      for (;;) {
        const data = await fetchJSON('/api/settings-v3/geodata-operation', {cache:'no-store'});
        const op = data.operation;
        if (expectedId && op && op.id !== expectedId) {
          displayGeoDataOperation('Состояние обновления изменилось. Не запускайте повторно до проверки журнала.', false, true);
          break;
        }
        if (data.active) {
          displayGeoDataOperation('XKeen обновляет GeoData на роутере. Текущий VPN не переключается; можно закрыть страницу.', true);
        } else if (!op) {
          // The daemon may have restarted. Never present a missing in-memory
          // receipt as success and never re-POST an uncertain operation.
          if (expectedId) displayGeoDataOperation('Результат обновления не подтверждён. Проверьте журнал и состояние Xray.', false, true);
          break;
        } else if (op.state === 'success') {
          displayGeoDataOperation(op.message || 'GeoData обновлены и проверены.', false);
          await load();
          break;
        } else if (op.state === 'failed') {
          displayGeoDataOperation(op.error || 'Обновление GeoData не подтверждено. Проверьте журнал.', false, true);
          await load();
          break;
        } else {
          displayGeoDataOperation('GeoData: статус неизвестен; повторный запуск заблокирован до диагностики.', false, true);
          break;
        }
        if (Date.now() > deadline) {
          displayGeoDataOperation('Нет подтверждения завершения. Операция может продолжаться на роутере; не запускайте её повторно.', false, true);
          break;
        }
        await new Promise(resolve => setTimeout(resolve, 1800));
      }
    } catch (_) {
      displayGeoDataOperation('Связь со статусом GeoData прервана. Обновление может продолжаться на роутере; проверьте журнал перед повтором.', false, true);
    } finally {
      geoDataPolling = false;
      const button = q('[data-v3-action="geodata"]');
      if (button) button.disabled = geoDataStarting;
    }
  }

  async function startGeoDataOperation() {
    if (geoDataStarting || geoDataPolling) return;
    geoDataStarting = true;
    displayGeoDataOperation('Запускаем штатное обновление XKeen…', true);
    try {
      const data = await fetchJSON('/api/settings-v3/action', {
        method:'POST', headers:{'Content-Type':'application/json'},
        body:JSON.stringify({action:'geodata_update'})
      });
      if (!data.operation || !data.operation.id) {
        displayGeoDataOperation('FreeNet не подтвердил запуск GeoData. Не запускайте повторно до проверки журнала.', false, true);
        return;
      }
      geoDataStarting = false;
      await pollGeoDataOperation(data.operation.id);
    } catch (err) {
      displayGeoDataOperation('Запуск GeoData не подтверждён: ' + err.message + '. Проверьте журнал перед повтором.', false, true);
    } finally {
      geoDataStarting = false;
      const button = q('[data-v3-action="geodata"]');
      if (button && !geoDataPolling) button.disabled = false;
    }
  }

  async function resumeGeoDataOperation() {
    if (geoDataStarting || geoDataPolling) return;
    try {
      const status = await fetchJSON('/api/settings-v3/geodata-operation', {cache:'no-store'});
      if (status.active) {
        void pollGeoDataOperation(status.operation?.id || '');
      } else if (status.operation?.state === 'failed') {
        displayGeoDataOperation(status.operation.error || 'Обновление GeoData не подтверждено.', false, true);
      } else if (status.operation?.state === 'success') {
        displayGeoDataOperation(status.operation.message || 'GeoData обновлены.', false);
      }
    } catch (_) {
      // The normal Settings snapshot and Journal remain available; never
      // infer failure or initiate a new mutation because status is offline.
    }
  }

  async function checkXKeenUpdate() {
    const button = q('#fn3_xkeen_check');
    const installed = q('#fn3_xkeen_installed');
    const latest = q('#fn3_xkeen_latest');
    const status = q('#fn3_xkeen_status');
    if (!button || button.disabled || !status) return;
    button.disabled = true;
    status.textContent = 'Проверяем установленный XKeen и официальный Stable…';
    try {
      // This is an authenticated read-only GET. Never call xkeen -uk or an apply endpoint.
      const data = await fetchJSON('/api/xkeen/update-check', {method:'GET', cache:'no-store', credentials:'include'});
      const safeVersion = value => typeof value === 'string' && /^[0-9]+(?:\.[0-9]+){1,3}$/.test(value) ? value : '';
      const have = safeVersion(data.installed_version), available = safeVersion(data.latest_stable_version);
      if (data.mutation !== 'NONE' || !have || !available || !['update-available','up-to-date','newer-than-stable'].includes(data.state)) {
        throw new Error('результат проверки XKeen не подтверждён');
      }
      if (installed) installed.textContent = have + (data.installed_channel === 'beta' ? ' · Beta' : data.installed_channel === 'stable' ? ' · Stable' : ' · канал неизвестен');
      if (latest) latest.textContent = available;
      status.textContent = data.state === 'update-available'
        ? 'Доступна новая Stable ' + available + '. XKeen не обновлялся; рабочий VPN сохранён.'
        : data.state === 'up-to-date' ? 'Установлена последняя Stable. Никаких изменений не требуется.'
        : 'Установленная версия новее текущей Stable. Смена канала и обновление не выполнялись.';
    } catch (_) {
      status.textContent = 'Не удалось достоверно проверить версию XKeen. Не считать обновление отсутствующим; ничего не изменено.';
    } finally {
      button.disabled = false;
    }
  }

  async function action(name) {
    const actionMap = {subscription:'subscription_check',geodata:'geodata_update',freenet:'freenet_check'};
    const action = actionMap[name] || name;
    if (action === 'geodata_update') { await startGeoDataOperation(); return; }
    if (action === 'xkeen_check') { await checkXKeenUpdate(); return; }
    try {
      if (action === 'backup_restore') {
        const latest = state.data?.backup_info?.latest || '';
        const target = latest ? `снимок ${latest}` : 'последний снимок настроек FreeNet';
        if (!confirm(`Восстановить ${target}? Перед восстановлением FreeNet создаст отдельный аварийный snapshot текущих файлов.`)) return;
      }
      const data = await fetchJSON('/api/settings-v3/action', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action})});
      if (action === 'backup_create' || action === 'backup_restore') {
        showBackupActionResult(data, action);
        if (data.backup_info) renderBackupInfo(data.backup_info);
      } else if (data.message) {
        console.info(data.message);
      }
      await load();
    } catch (err) {
      if (action === 'backup_create' || action === 'backup_restore') showBackupActionError(err.message);
      else alert(`Операция не выполнена: ${err.message}`);
    }
  }

  async function checkAuto() {
    if (state.checking) return;
    state.checking = true;
    const btn = q('#fn3Check'); const started = Date.now();
    try {
      await fetchJSON('/api/automation', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'check'})});
      for (;;) {
        const s = await fetchJSON('/api/automation/check', {cache:'no-store'});
        const sec = Math.max(1, Math.floor((Date.now()-started)/1000));
        if (btn) q('span',btn).textContent = `Проверяем… ${sec} с`;
        if (!s.active) break;
        await new Promise(r => setTimeout(r, 1200));
      }
      await load();
    } catch (err) { alert(`Проверка не завершена: ${err.message}`); }
    finally { state.checking = false; if (btn) q('span',btn).textContent = 'Проверить сейчас'; }
  }

  function countryPopupHeader() {
    return `<div style="display:flex;justify-content:space-between;align-items:center"><strong>Выбранные страны</strong><button type="button" id="fn3PopClose" class="fn3-link">Закрыть</button></div>`;
  }

  function positionCountryPopup(pop) {
    const selected = q('[data-scope-card="allowlist"]'); const r = selected?.getBoundingClientRect();
    pop.style.left = `${Math.min(window.innerWidth-540, Math.max(14, r?.left || 100))}px`;
    pop.style.top = `${Math.min(window.innerHeight-630, Math.max(14, (r?.bottom || 100)+6))}px`;
  }

  function bindCountryPopupActions() {
    const pop = q('#fn3CountryPop'); if (!pop) return;
    const close = q('#fn3PopClose', pop); if (close) close.onclick = () => pop.hidden = true;
    const apply = q('#fn3CountriesApply', pop);
    if (apply) apply.onclick = () => {
      state.countries = qa('.fn3-country-item input:checked',pop).map(x=>x.value);
      pop.hidden = true;
      markDirty();
    };
  }

  function renderCountryPopup() {
    const pop = q('#fn3CountryPop'); if (!pop) return;
    if (state.countryCatalogLoading && !state.countryCatalog.length) {
      pop.innerHTML = `${countryPopupHeader()}<div class="fn3-country-state">Получаем доступные страны из текущей подписки…</div>`;
      bindCountryPopupActions();
      return;
    }

    const rows = state.countryCatalog.map(option => {
      const code = String(option?.code || '').toLowerCase();
      if (!/^[a-z]{2}$/.test(code) || code === 'ru') return '';
      const checked = state.countries.includes(code) ? ' checked' : '';
      const unavailable = option.available === false;
      const note = unavailable ? '<small>Ранее выбрана — сейчас нет активного Extra-профиля</small>' : '';
      return `<label class="fn3-country-item${unavailable?' unavailable':''}"><input type="checkbox" value="${code}"${checked}>${countryFlagMarkup(code)}<span class="fn3-country-copy"><span>${escapeHTML(option.name || 'Страна VPN')}</span>${note}</span></label>`;
    }).filter(Boolean).join('');
    const warning = state.countryCatalogWarning ? `<div class="fn3-country-state">${escapeHTML(state.countryCatalogWarning)}</div>` : '';
    const empty = rows ? '' : '<div class="fn3-country-state">Доступные страны пока не найдены. Текущий сохранённый выбор не меняется.</div>';
    pop.innerHTML = `${countryPopupHeader()}${warning}${rows?`<div class="fn3-country-list">${rows}</div>`:''}${empty}<div class="fn3-pop-actions"><button id="fn3CountriesApply" class="btn primary" type="button"${rows?'':' disabled'}>Применить список</button></div>`;
    bindCountryPopupActions();
  }

  async function loadCountryCatalog(force = false) {
    if (state.countryCatalogLoading) return;
    if (state.countryCatalog.length && !force) return;
    state.countryCatalogLoading = true;
    state.countryCatalogWarning = '';
    renderCountryPopup();
    try {
      const data = await fetchJSON('/api/settings-v3/countries', {cache:'no-store'});
      state.countryCatalog = Array.isArray(data.countries) ? data.countries : [];
      state.countryCatalogFresh = !!data.fresh;
      state.countryCatalogWarning = data.warning || '';
      if (!state.countries.length && Array.isArray(data.selected)) state.countries = data.selected.slice();
    } catch (_) {
      state.countryCatalogWarning = 'Не удалось обновить каталог стран. Сохранённый выбор не изменён.';
    } finally {
      state.countryCatalogLoading = false;
      renderCountryPopup();
    }
  }

  function openCountries() {
    const pop = q('#fn3CountryPop'); if (!pop) return;
    pop.hidden = false;
    positionCountryPopup(pop);
    renderCountryPopup();
    void loadCountryCatalog(true);
  }

  function bind() {
    q('#fn3Save').onclick = save; q('#fn3MaintenanceSave').onclick = save; q('#fn3Check').onclick = checkAuto;
    document.addEventListener('freenet:controls-busy', event => {
      state.controlsBusy = !!event.detail;
      queueMicrotask(renderSave);
    });
    q('#fn3AutoEnabled').onchange = () => { q('#fn3AutoLabel').textContent = q('#fn3AutoEnabled').checked ? 'Включено' : 'Выключено'; markDirty(); };
    qa('input[name="fn3Mode"]').forEach(i => i.onchange = markDirty);
    q('#fn3HealthInterval').onchange = markDirty;
    q('#fn3EndpointInterval').onchange = markDirty;
    qa('input[name="fn3Scope"]').forEach(i => i.onchange = () => { if (i.value === 'allowlist') openCountries(); markDirty(); });
    ['subscription','geodata','freenet','backup'].forEach(k => { q(`#fn3_${k}_enabled`).onchange = markDirty; q(`#fn3_${k}_interval`).onchange = markDirty; });
    qa('[data-v3-action]').forEach(b => b.onclick = () => action(b.dataset.v3Action));
    q('#fn3AllEvents').onclick = () => window.openFreeNetJournal('all');
    q('[data-scope-card="allowlist"]').ondblclick = openCountries;
  }

  function ensureSettingsPage() {
    let page = q('[data-page-view="settings"]');
    if (page) return page;
    const content = q('.content');
    if (!content) return null;
    page = document.createElement('section');
    page.className = 'page fn3-page';
    page.dataset.pageView = 'settings';
    content.appendChild(page);
    return page;
  }

  function bindSettingsSectionFolding(page) {
    const headers = '.fn3-card-head[role="button"],.fn3-extra-title[role="button"],.fn3-extra-head[role="button"],.fn3-dns-head[role="button"],.fn3-subscription-head[role="button"]';
    const toggle = (header) => {
      const card = header.closest('.fn3-extra-card,.fn3-card');
      if (!card) return;
      const collapsed = card.classList.toggle('fn3-section-collapsed');
      header.setAttribute('aria-expanded', String(!collapsed));
      // One action opens the whole Maintenance workspace. Its child cards
      // remain individually foldable afterwards, and re-entry still resets all.
      if (header.classList.contains('fn3-extra-title') && !collapsed) {
        card.querySelectorAll('.fn3-extra-card').forEach(child => {
          child.classList.remove('fn3-section-collapsed');
          const childHead = child.querySelector('.fn3-extra-head[aria-expanded]');
          if (childHead) childHead.setAttribute('aria-expanded', 'true');
        });
      }
    };
    page.addEventListener('click', event => {
      const header = event.target.closest?.(headers);
      if (!header || !page.contains(header) || event.target.closest?.('button,a,input,select,textarea,label')) return;
      toggle(header);
    });
    page.addEventListener('keydown', event => {
      if (!['Enter',' '].includes(event.key)) return;
      const header = event.target.closest?.(headers);
      if (!header || event.target !== header) return;
      event.preventDefault();
      toggle(header);
    });
  }

  function collapseSettingsSections(page) {
    if (!page) return;
    page.querySelectorAll('.fn3-card,.fn3-extra-card').forEach(card => {
      card.classList.add('fn3-section-collapsed');
      const header = card.querySelector('[role="button"][aria-expanded]');
      if (header) header.setAttribute('aria-expanded', 'false');
    });
  }

  function mountSettings() {
    const page = ensureSettingsPage();
    if (!page) return false;
    ; rewireNavigation();
    if (page.dataset.settingsV3 === '1') return true;
    page.dataset.settingsV3 = '1';
    page.classList.add('fn3-page');
    page.innerHTML = pageMarkup();
    bindSettingsSectionFolding(page);
    collapseSettingsSections(page);
    // The canonical nav may hold a direct reference to the original setPage
    // function. Capture real Settings navigation before either handler runs.
    document.addEventListener('click', event => {
      const settingsNav = event.target.closest?.('.sidebar .nav-btn[data-page="settings"]');
      if (settingsNav && !page.classList.contains('active')) collapseSettingsSections(page);
    }, true);
    if (typeof window.setPage === 'function' && !window.setPage.__fn3CollapsedOnEntry) {
      const originalSetPage = window.setPage;
      const foldedSetPage = function(pageName) {
        if (pageName === 'settings' && !page.classList.contains('active')) collapseSettingsSections(page);
        return originalSetPage.apply(this, arguments);
      };
      foldedSetPage.__fn3CollapsedOnEntry = true;
      window.setPage = foldedSetPage;
    }
    document.dispatchEvent(new CustomEvent('freenet:settings-v3-mounted'));
    bind(); load(); void resumeGeoDataOperation();
    return true;
  }

  function journalPageActive() {
    const page = q('[data-page-view="journal"]');
    return !!page && document.visibilityState !== 'hidden' &&
      (page.classList.contains('active') || location.hash.slice(1) === 'journal');
  }

  function journalStatsFallback(events) {
    const list = Array.isArray(events) ? events : [];
    const stats = {total:list.length,success:0,neutral:0,errors:0};
    list.forEach(event => {
      const cls = humanResult(event?.result, event?.message)[2];
      if (cls === 'ok') stats.success += 1;
      else if (cls === 'bad') stats.errors += 1;
      else stats.neutral += 1;
    });
    return stats;
  }

  function renderJournalSummary() {
    const host = q('#fn3JournalSummary'); if (!host) return;
    const stats = state.journalStats || journalStatsFallback(state.journalEvents);
    host.innerHTML = `
      <div class="fn3-journal-stat"><span>Событий в выборке</span><strong>${Number(stats.total || state.journalFilteredTotal || 0)}</strong></div>
      <div class="fn3-journal-stat ok"><span>Успешно</span><strong>${Number(stats.success || 0)}</strong></div>
      <div class="fn3-journal-stat"><span>Служебные / без изменений</span><strong>${Number(stats.neutral || 0)}</strong></div>
      <div class="fn3-journal-stat bad"><span>Ошибки</span><strong>${Number(stats.errors || 0)}</strong></div>`;
  }


  function renderJournalAutoDecision() {
    const host = q('#fn3JournalAutoDecision'); if (!host) return;
    const decision = state.journalDecision || {};
    const health = state.journalHealth || {};
    const names = {same:'оставлен без изменений',cooldown:'отложен',switched:'переключён',updated:'обновлён',
      failed:'не выполнено',uncertain:'не подтверждено',candidate:'найден кандидат',blocked:'заблокировано'};
    const lastCheck = health.last_completed
      ? formatDate(health.last_completed) + (health.freshness === 'stale' ? ' · запись устарела' : '')
      : 'нет завершённой проверки';
    const decisionResult = String(decision.decision_result || '');
    const lastDecision = decision.decision_at && decisionResult
      ? formatDate(decision.decision_at) + ' · ' + (names[decisionResult] || decisionResult)
      : 'нет записанного решения';
    const origin = decision.last_switch_origin === 'manual' ? 'вручную' :
      decision.last_switch_origin === 'auto' ? 'AUTO VPN' : 'источник не определён';
    const lastSwitch = decision.last_switch_at
      ? formatDate(decision.last_switch_at) + ' · ' + origin
      : 'нет подтверждённой записи';
    const rows = [
      ['Активный профиль', decision.current_profile || 'Точное имя не определено'],
      ['Последняя проверка', lastCheck],
      ['Результат проверки', health.last_message || 'Причина не записана'],
      ['Последнее решение AUTO', lastDecision],
      ['Причина решения', decision.decision_reason || 'Нет записанного объяснения'],
      ['Последнее переключение', lastSwitch]
    ];
    host.replaceChildren();
    for (const [title, value] of rows) {
      const line = document.createElement('div');
      line.className = 'fn3-auto-decision-row';
      const label = document.createElement('span');
      label.textContent = title;
      const text = document.createElement('strong');
      text.textContent = value;
      line.append(label, text);
      host.append(line);
    }
    const note = document.createElement('p');
    note.className = 'fn3-auto-decision-note';
    note.textContent = 'Решение, проверка и смена — независимые события. Старая запись не доказывает, что AUTO выбрал активный профиль. История ниже содержит исходные причины и результаты.';
    host.append(note);
  }

  function journalRangeParams() {
    const now = new Date();
    let from = '', to = '';
    if (state.journalDatePreset === '24h') {
      from = new Date(now.getTime() - 24 * 60 * 60 * 1000).toISOString();
      to = now.toISOString();
    } else if (state.journalDatePreset === '7d') {
      from = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000).toISOString();
      to = now.toISOString();
    } else if (state.journalDatePreset === 'today') {
      const start = new Date(now.getFullYear(), now.getMonth(), now.getDate());
      from = start.toISOString();
      to = now.toISOString();
    } else if (state.journalDatePreset === 'custom') {
      if (state.journalCustomFrom) from = new Date(state.journalCustomFrom).toISOString();
      if (state.journalCustomTo) to = new Date(state.journalCustomTo).toISOString();
    }
    return {from,to};
  }

  function journalQueryString(includePage = true) {
    const params = new URLSearchParams();
    if (includePage) {
      params.set('page', String(state.journalPage || 1));
      params.set('page_size', String(state.journalPageSize || 100));
    }
    if (state.journalFilter !== 'all') params.set('category', state.journalFilter);
    if (state.journalResultFilter !== 'all') params.set('result', state.journalResultFilter);
    if (state.journalQuery) params.set('q', state.journalQuery);
    const range = journalRangeParams();
    if (range.from) params.set('from', range.from);
    if (range.to) params.set('to', range.to);
    return params.toString();
  }

  function renderJournalPageState() {
    const page = q('[data-page-view="journal"]'); if (!page || page.dataset.journalV4 !== '1') return;
    qa('[data-journal-filter]', page).forEach(button => button.classList.toggle('active', button.dataset.journalFilter === state.journalFilter));
    qa('[data-journal-result]', page).forEach(button => button.classList.toggle('active', button.dataset.journalResult === state.journalResultFilter));
    qa('[data-journal-range]', page).forEach(button => button.classList.toggle('active', button.dataset.journalRange === state.journalDatePreset));
    const custom = q('#fn3JournalCustomRange', page);
    if (custom) custom.hidden = state.journalDatePreset !== 'custom';
    const search = q('#fn3JournalSearch', page);
    if (search && document.activeElement !== search && search.value !== state.journalQuery) search.value = state.journalQuery;
    const advanced = q('#fn3JournalAdvancedFilters', page);
    if (advanced) advanced.hidden = !state.journalFiltersOpen;
    const activeFilterCount = [
      state.journalDatePreset !== '24h',
      state.journalFilter !== 'all',
      state.journalResultFilter !== 'all'
    ].filter(Boolean).length;
    const filterToggle = q('#fn3JournalFiltersToggle', page);
    if (filterToggle) {
      filterToggle.classList.toggle('active', state.journalFiltersOpen || activeFilterCount > 0);
      filterToggle.setAttribute('aria-expanded', state.journalFiltersOpen ? 'true' : 'false');
      filterToggle.innerHTML = `Фильтры${activeFilterCount ? `<span class="fn3-journal-filter-count">${activeFilterCount}</span>` : ''}`;
    }
    const live = q('#fn3JournalLive', page);
    if (live) {
      live.classList.toggle('active', state.journalLive);
      live.setAttribute('aria-pressed', state.journalLive ? 'true' : 'false');
      live.setAttribute('title', state.journalLive ? 'Остановить автообновление журнала' : 'Возобновить автообновление журнала');
      live.innerHTML = state.journalLive
        ? '<span class="fn3-journal-live-dot"></span>Пауза'
        : 'Возобновить';
    }
    const meta = q('#fn3JournalMeta', page);
    if (meta) {
      meta.classList.toggle('bad', !!state.journalError);
      const retained = state.journalRetainedFrom && state.journalRetainedTo
        ? ` · архив ${formatDate(state.journalRetainedFrom)} → ${formatDate(state.journalRetainedTo)}`
        : '';
      meta.textContent = state.journalError
        ? `Не удалось обновить журнал: ${state.journalError}`
        : state.journalRefreshing
          ? 'Обновляем read-only журнал…'
          : state.journalGeneratedAt
            ? `Сохранено ${state.journalTotal} событий · выбрано ${state.journalFilteredTotal}${retained} · read-only`
            : 'Read-only журнал готов к обновлению.';
    }
    // The last canonical event is not the last completed health check.
    // A compacted/no-change Journal must never silently imply a live watchdog.
    const monitor = q('#fn3JournalHealth', page);
    if (monitor) {
      const health = state.journalHealth;
      const freshness = health?.freshness || 'unknown';
      monitor.classList.toggle('fresh', freshness === 'fresh');
      monitor.classList.toggle('stale', freshness === 'stale');
      monitor.classList.toggle('disabled', freshness === 'disabled');
      const last = health?.last_completed ? formatDate(health.last_completed) : 'нет записи';
      const result = ({
        healthy: 'норма по тесту', success: 'норма по тесту',
        failed: 'отказ', critical: 'отказ', uncertain: 'не подтверждено',
        switched: 'переключено', updated: 'обновлено'
      })[health?.result] || 'неизвестно';
      monitor.textContent = freshness === 'disabled'
        ? 'AUTO VPN: выключен'
        : freshness === 'stale'
          ? `AUTO VPN: нет свежей завершённой проверки (последняя: ${last})`
          : freshness === 'fresh'
            ? `AUTO VPN: проверка ${last} · ${result}`
            : 'AUTO VPN: время последней проверки неизвестно';
      monitor.title = 'Завершённая диагностическая проверка AUTO VPN, не проверка клиентского UDP-трафика. Архив событий агрегируется отдельно.';
    }
    const retention = q('#fn3JournalRetentionRange', page);
    if (retention) {
      retention.textContent = state.journalRetainedFrom && state.journalRetainedTo
        ? `Архив: ${formatDate(state.journalRetainedFrom)} → ${formatDate(state.journalRetainedTo)}`
        : 'Архив: данных пока нет';
    }
    const pageLabel = q('#fn3JournalPageLabel', page);
    if (pageLabel) pageLabel.textContent = `Страница ${state.journalPage} из ${state.journalPages}`;
    const prev = q('#fn3JournalPrev', page); if (prev) prev.disabled = state.journalPage <= 1;
    const next = q('#fn3JournalNext', page); if (next) next.disabled = state.journalPage >= state.journalPages;
    const size = q('#fn3JournalPageSize', page); if (size) size.value = String(state.journalPageSize);
    renderJournalSummary();
    renderJournalAutoDecision();
    renderJournal(state.journalEvents, '#fn3JournalFull');
  }

  async function loadJournal(force = false) {
    if (state.journalRefreshing) return;
    if (!force && (!state.journalLive || !journalPageActive())) return;
    if (document.visibilityState === 'hidden') return;
    state.journalRefreshing = true;
    state.journalError = '';
    renderJournalPageState();
    try {
      const data = await fetchJSON('/api/journal?' + journalQueryString(true), {cache:'no-store'});
      const rawEvents = Array.isArray(data.events) ? data.events : [];
      const legacyEventOnlyPayload = data.total == null && data.filtered_total == null && data.stats == null;
      state.journalEvents = legacyEventOnlyPayload ? filteredJournalEvents(rawEvents) : rawEvents;
      state.journalGeneratedAt = data.generated_at || new Date().toISOString();
      state.journalTotal = Number(data.total ?? rawEvents.length);
      state.journalFilteredTotal = Number(data.filtered_total ?? state.journalEvents.length);
      state.journalPage = Number(data.page || 1);
      state.journalPages = Number(data.pages || 1);
      state.journalPageSize = Number(data.page_size || state.journalPageSize || 100);
      state.journalStats = data.stats || journalStatsFallback(state.journalEvents);
      state.journalRetainedFrom = data.retained_from || '';
      state.journalRetainedTo = data.retained_to || '';
      state.journalHealth = data.health && typeof data.health === 'object' ? data.health : null;
      state.journalDecision = data.auto_decision && typeof data.auto_decision === 'object' ? data.auto_decision : null;
    } catch (err) {
      state.journalError = err?.message || 'неизвестная ошибка чтения';
    } finally {
      state.journalRefreshing = false;
      renderJournalPageState();
    }
  }

  function ensureJournalLiveTimer() {
    if (state.journalTimer) return;
    state.journalTimer = window.setInterval(() => {
      if (journalPageActive() && state.journalLive) void loadJournal(false);
    }, 5000);
  }

  function resetJournalPageAndLoad() {
    state.journalPage = 1;
    void loadJournal(true);
  }

  // Read-only transaction checkpoint display: never unlocks or restarts Xray.
  async function loadVPNTransactionDoctor() {
    const target = q('#fn3VPNTransactionDoctor');
    if (!target) return;
    target.textContent = 'Проверяем защищённую VPN-транзакцию…';
    try {
      const response = await fetch('/api/vpn/transaction-doctor', {cache:'no-store', signal:AbortSignal.timeout(10000)});
      if (!response.ok) throw new Error('status '+response.status);
      const data = await response.json();
      if (data.mode !== 'read-only' || data.mutation !== 'NONE') throw new Error('unexpected mutation policy');
      const blocked = data.mutation_blocked === true || data.checkpoint !== 'absent';
      const labels = {matched:'совпадает',different:'изменён',missing:'отсутствует',
        invalid:'повреждён',unknown:'не определён','not-applicable':'не требуется'};
      const detail = key => labels[data[key]] || 'не определён';
      target.replaceChildren();
      const headline = document.createElement('strong');
      headline.textContent = blocked ? 'VPN-транзакция: защитная блокировка активна' : 'VPN-транзакция: незавершённого checkpoint нет';
      target.append(headline);
      const status = document.createElement('p');
      status.textContent = blocked
        ? 'Никакие переключения VPN не выполняйте, пока исходное состояние не подтверждено. Ручной сброс checkpoint запрещён.'
        : 'Защищённый checkpoint отсутствует. Это не является проверкой доступности VPN.';
      target.append(status);
      if (blocked) {
        const detailNode = document.createElement('p');
        const core = data.xray_core === 'running' ? 'процесс запущен' : 'не подтверждён';
        detailNode.textContent = 'Исходящие: '+detail('outbound')+' · Профиль: '+detail('profile')+
          ' · Фильтр: '+detail('filter')+' · Xray: '+core+' · Решение: '+String(data.decision||'STOP_UNKNOWN');
        target.append(detailNode);
      }
    } catch (_) {
      target.textContent = 'Не удалось получить read-only состояние VPN-транзакции. Статус защиты неизвестен: не запускайте переключение без диагностики.';
    }
  }

  function mountJournalPage() {
    const page = q('[data-page-view="journal"]'); if (!page) return;
    state.journalFiltersOpen = false;
    const filters = [['all','Все'],['vpn','VPN'],['auto','AUTO VPN'],['subscription','Подписка'],['system','Система']];
    const results = [['all','Все результаты'],['ok','Успешно'],['neutral','Служебные / без изменений'],['bad','Ошибки']];
    const ranges = [['today','Сегодня'],['24h','24 часа'],['7d','7 дней'],['custom','Интервал']];
    if (page.dataset.journalV4 !== '1') {
      page.dataset.journalV4 = '1';
      page.innerHTML = `<section class="fn3-journal-hero">
        <div class="fn3-journal-kicker">СОБЫТИЯ И ДИАГНОСТИКА</div>
        <h1>Журнал</h1>
        <p>История значимых событий FreeNet: VPN, AUTO VPN, обновления и системные действия. Повторяющиеся штатные проверки объединяются. Видны причина и результат, а полные детали доступны по нажатию. Отдельный технический лог помогает разбирать инциденты.</p>
        <div class="fn3-journal-policy">
          <span>До 15 000 значимых событий</span>
          <span class="healthy">Штатная отметка — не чаще 1 раза в 6 часов</span>
          <span id="fn3JournalRetentionRange">Архив загружается…</span>
          <span id="fn3JournalHealth" class="fn3-journal-health" role="status" aria-live="polite">AUTO VPN: проверяем время последней проверки…</span>
        </div>
      </section>
      <section class="fn3-journal-control-card">
        <div class="fn3-journal-toolbar"><input id="fn3JournalSearch" class="fn3-journal-search" type="search" autocomplete="off" placeholder="Поиск: сервер, AUTO, ошибка, обновление, 189 мс…"><div class="fn3-journal-actions"><button id="fn3JournalFiltersToggle" class="fn3-journal-filter-toggle" type="button" aria-expanded="false">Фильтры</button><button id="fn3JournalExport" class="fn3-journal-export" type="button">Экспорт CSV</button><button id="fn3JournalDiagnostics" class="fn3-journal-export" type="button" title="Недавние исходные события без запуска проверок VPN">Скачать технический лог</button><button id="fn3JournalArchive30d" class="fn3-journal-export" type="button" title="Архив значимых технических событий до 30 дней, потоковый сжатый JSONL, без новых проверок VPN">Архив за 30 дней (.gz)</button><button id="fn3JournalLive" class="fn3-journal-live active" type="button" aria-pressed="true" title="Остановить автообновление журнала"><span class="fn3-journal-live-dot"></span>Пауза</button><button id="fn3JournalRefresh" class="fn3-journal-refresh" type="button">Обновить</button></div></div>
        <div id="fn3JournalMeta" class="fn3-journal-meta">Журнал показывает значимые события; технический лог содержит отдельную хронологию, состояния защит и исходные этапы без агрегации.</div>
        <div id="fn3JournalAdvancedFilters" class="fn3-journal-filter-groups" hidden>
          <div class="fn3-journal-filter-row"><span class="fn3-journal-filter-title">Период</span><div class="fn3-journal-range">${ranges.map(([key,label]) => `<button class="fn3-journal-filter" type="button" data-journal-range="${key}">${label}</button>`).join('')}<span id="fn3JournalCustomRange" class="fn3-journal-custom" hidden><input id="fn3JournalFrom" type="datetime-local" aria-label="Начало периода"><input id="fn3JournalTo" type="datetime-local" aria-label="Конец периода"></span></div></div>
          <div class="fn3-journal-filter-row"><span class="fn3-journal-filter-title">Событие</span><div class="fn3-journal-filters">${filters.map(([key,label]) => `<button class="fn3-journal-filter" type="button" data-journal-filter="${key}">${label}</button>`).join('')}</div></div>
          <div class="fn3-journal-filter-row"><span class="fn3-journal-filter-title">Результат</span><div class="fn3-journal-filters">${results.map(([key,label]) => `<button class="fn3-journal-filter" type="button" data-journal-result="${key}">${label}</button>`).join('')}</div></div>
        </div>
      </section>
      <section class="fn3-journal-control-card" aria-label="История решений AUTO VPN">
        <div class="fn3-journal-stream-head"><h2>Почему сейчас этот VPN</h2><p>Последние зафиксированные проверки и решения AUTO · без запуска диагностики и переключений</p></div>
        <div id="fn3JournalAutoDecision" class="fn3-auto-decision" role="status" aria-live="polite">Читаем состояние AUTO VPN…</div>
        <button id="fn3JournalAutoHistory" class="fn3-journal-refresh" type="button">История решений AUTO VPN за 7 дней</button>
      </section>
      <section class="fn3-journal-control-card" aria-label="Состояние восстановления VPN">
        <div class="fn3-journal-stream-head"><h2>Восстановление VPN · только диагностика</h2></div>
        <div id="fn3VPNTransactionDoctor" role="status" aria-live="polite">Проверяем защищённую VPN-транзакцию…</div>
      </section>
      <div id="fn3JournalSummary" class="fn3-journal-summary"></div>
      <section class="fn3-journal-stream-card">
        <div class="fn3-journal-stream-head"><div><h2>События</h2><p>Новое сверху · подробные этапы инцидента сохраняются без агрегации</p></div></div>
        <div id="fn3JournalFull" class="fn3-journal-event-list"></div>
        <div class="fn3-journal-pager"><span class="fn3-journal-retention">Обычный журнал ограничен по строкам; технические события сохраняются по UTC-дням до 30 дней. Если дневной лимит достигнут, архив помечает пропуск.</span><div class="fn3-journal-pager-controls"><label class="fn3-journal-page-label">На странице <select id="fn3JournalPageSize" class="fn3-journal-page-size"><option>50</option><option selected>100</option><option>200</option><option>500</option></select></label><button id="fn3JournalPrev" class="fn3-journal-page-btn" type="button">← Назад</button><span id="fn3JournalPageLabel" class="fn3-journal-page-label">Страница 1 из 1</span><button id="fn3JournalNext" class="fn3-journal-page-btn" type="button">Вперёд →</button></div></div>
      </section>`;

      q('#fn3JournalAutoHistory', page).onclick = () => {
        state.journalFilter = 'auto';
        state.journalResultFilter = 'all';
        state.journalQuery = '';
        state.journalDatePreset = '7d';
        state.journalPage = 1;
        void loadJournal(true);
      };
      qa('[data-journal-filter]', page).forEach(button => button.onclick = () => {
        state.journalFilter = button.dataset.journalFilter || 'all';
        resetJournalPageAndLoad();
      });
      qa('[data-journal-result]', page).forEach(button => button.onclick = () => {
        state.journalResultFilter = button.dataset.journalResult || 'all';
        resetJournalPageAndLoad();
      });
      qa('[data-journal-range]', page).forEach(button => button.onclick = () => {
        state.journalDatePreset = button.dataset.journalRange || '24h';
        renderJournalPageState();
        if (state.journalDatePreset !== 'custom') resetJournalPageAndLoad();
      });
      const from = q('#fn3JournalFrom', page);
      const to = q('#fn3JournalTo', page);
      if (from) from.onchange = () => { state.journalCustomFrom = from.value || ''; if (state.journalCustomTo) resetJournalPageAndLoad(); };
      if (to) to.onchange = () => { state.journalCustomTo = to.value || ''; if (state.journalCustomFrom) resetJournalPageAndLoad(); };
      const search = q('#fn3JournalSearch', page);
      if (search) search.oninput = () => {
        state.journalQuery = search.value || '';
        window.clearTimeout(state.journalSearchTimer);
        state.journalSearchTimer = window.setTimeout(resetJournalPageAndLoad, 300);
      };
      const filtersToggle = q('#fn3JournalFiltersToggle', page);
      if (filtersToggle) filtersToggle.onclick = () => {
        state.journalFiltersOpen = !state.journalFiltersOpen;
        renderJournalPageState();
      };
      const live = q('#fn3JournalLive', page);
      if (live) live.onclick = () => {
        state.journalLive = !state.journalLive;
        renderJournalPageState();
        if (state.journalLive) void loadJournal(true);
      };
      const refresh = q('#fn3JournalRefresh', page);
      if (refresh) refresh.onclick = () => void loadJournal(true);
      const exportButton = q('#fn3JournalExport', page);
      if (exportButton) exportButton.onclick = () => {
        window.location.href = '/api/journal/export?' + journalQueryString(false);
      };
      const diagnosticButton = q('#fn3JournalDiagnostics', page);
      if (diagnosticButton) diagnosticButton.onclick = () => {
        window.location.href = '/api/journal/diagnostics';
      };
      const archiveButton = q('#fn3JournalArchive30d', page);
      if (archiveButton) archiveButton.onclick = () => {
        window.location.href = '/api/journal/technical-archive';
      };
      const pageSize = q('#fn3JournalPageSize', page);
      if (pageSize) pageSize.onchange = () => {
        state.journalPageSize = Number(pageSize.value || 100);
        resetJournalPageAndLoad();
      };
      const prev = q('#fn3JournalPrev', page);
      if (prev) prev.onclick = () => { if (state.journalPage > 1) { state.journalPage -= 1; void loadJournal(true); } };
      const next = q('#fn3JournalNext', page);
      if (next) next.onclick = () => { if (state.journalPage < state.journalPages) { state.journalPage += 1; void loadJournal(true); } };
    }
    renderJournalPageState();
    ensureJournalLiveTimer();
    void loadVPNTransactionDoctor();
    void loadJournal(true);
  }

  window.openFreeNetJournal = filter => {
    const allowed = new Set(['all','vpn','auto','subscription','system']);
    state.journalFilter = allowed.has(String(filter || '')) ? String(filter) : 'all';
    if (typeof window.setPage === 'function') window.setPage('journal');
    mountJournalPage();
  };

  function boot() {
    rewireNavigation();
    mountSettings();
    ensureJournalLiveTimer();
    document.addEventListener('visibilitychange', () => {
      if (journalPageActive() && state.journalLive) void loadJournal(false);
    });
    window.addEventListener('hashchange', () => {
      if (location.hash.slice(1) === 'journal') mountJournalPage();
    });
    if (location.hash.slice(1) === 'settings' && typeof window.setPage === 'function') window.setPage('settings');
    if (location.hash.slice(1) === 'journal') mountJournalPage();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot, {once:true});
  else boot();
})();