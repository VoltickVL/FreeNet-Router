;(() => {
  'use strict';
  if (window.__freenetSettingsV3Loaded) return;
  window.__freenetSettingsV3Loaded = true;

  const q = (s, r = document) => r.querySelector(s);
  const qa = (s, r = document) => Array.from(r.querySelectorAll(s));
  const state = { data: null, status: null, baseline: '', dirty: false, saving: false, checking: false, countries: [] };

  const svg = (name) => {
    const paths = {
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

  function injectStyles() {
    if (q('#freenetSettingsV3Styles')) return;
    const style = document.createElement('style');
    style.id = 'freenetSettingsV3Styles';
    style.textContent = `
      body:has([data-page-view="settings"].active) .content{width:min(1460px,calc(100% - 48px))!important;padding-top:14px!important}
      body:has([data-page-view="settings"].active) #pageTitle{display:none!important}
      .fn3-page{display:block!important}.fn3-head{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;margin:0 0 13px}.fn3-head h1{font-size:30px;line-height:1.05;margin:0;letter-spacing:-.04em}.fn3-head p{margin:6px 0 0;color:#8fa7c3;font-size:12px}.fn3-save{min-width:210px;min-height:42px!important;justify-content:center!important}.fn3-save[disabled]{opacity:.52;filter:saturate(.7)}
      .fn3-grid{display:grid;grid-template-columns:minmax(0,1.06fr) minmax(0,1fr);gap:14px;align-items:start}.fn3-left,.fn3-right{display:grid;gap:14px}.fn3-card{border:1px solid #244a6d;border-radius:14px;background:linear-gradient(180deg,rgba(12,31,52,.98),rgba(7,22,38,.99));padding:15px;box-shadow:0 12px 32px rgba(0,0,0,.12);min-width:0}.fn3-card-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}.fn3-title{display:flex;align-items:flex-start;gap:11px}.fn3-icon{display:grid;place-items:center;width:36px;height:36px;flex:0 0 36px;border-radius:10px;background:linear-gradient(180deg,#1763d0,#114692);color:#b8d6ff}.fn3-icon svg{width:22px;height:22px}.fn3-card h2{font-size:19px;margin:0;letter-spacing:-.02em}.fn3-card h3{font-size:15px;margin:0}.fn3-sub{margin-top:3px;color:#8fa6c0;font-size:11px;line-height:1.4}
      .fn3-master{display:flex;align-items:center;gap:9px;font-size:12px;font-weight:750;color:#eef5ff}.fn3-switch{position:relative;width:45px;height:24px;display:inline-block;flex:0 0 45px}.fn3-switch input{position:absolute;opacity:0;pointer-events:none}.fn3-switch span{position:absolute;inset:0;border-radius:999px;background:#3b4c61;border:1px solid #586a80;transition:.15s}.fn3-switch span:after{content:'';position:absolute;top:2px;left:2px;width:18px;height:18px;border-radius:50%;background:#eef5fc;transition:.15s;box-shadow:0 2px 5px #0006}.fn3-switch input:checked+span{background:#18c78c;border-color:#34e3a8}.fn3-switch input:checked+span:after{transform:translateX(21px);background:white}
      .fn3-info{display:flex;gap:10px;align-items:flex-start;margin-top:12px;padding:11px 12px;border:1px solid #2c6093;border-radius:10px;background:rgba(23,75,132,.23);font-size:11px;line-height:1.48;color:#bed0e4}.fn3-info svg{width:19px;height:19px;flex:0 0 19px;color:#69a9ff}.fn3-section-label{margin:13px 0 7px;color:#f1f6ff;font-size:12px;font-weight:800}.fn3-scope-list{border:1px solid #295174;border-radius:11px;overflow:hidden;background:#081b2f}.fn3-scope{display:grid;grid-template-columns:auto 1fr auto;gap:11px;align-items:center;padding:10px 12px;border-top:1px solid #254765;cursor:pointer;min-height:58px}.fn3-scope:first-child{border-top:0}.fn3-scope.selected{background:linear-gradient(180deg,#0d345c,#0a2a4b);box-shadow:inset 0 0 0 1px #2f8cf8}.fn3-scope input{width:18px;height:18px;accent-color:#31e1a3}.fn3-scope strong{display:block;font-size:13px}.fn3-scope small{display:block;color:#9ab0c9;font-size:10.5px;margin-top:3px}.fn3-recommended{display:inline-block;margin-left:6px;padding:2px 6px;border-radius:999px;background:#0b6e50;color:#67f1bc;font-size:9px}.fn3-chevron{width:18px;color:#7fb7f8}.fn3-safety{display:flex;gap:9px;align-items:flex-start;margin-top:10px;padding:10px 11px;border:1px solid #2b5f91;border-radius:9px;color:#b7cbe1;font-size:10.5px;line-height:1.42}.fn3-safety svg{width:18px;flex:0 0 18px;color:#6daeff}.fn3-auto-actions{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,.78fr);gap:10px;margin-top:11px}.fn3-auto-actions .btn{min-height:44px!important;justify-content:center!important}.fn3-control{display:flex;align-items:center;gap:9px;padding:9px 11px;border:1px solid #275174;border-radius:10px;background:#091b2e;color:#c6d6e8}.fn3-control-dot{width:20px;height:20px;border-radius:50%;border:4px solid #0b4c3a;background:#26d69b;box-shadow:0 0 0 2px #179d76}.fn3-control strong{display:block;color:#4ee3aa;font-size:12px}.fn3-control small{display:block;color:#879eb8;font-size:9.5px;margin-top:2px}
      .fn3-vpn-head{display:flex;align-items:center;justify-content:space-between}.fn3-status-pill{padding:5px 10px;border-radius:999px;background:#075b43;color:#55efb2;font-size:10px;font-weight:800}.fn3-profile{margin-top:10px;border:1px solid #294f70;border-radius:10px;background:#07192a;padding:12px}.fn3-profile-top{display:flex;align-items:center;gap:10px}.fn3-flag{font-size:22px;line-height:1}.fn3-profile-name{font-size:18px;font-weight:850}.fn3-profile-facts{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin-top:10px}.fn3-fact{padding-left:10px;border-left:1px solid #315170}.fn3-fact span{display:block;color:#8ba2bd;font-size:9px}.fn3-fact strong{display:block;margin-top:4px;font-size:12px}.fn3-endpoint{display:flex;align-items:center;gap:7px}.fn3-copy{border:0;background:transparent;color:#62a6f6;padding:2px;cursor:pointer}.fn3-copy svg{width:17px;height:17px}.fn3-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:7px;margin-top:9px}.fn3-metric{border:1px solid #294f70;border-radius:9px;background:#07192a;padding:8px;min-height:55px}.fn3-metric span{display:flex;align-items:center;gap:5px;color:#8da5c0;font-size:9px}.fn3-metric svg{width:14px;color:#66a8ff}.fn3-metric strong{display:block;margin-top:4px;font-size:12px}.fn3-health{display:flex;align-items:center;gap:9px;margin-top:9px;padding:9px 11px;border:1px solid #16815e;border-radius:9px;background:#073c30;color:#5ee7af;font-size:10.5px;font-weight:700}.fn3-health svg{width:18px}.fn3-health small{display:block;color:#b1d9ca;font-weight:500;margin-top:2px}
      .fn3-journal-head{display:flex;align-items:center;justify-content:space-between;margin-bottom:9px}.fn3-link{border:0;background:transparent;color:#92bdeb;font-size:10px;cursor:pointer}.fn3-table{width:100%;border-collapse:collapse;font-size:9.5px}.fn3-table th{text-align:left;padding:6px 7px;background:#123655;color:#a8bdd2}.fn3-table td{padding:6px 7px;border-top:1px solid #254765;color:#d2deea;vertical-align:top}.fn3-result{display:inline-flex;align-items:center;gap:5px;color:#52e4a8}.fn3-result.neutral{color:#b9c8d7}.fn3-dot{width:7px;height:7px;border-radius:50%;background:currentColor}
      .fn3-extra{grid-column:1/-1}.fn3-extra-title{margin-bottom:10px}.fn3-extra-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px}.fn3-extra-card{border:1px solid #2c5274;border-radius:11px;background:#081b2f;padding:11px;min-width:0}.fn3-extra-head{display:grid;grid-template-columns:auto 1fr auto;gap:9px;align-items:start}.fn3-extra-icon{display:grid;place-items:center;width:34px;height:34px;border-radius:9px;background:#1555a5;color:#b4d4ff}.fn3-extra-icon svg{width:20px;height:20px}.fn3-extra-card h3{font-size:12px}.fn3-extra-card p{margin:3px 0 0;color:#8fa5bf;font-size:9px;line-height:1.4}.fn3-extra-row{display:grid;grid-template-columns:auto minmax(0,1fr);gap:8px;align-items:center;margin-top:10px}.fn3-extra-row label{color:#9eb2c9;font-size:9.5px}.fn3-extra-row select{height:31px;border:1px solid #315777;border-radius:7px;background:#091a2b;color:#e7eff9;padding:0 8px;font-size:10px}.fn3-extra-meta{display:grid;grid-template-columns:auto 1fr;gap:7px;margin-top:7px;font-size:9px}.fn3-extra-meta span{color:#879db7}.fn3-extra-meta b{color:#dce7f2}.fn3-extra-meta b.ok{color:#4de0a5}.fn3-extra-action{width:100%;margin-top:9px;min-height:34px!important;justify-content:center!important;font-size:10px!important}.fn3-backup-actions{display:grid;grid-template-columns:1fr 1fr;gap:7px;margin-top:9px}.fn3-backup-actions .btn{min-height:34px!important;justify-content:center!important;font-size:9.5px!important}.fn3-danger{border-color:#9b3b51!important;color:#ffc0cc!important;background:#351725!important}
      .fn3-country-pop{position:fixed;z-index:1900;width:min(430px,calc(100vw - 28px));max-height:min(560px,calc(100vh - 36px));overflow:auto;padding:14px;border:1px solid #35638e;border-radius:14px;background:#091c30;box-shadow:0 24px 74px #0009}.fn3-country-pop[hidden]{display:none!important}.fn3-country-list{display:grid;grid-template-columns:1fr 1fr;gap:6px;margin-top:10px}.fn3-country-item{display:flex;align-items:center;gap:7px;padding:8px;border:1px solid #294f70;border-radius:8px;font-size:10px}.fn3-country-item input{accent-color:#2fdfa1}.fn3-pop-actions{display:flex;justify-content:flex-end;gap:8px;margin-top:11px}.fn3-compat{display:none!important}
      .fn3-journal-page .fn3-card{margin-top:12px}.fn3-journal-page h1{font-size:30px;margin:0}.fn3-journal-page p{color:#8fa6c0;margin:5px 0 0;font-size:12px}.fn3-journal-page .fn3-table{margin-top:12px;font-size:10px}
      @media(max-width:1120px){.fn3-grid{grid-template-columns:1fr}.fn3-extra{grid-column:auto}.fn3-extra-grid{grid-template-columns:1fr 1fr}}@media(max-width:760px){.fn3-head{display:block}.fn3-save{width:100%;margin-top:10px}.fn3-auto-actions,.fn3-profile-facts{grid-template-columns:1fr}.fn3-metrics{grid-template-columns:1fr 1fr}.fn3-extra-grid{grid-template-columns:1fr}.fn3-country-list{grid-template-columns:1fr}}
    `;
    document.head.appendChild(style);
  }

  function hideProviderTopbar() {
    qa('.topbar *').forEach(el => {
      const text = (el.textContent || '').trim();
      if ((text === 'Провайдер' || text.includes('Провайдер')) && el.children.length < 8) {
        let node = el;
        for (let i = 0; i < 3 && node.parentElement && !node.matches('.topbar-item,.top-stat,.top-chip'); i++) node = node.parentElement;
        (node.matches('.topbar-item,.top-stat,.top-chip') ? node : el).style.display = 'none';
      }
    });
  }

  function rewireNavigation() {
    const nav = q('.sidebar .nav');
    if (!nav) return;
    qa('.nav-btn', nav).forEach(btn => {
      const page = btn.dataset.page;
      if (page === 'system') btn.remove();
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
    if (!q('[data-page-view="journal"]')) {
      const page = document.createElement('section');
      page.className = 'page fn3-journal-page';
      page.dataset.pageView = 'journal';
      const content = q('.content');
      if (content) content.appendChild(page);
    }
    journal.onclick = () => {
      location.hash = '#journal';
      if (typeof window.setPage === 'function') window.setPage('journal');
      mountJournalPage();
    };
  }

  const formatDate = (value, compact = false) => {
    if (!value) return '—';
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return '—';
    const p = new Intl.DateTimeFormat('ru-RU', {day:'2-digit',month:'2-digit',hour:'2-digit',minute:'2-digit'}).format(d).replace(',', '');
    return compact ? p : p;
  };

  function intervalLabel(value) {
    return ({'30m':'30 минут','1h':'1 час','3h':'3 часа','6h':'6 часов','12h':'12 часов','24h':'24 часа'})[value] || '6 часов';
  }

  function nextLabel(value) {
    if (!value) return 'не запланировано';
    const ms = new Date(value).getTime() - Date.now();
    if (!Number.isFinite(ms)) return formatDate(value);
    if (ms <= 0) return 'в ближайшее время';
    const min = Math.max(1, Math.ceil(ms / 60000));
    if (min < 60) return `через ${min} мин`;
    const h = Math.floor(min / 60), m = min % 60;
    return `через ${h} ч${m ? ` ${m} мин` : ''}`;
  }

  function flag(code) {
    const cc = String(code || '').toLowerCase();
    if (!/^[a-z]{2}$/.test(cc)) return '🌐';
    return String.fromCodePoint(...cc.toUpperCase().split('').map(c => 127397 + c.charCodeAt(0)));
  }

  function humanResult(result, message) {
    const raw = String(result || '').toLowerCase();
    const msg = String(message || '');
    if (raw === 'success' || raw === 'healthy' || raw === 'switched' || raw === 'updated') return ['Успешно', translateMessage(msg), 'ok'];
    if (raw === 'same' || raw === 'no_new' || raw === 'candidate' || raw === 'disabled') return ['Без изменений', translateMessage(msg), 'neutral'];
    if (raw === 'uncertain' || raw === 'busy') return ['Пропущено', translateMessage(msg), 'neutral'];
    if (raw === 'failed' || raw === 'critical') return ['Ошибка', translateMessage(msg), 'neutral'];
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
      <div class="fn3-head"><div><h1>Настройки / Система</h1><p>Автоматизация, обновления и резервное копирование FreeNet.</p></div><button id="fn3Save" class="btn primary fn3-save" type="button" disabled>${svg('save')}<span>Сохранено</span></button></div>
      <div class="fn3-grid">
        <div class="fn3-left">
          <section class="fn3-card">
            <div class="fn3-card-head"><div class="fn3-title"><span class="fn3-icon">${svg('vpn')}</span><div><h2>AUTO VPN</h2><div class="fn3-sub">FreeNet автоматически поддерживает рабочий VPN.</div></div></div><label class="fn3-master"><span class="fn3-switch"><input id="fn3AutoEnabled" type="checkbox"><span></span></span><span id="fn3AutoLabel">Выключено</span></label></div>
            <div class="fn3-info">${svg('info')}<span>FreeNet каждые 5 минут проверяет доступность текущего VPN.<br>При сбое сначала пытается восстановить текущее подключение, а если это не помогает — автоматически выбирает проверенную замену в разрешённых странах.</span></div>
            <div class="fn3-section-label">Где искать замену VPN</div>
            <div class="fn3-scope-list">
              <label class="fn3-scope" data-scope-card="current"><input type="radio" name="fn3Scope" value="current"><span><strong>Текущая страна</strong><small>Замена только в той же стране — другой сервер или город.</small></span><span class="fn3-chevron">${svg('chevron')}</span></label>
              <label class="fn3-scope" data-scope-card="region"><input type="radio" name="fn3Scope" value="region"><span><strong>Ближайшие страны <em class="fn3-recommended">Рекомендуется</em></strong><small>Страны текущего региона — наиболее стабильный вариант.</small></span><span class="fn3-chevron">${svg('chevron')}</span></label>
              <label class="fn3-scope" data-scope-card="allowlist"><input type="radio" name="fn3Scope" value="allowlist"><span><strong>Выбранные страны</strong><small>Вы сами выбираете список стран.</small></span><span class="fn3-chevron">${svg('chevron')}</span></label>
            </div>
            <div class="fn3-safety">${svg('shield')}<span>Рабочий VPN не меняется без причины. Все переключения выполняются только при подтверждённом сбое, с проверкой и автоматическим возвратом при необходимости.</span></div>
            <div class="fn3-auto-actions"><button id="fn3Check" class="btn primary" type="button">${svg('play')}<span>Проверить сейчас</span></button><div class="fn3-control"><span class="fn3-control-dot"></span><div><strong id="fn3ControlTitle">Под контролем</strong><small id="fn3ControlNext">Следующая проверка: —</small></div></div></div>
          </section>
        </div>
        <div class="fn3-right">
          <section class="fn3-card">
            <div class="fn3-vpn-head"><h2>Текущий VPN</h2><span id="fn3VPNState" class="fn3-status-pill">Стабильно</span></div>
            <div class="fn3-profile"><div class="fn3-profile-top"><span id="fn3Flag" class="fn3-flag">🌐</span><div id="fn3Profile" class="fn3-profile-name">Определяем…</div></div><div class="fn3-profile-facts"><div class="fn3-fact"><span>Профиль</span><strong id="fn3ProfileSmall">—</strong></div><div class="fn3-fact"><span>Адрес подключения</span><div class="fn3-endpoint"><strong id="fn3Endpoint">—</strong><button id="fn3Copy" class="fn3-copy" type="button" title="Копировать">${svg('copy')}</button></div></div></div></div>
            <div class="fn3-metrics"><div class="fn3-metric"><span>${svg('clock')}Последняя проверка</span><strong id="fn3LastQuality">—</strong></div><div class="fn3-metric"><span>${svg('signal')}Отклик</span><strong id="fn3Latency">—</strong></div><div class="fn3-metric"><span>${svg('speed')}Скорость</span><strong id="fn3Speed">—</strong></div><div class="fn3-metric"><span>${svg('shield')}Стабильность</span><strong id="fn3Jitter">—</strong></div></div>
            <div class="fn3-health">${svg('check')}<div><span id="fn3HealthText">Текущий VPN работает стабильно.</span><small>При любых проблемах FreeNet автоматически восстановит подключение.</small></div></div>
          </section>
          <section class="fn3-card"><div class="fn3-journal-head"><h2>Журнал AUTO VPN</h2><button id="fn3AllEvents" class="fn3-link" type="button">Все события →</button></div><table class="fn3-table"><thead><tr><th>Время</th><th>Событие</th><th>Результат</th><th>Комментарий</th></tr></thead><tbody id="fn3Journal"></tbody></table></section>
        </div>
        <section class="fn3-card fn3-extra"><div class="fn3-extra-title"><h2>Дополнительные автоматизации</h2><div class="fn3-sub">Обновление данных, компонентов и резервное копирование.</div></div><div class="fn3-extra-grid">${extraCard('subscription','subscription','Обновление подписки','Автоматическое обновление данных подписки — списка доступных VPN.','Проверять','Проверить сейчас')}${extraCard('geodata','globe','GeoData / GeoIP','Данные геолокации, используемые для маршрутизации и фильтров.','Обновлять','Обновить сейчас')}${extraCard('freenet','box','Обновление FreeNet','Автоматическая проверка новых версий FreeNet. Установка — только после подтверждения.','Проверять','Проверить сейчас')}${backupCard()}</div></section>
      </div>
      <div class="fn3-compat"><select id="fnISP"><option value="custom">Свой</option></select><select id="fnDNS"><option value="firmware">DNS через роутер</option><option value="xkeen">Раздельный DNS</option></select><input id="fnAutoEnabled" type="checkbox"><select id="fnAutoInterval"><option value="manual">Вручную</option></select><input id="fnAutoApply" type="checkbox"><input type="radio" name="fnAutoMode" value="best"><input type="radio" name="fnAutoPolicy" value="degraded"><input type="radio" name="fnCountryScope" value="region"><input id="fnGeoDataEnabled" type="checkbox"><span id="fnGeoDataSchedule"></span><span id="fnAutoEnabledLabel"></span><span id="fnCurrentProfile"></span><span id="fnCurrentProfileSmall"></span><span id="fnCurrentEndpoint"></span><span id="fnCurrentFlag"></span><span id="fnWatchState"></span><span id="fnLastRun"></span><span id="fnLatency"></span><span id="fnSpeed"></span><span id="fnJitter"></span><span id="fnHealthBanner"></span><tbody id="fnJournalBody"></tbody><button id="fnSaveSettings"></button><button id="fnCheckNow"></button></div>
      <div id="fn3CountryPop" class="fn3-country-pop" hidden></div>`;
  }

  function intervalOptions(selected) {
    return ['30m','1h','3h','6h','12h','24h'].map(v => `<option value="${v}"${v===selected?' selected':''}>раз в ${intervalLabel(v)}</option>`).join('');
  }

  function extraCard(key, icon, title, description, verb, actionLabel) {
    return `<article class="fn3-extra-card"><div class="fn3-extra-head"><span class="fn3-extra-icon">${svg(icon)}</span><div><h3>${title}</h3><p>${description}</p></div><label class="fn3-switch"><input id="fn3_${key}_enabled" type="checkbox"><span></span></label></div><div class="fn3-extra-row"><label for="fn3_${key}_interval">${verb}</label><select id="fn3_${key}_interval"></select></div><div class="fn3-extra-meta"><span>Последняя проверка</span><b id="fn3_${key}_last">—</b><span>Следующая проверка</span><b id="fn3_${key}_next">—</b></div><button class="btn secondary fn3-extra-action" type="button" data-v3-action="${key}">${svg('refresh')}${actionLabel}</button></article>`;
  }

  function backupCard() {
    return `<article class="fn3-extra-card"><div class="fn3-extra-head"><span class="fn3-extra-icon">${svg('backup')}</span><div><h3>Резервное копирование</h3><p>Автоматическое создание резервной копии настроек FreeNet.</p></div><label class="fn3-switch"><input id="fn3_backup_enabled" type="checkbox"><span></span></label></div><div class="fn3-extra-row"><label for="fn3_backup_interval">Создавать</label><select id="fn3_backup_interval"></select></div><div class="fn3-extra-meta"><span>Последняя копия</span><b id="fn3_backup_last">—</b><span>Следующая копия</span><b id="fn3_backup_next">—</b></div><div class="fn3-backup-actions"><button class="btn secondary" type="button" data-v3-action="backup_create">${svg('download')}Создать копию</button><button class="btn secondary fn3-danger" type="button" data-v3-action="backup_restore">${svg('upload')}Восстановить</button></div></article>`;
  }

  function currentForm() {
    const scope = q('input[name="fn3Scope"]:checked')?.value || 'region';
    const read = key => ({enabled: !!q(`#fn3_${key}_enabled`)?.checked, interval: q(`#fn3_${key}_interval`)?.value || ''});
    return {enabled: !!q('#fn3AutoEnabled')?.checked, scope, countries: state.countries.slice().sort(), subscription:read('subscription'), geodata:read('geodata'), freenet:read('freenet'), backup:read('backup')};
  }

  function formKey() { return JSON.stringify(currentForm()); }
  function markDirty() { state.dirty = formKey() !== state.baseline; renderSave(); renderScope(); }
  function renderSave() {
    const btn = q('#fn3Save'); if (!btn) return;
    btn.disabled = state.saving || !state.dirty;
    btn.classList.toggle('primary', state.dirty);
    q('span', btn).textContent = state.saving ? 'Сохраняем…' : state.dirty ? 'Сохранить изменения' : 'Сохранено';
  }
  function renderScope() { qa('[data-scope-card]').forEach(n => n.classList.toggle('selected', q('input',n)?.checked)); }

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
    if (last) last.textContent = data?.last_run ? `${formatDate(data.last_run)}${data.result ? ` · ${data.result === 'success' ? 'Успешно' : data.result === 'available' ? 'Доступно обновление' : 'Ошибка'}` : ''}` : '—';
    if (last) last.classList.toggle('ok', data?.result === 'success' || data?.result === 'available');
    if (next) next.textContent = data?.enabled ? (data?.next_run ? formatDate(data.next_run) : 'после первого запуска') : 'выключено';
  }

  function applyData(data, status) {
    state.data = data; state.status = status;
    const auto = data.auto_vpn || {}, snap = data.automation || {};
    q('#fn3AutoEnabled').checked = !!auto.enabled;
    q('#fn3AutoLabel').textContent = auto.enabled ? 'Включено' : 'Выключено';
    state.countries = Array.isArray(auto.countries) ? auto.countries.slice() : [];
    const scope = ['current','region','allowlist'].includes(auto.country_scope) ? auto.country_scope : 'region';
    const scopeInput = q(`input[name="fn3Scope"][value="${scope}"]`); if (scopeInput) scopeInput.checked = true;
    renderScope();
    q('#fn3ControlTitle').textContent = auto.enabled ? 'Под контролем' : 'Автоматика выключена';
    q('#fn3ControlNext').textContent = auto.enabled ? `Следующая проверка: ${nextLabel(auto.next_health)}` : 'Автоматические проверки не выполняются';

    const profile = snap.current_profile || status.profile_label || 'VPN';
    q('#fn3Profile').textContent = profile; q('#fn3ProfileSmall').textContent = profile;
    q('#fn3Endpoint').textContent = snap.current_endpoint || status.endpoint || '—';
    q('#fn3Flag').textContent = flag(snap.country_code || status.country_code);
    q('#fn3LastQuality').textContent = snap.current_quality_checked_at ? formatDate(snap.current_quality_checked_at) : '—';
    q('#fn3Latency').textContent = snap.current_quality_known && snap.current_latency_ms ? `${snap.current_latency_ms} мс` : '—';
    q('#fn3Speed').textContent = snap.current_quality_known && snap.current_download_mbps ? `${Math.round(snap.current_download_mbps)} Мбит/с` : '—';
    q('#fn3Jitter').textContent = snap.current_quality_known && snap.current_jitter_ms ? `${snap.current_jitter_ms} мс` : '—';
    q('#fn3VPNState').textContent = snap.current_quality_known ? 'Стабильно' : 'Под наблюдением';
    q('#fn3HealthText').textContent = snap.current_quality_known ? 'Текущий VPN работает стабильно.' : 'FreeNet контролирует доступность текущего VPN.';
    renderJournal(data.events || []);
    applySchedule('subscription', data.subscription); applySchedule('geodata', data.geodata); applySchedule('freenet', data.freenet); applySchedule('backup', data.backup);
    state.baseline = formKey(); state.dirty = false; renderSave();
  }

  function renderJournal(events, target = '#fn3Journal') {
    const body = q(target); if (!body) return;
    const filtered = (events || []).slice(0, target === '#fn3Journal' ? 6 : 30);
    if (!filtered.length) { body.innerHTML = '<tr><td colspan="4" style="text-align:center;color:#8198b2;padding:16px">Событий пока нет.</td></tr>'; return; }
    body.innerHTML = filtered.map(e => {
      const [result, msg, tone] = humanResult(e.result, e.message);
      const kind = e.kind === 'auto_vpn' || e.kind === 'AUTO VPN' ? 'Автопроверка' : e.kind === 'subscription' ? 'Подписка' : e.kind === 'geodata' ? 'GeoData / GeoIP' : e.kind === 'freenet' ? 'FreeNet' : e.kind === 'backup' ? 'Резервная копия' : 'AUTO VPN';
      return `<tr><td>${formatDate(e.at)}</td><td>${kind}</td><td><span class="fn3-result ${tone==='ok'?'':'neutral'}"><i class="fn3-dot"></i>${result}</span></td><td>${escapeHTML(msg)}</td></tr>`;
    }).join('');
  }

  function escapeHTML(value) { return String(value || '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

  async function load() {
    try {
      const [data,status] = await Promise.all([fetchJSON('/api/settings-v3'), fetchJSON('/api/status')]);
      applyData(data,status);
    } catch (err) {
      console.error('Settings v3 load failed', err);
    }
  }

  async function save() {
    if (!state.dirty || state.saving) return;
    state.saving = true; renderSave();
    const form = currentForm();
    try {
      const data = await fetchJSON('/api/settings-v3', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'save',auto_vpn_enabled:form.enabled,country_scope:form.scope,countries:form.countries,subscription_enabled:form.subscription.enabled,subscription_interval:form.subscription.interval,geodata_enabled:form.geodata.enabled,geodata_interval:form.geodata.interval,freenet_enabled:form.freenet.enabled,freenet_interval:form.freenet.interval,backup_enabled:form.backup.enabled,backup_interval:form.backup.interval})});
      const status = await fetchJSON('/api/status'); applyData(data,status);
    } catch (err) { alert(`Не удалось сохранить настройки: ${err.message}`); }
    finally { state.saving = false; renderSave(); }
  }

  async function action(name) {
    try {
      const actionMap = {subscription:'subscription_check',geodata:'geodata_update',freenet:'freenet_check'};
      const action = actionMap[name] || name;
      if (action === 'backup_restore' && !confirm('Восстановить последнюю резервную копию настроек FreeNet? Перед восстановлением будет создан аварийный снимок текущих файлов.')) return;
      const data = await fetchJSON('/api/settings-v3/action', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action})});
      if (data.message) console.info(data.message);
      await load();
    } catch (err) { alert(`Операция не выполнена: ${err.message}`); }
  }

  async function checkAuto() {
    if (state.checking) return;
    state.checking = true;
    const btn = q('#fn3Check'); const started = Date.now();
    try {
      await fetchJSON('/api/automation', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'check'})});
      for (;;) {
        const s = await fetchJSON('/api/automation/check');
        const sec = Math.max(1, Math.floor((Date.now()-started)/1000));
        if (btn) q('span',btn).textContent = `Проверяем… ${sec} с`;
        if (!s.active) break;
        await new Promise(r => setTimeout(r, 1200));
      }
      await load();
    } catch (err) { alert(`Проверка не завершена: ${err.message}`); }
    finally { state.checking = false; if (btn) q('span',btn).textContent = 'Проверить сейчас'; }
  }

  function openCountries() {
    const pop = q('#fn3CountryPop'); if (!pop) return;
    const known = [['pl','Польша'],['de','Германия'],['nl','Нидерланды'],['fi','Финляндия'],['fr','Франция'],['se','Швеция'],['ch','Швейцария'],['at','Австрия'],['cz','Чехия'],['lt','Литва'],['lv','Латвия'],['ee','Эстония']];
    pop.innerHTML = `<div style="display:flex;justify-content:space-between;align-items:center"><strong>Выбранные страны</strong><button type="button" id="fn3PopClose" class="fn3-link">Закрыть</button></div><div class="fn3-country-list">${known.map(([c,n])=>`<label class="fn3-country-item"><input type="checkbox" value="${c}"${state.countries.includes(c)?' checked':''}>${flag(c)} ${n}</label>`).join('')}</div><div class="fn3-pop-actions"><button id="fn3CountriesApply" class="btn primary" type="button">Применить список</button></div>`;
    const selected = q('[data-scope-card="allowlist"]'); const r = selected?.getBoundingClientRect();
    pop.style.left = `${Math.min(window.innerWidth-450, Math.max(14, r?.left || 100))}px`; pop.style.top = `${Math.min(window.innerHeight-570, Math.max(14, (r?.bottom || 100)+6))}px`; pop.hidden = false;
    q('#fn3PopClose').onclick = () => pop.hidden = true;
    q('#fn3CountriesApply').onclick = () => { state.countries = qa('.fn3-country-item input:checked',pop).map(x=>x.value); pop.hidden = true; markDirty(); };
  }

  function bind() {
    q('#fn3Save').onclick = save; q('#fn3Check').onclick = checkAuto;
    q('#fn3AutoEnabled').onchange = () => { q('#fn3AutoLabel').textContent = q('#fn3AutoEnabled').checked ? 'Включено' : 'Выключено'; markDirty(); };
    qa('input[name="fn3Scope"]').forEach(i => i.onchange = () => { if (i.value === 'allowlist') openCountries(); markDirty(); });
    ['subscription','geodata','freenet','backup'].forEach(k => { q(`#fn3_${k}_enabled`).onchange = markDirty; q(`#fn3_${k}_interval`).onchange = markDirty; });
    qa('[data-v3-action]').forEach(b => b.onclick = () => action(b.dataset.v3Action));
    q('#fn3Copy').onclick = async () => { const text = q('#fn3Endpoint').textContent; try { await navigator.clipboard.writeText(text); } catch (_) {} };
    q('#fn3AllEvents').onclick = () => { location.hash='#journal'; if (typeof window.setPage === 'function') window.setPage('journal'); mountJournalPage(); };
    q('[data-scope-card="allowlist"]').ondblclick = openCountries;
  }

  function mountSettings() {
    const page = q('[data-page-view="settings"]'); if (!page) return;
    injectStyles(); hideProviderTopbar(); rewireNavigation();
    if (page.dataset.settingsV3 === '1') return;
    page.dataset.settingsV3 = '1'; page.className = 'page fn3-page'; page.innerHTML = pageMarkup();
    bind(); load();
  }

  function mountJournalPage() {
    const page = q('[data-page-view="journal"]'); if (!page) return;
    page.innerHTML = `<h1>Журнал</h1><p>История AUTO VPN, обновлений и резервного копирования FreeNet.</p><section class="fn3-card"><table class="fn3-table"><thead><tr><th>Время</th><th>Событие</th><th>Результат</th><th>Комментарий</th></tr></thead><tbody id="fn3JournalFull"></tbody></table></section>`;
    renderJournal(state.data?.events || [], '#fn3JournalFull');
  }

  function route() {
    hideProviderTopbar(); rewireNavigation();
    const page = location.hash.slice(1);
    if (page === 'settings' || (!page && q('[data-page-view="settings"].active'))) mountSettings();
    if (page === 'journal') mountJournalPage();
  }

  document.addEventListener('DOMContentLoaded', route, {once:true});
  window.addEventListener('hashchange', () => setTimeout(route, 0));
  setTimeout(route, 0);
})();
