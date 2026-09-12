(() => {
  const qs = (s, root = document) => root.querySelector(s);
  const esc = (v) => String(v == null ? '' : v).replace(/[&<>"']/g, (c) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  let automationState = null;
  let automationBusy = false;

  function mountAutomationStyles() {
    if (qs('#freenetAutomationStyles')) return;
    const style = document.createElement('style');
    style.id = 'freenetAutomationStyles';
    style.textContent = `
      .automation-page{max-width:1180px;margin:0 auto}
      .automation-grid{display:grid;grid-template-columns:minmax(0,1.15fr) minmax(360px,.85fr);gap:14px;align-items:start}
      .automation-stack{display:grid;gap:14px}
      .automation-card{box-shadow:none}
      .automation-card .card-head{margin-bottom:13px}
      .automation-title{display:flex;align-items:center;gap:12px;min-width:0}
      .automation-icon{width:42px;height:42px;display:grid;place-items:center;flex:0 0 auto;border-radius:13px;background:linear-gradient(180deg,#194e99,#123a73);color:var(--accent2);font-size:22px}
      .automation-title h2{font-size:20px;margin:0;letter-spacing:-.025em}
      .automation-title p{margin:3px 0 0;color:var(--muted);font-size:12.5px;line-height:1.45}
      .automation-switch{display:inline-flex;align-items:center;gap:9px;color:var(--text);font-size:12.5px;font-weight:700;cursor:pointer;user-select:none}
      .automation-switch input{position:absolute;opacity:0;pointer-events:none}
      .automation-switch-track{position:relative;width:48px;height:27px;border-radius:999px;background:#34445a;border:1px solid var(--line2);transition:.15s ease;box-shadow:inset 0 1px 4px rgba(0,0,0,.25)}
      .automation-switch-track::after{content:'';position:absolute;top:3px;left:3px;width:19px;height:19px;border-radius:50%;background:#d8e3f2;transition:.15s ease;box-shadow:0 2px 8px rgba(0,0,0,.35)}
      .automation-switch input:checked + .automation-switch-track{background:linear-gradient(180deg,#29d68c,#19a96d);border-color:#36e59a}
      .automation-switch input:checked + .automation-switch-track::after{transform:translateX(21px);background:#fff}
      .automation-switch input:disabled + .automation-switch-track{opacity:.55;cursor:not-allowed}
      .automation-info{display:flex;gap:10px;align-items:flex-start;padding:13px 14px;border-radius:12px;border:1px solid #2f6ec9;background:linear-gradient(180deg,rgba(24,77,151,.38),rgba(12,43,87,.35));color:#b9d5ff;font-size:12.5px;line-height:1.5}
      .automation-info b{color:#d9e9ff}
      .automation-main-grid{display:grid;grid-template-columns:minmax(0,.78fr) minmax(0,1.22fr);gap:12px;margin-top:13px}
      .automation-control-block{display:grid;gap:10px}
      .automation-label{color:var(--muted);font-size:11px;font-weight:700;margin-bottom:6px}
      .automation-field select{width:100%;border:1px solid #2b405e;background:#0b1726;color:var(--text);border-radius:10px;padding:10px 12px;outline:none}
      .automation-options{display:grid;gap:10px;margin-top:4px}
      .automation-option{display:flex;align-items:center;gap:9px;color:#c8d4e6;font-size:12px;line-height:1.4}
      .automation-option .automation-switch-track{width:39px;height:23px}
      .automation-option .automation-switch-track::after{width:15px;height:15px}
      .automation-option input:checked + .automation-switch-track::after{transform:translateX(16px)}
      .automation-metrics{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:9px}
      .automation-metric{min-height:64px;padding:11px 12px;border:1px solid #28405e;border-radius:11px;background:#0b1726}
      .automation-metric.wide{grid-column:1/-1}
      .automation-metric-label{color:var(--muted);font-size:10.5px;margin-bottom:5px}
      .automation-metric-value{font-size:13px;font-weight:750;line-height:1.35;overflow-wrap:anywhere}
      .automation-metric-value.ok{color:var(--ok)}
      .automation-metric-value.accent{color:var(--accent2)}
      .automation-actions{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:14px}
      .automation-actions .btn{justify-content:center;text-align:center}
      .automation-profile{padding:14px;border:1px solid #28405e;border-radius:12px;background:#0b1726}
      .automation-profile-head{display:flex;align-items:center;justify-content:space-between;gap:12px}
      .automation-profile-name{display:flex;align-items:center;gap:10px;font-size:18px;font-weight:800;letter-spacing:-.02em}
      .automation-profile-body{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:13px}
      .automation-profile-row{padding:10px 11px;border-left:1px solid #2d4666;color:var(--muted);font-size:11px}
      .automation-profile-row strong{display:block;margin-top:3px;color:var(--text);font-size:12.5px}
      .automation-status-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:9px;margin-top:10px}
      .automation-status-box{padding:11px;border:1px solid #28405e;border-radius:11px;background:#0a1523;color:var(--muted);font-size:10.5px}
      .automation-status-box strong{display:block;margin-top:4px;color:var(--text);font-size:12px;line-height:1.35}
      .automation-status-box strong.ok{color:var(--ok)}
      .automation-log{overflow:hidden;border:1px solid #28405e;border-radius:12px;background:#091522}
      .automation-log table{width:100%;border-collapse:collapse;font-size:11px}
      .automation-log th{padding:9px 10px;text-align:left;color:var(--muted);font-weight:650;background:#102039;border-bottom:1px solid #28405e}
      .automation-log td{padding:9px 10px;border-bottom:1px solid #1d3048;color:#cbd6e5;vertical-align:top}
      .automation-log tr:last-child td{border-bottom:0}
      .automation-result{display:inline-flex;align-items:center;gap:6px;white-space:nowrap}
      .automation-result::before{content:'';width:7px;height:7px;border-radius:50%;background:var(--muted2)}
      .automation-result.ok{color:var(--ok)}.automation-result.ok::before{background:var(--ok)}
      .automation-result.warn{color:var(--warn)}.automation-result.warn::before{background:var(--warn)}
      .automation-extra-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px}
      .automation-extra{padding:12px;border:1px solid #28405e;border-radius:12px;background:#0b1726;min-height:145px;display:flex;flex-direction:column}
      .automation-extra-head{display:flex;align-items:flex-start;justify-content:space-between;gap:8px}
      .automation-extra h3{margin:0;font-size:13px}.automation-extra p{margin:4px 0 0;color:var(--muted);font-size:11px;line-height:1.45}
      .automation-extra-meta{display:flex;gap:7px;align-items:center;margin-top:auto;padding-top:12px;color:#c6d3e4;font-size:11px;font-weight:700}
      .automation-extra-note{margin-top:9px;padding:9px 10px;border:1px solid #263d5a;border-radius:9px;background:#091522;color:var(--muted);font-size:10.5px;line-height:1.45}
      .automation-safety{display:grid;gap:10px}
      .automation-safety-item{display:flex;gap:9px;align-items:flex-start;color:#bfcde0;font-size:11.5px;line-height:1.45}
      .automation-check{width:18px;height:18px;display:grid;place-items:center;flex:0 0 auto;border-radius:50%;background:rgba(73,218,146,.14);color:var(--ok);font-weight:900}
      .automation-empty{padding:16px;color:var(--muted);text-align:center}
      .automation-feedback{margin-top:10px}
      @media(max-width:1050px){.automation-grid{grid-template-columns:1fr}.automation-main-grid{grid-template-columns:1fr}.automation-extra-grid{grid-template-columns:1fr 1fr}.automation-status-grid{grid-template-columns:1fr 1fr 1fr}}
      @media(max-width:720px){.automation-extra-grid,.automation-status-grid,.automation-actions,.automation-profile-body{grid-template-columns:1fr}.automation-metrics{grid-template-columns:1fr}.automation-metric.wide{grid-column:auto}.automation-profile-head{align-items:flex-start;flex-direction:column}.automation-switch{font-size:11.5px}}
    `;
    document.head.appendChild(style);
  }

  function formatDate(value) {
    if (!value) return '—';
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return esc(value);
    return d.toLocaleString('ru-RU', {day:'2-digit',month:'2-digit',hour:'2-digit',minute:'2-digit'});
  }

  function intervalLabel(value) {
    return ({'30m':'30 минут','1h':'1 час','3h':'3 часа','6h':'6 часов','manual':'Вручную'})[value] || 'Вручную';
  }

  function resultClass(result) {
    const v = String(result || '').toLowerCase();
    if (/(success|updated|unchanged|no_new|same|ok)/.test(v)) return 'ok';
    if (/(ambiguous|uncertain|worse|failed|error|rollback)/.test(v)) return 'warn';
    return '';
  }

  function resultLabel(result) {
    const labels = {
      updated:'Успешно', unchanged:'Без изменений', no_new:'Без изменений', same:'Без изменений',
      ambiguous:'Неоднозначно', uncertain:'Не применено', worse:'Кандидат отклонён', failed:'Ошибка',
      error:'Ошибка', rollback:'Откат выполнен', success:'Успешно'
    };
    return labels[String(result || '').toLowerCase()] || (result || 'Нет данных');
  }

  function automationMarkup(data) {
    const s = data.settings || {};
    const enabled = !!s.enabled;
    const interval = s.interval || 'manual';
    const profile = data.current_profile || 'Текущий профиль не определён';
    const endpoint = data.current_endpoint || '—';
    const cc = String(data.country_code || '').toLowerCase();
    const events = Array.isArray(data.events) ? data.events : [];
    const eventRows = events.length ? events.map((e) => `
      <tr><td>${esc(formatDate(e.at))}</td><td>${esc(e.kind || 'AUTO VPN')}</td><td><span class="automation-result ${resultClass(e.result)}">${esc(resultLabel(e.result))}</span></td><td>${esc(e.message || '—')}</td></tr>`).join('') : '<tr><td colspan="4" class="automation-empty">Автоматических операций пока не было.</td></tr>';
    return `
      <div class="page-head"><div><h1>Автоматизация</h1><p>Управление автоматическим обслуживанием VPN, обновлением endpoint и регулярными обновлениями сервисов.</p></div></div>
      <div class="automation-grid">
        <div class="automation-stack">
          <section class="card automation-card">
            <div class="card-head">
              <div class="automation-title"><span class="automation-icon">↻</span><div><h2>AUTO VPN v1</h2><p>Автоматический контроль и обновление endpoint</p></div></div>
              <label class="automation-switch"><input id="autoVpnEnabled" type="checkbox" ${enabled ? 'checked' : ''}><span class="automation-switch-track"></span><span>Автообновление VPN</span></label>
            </div>
            <div class="automation-info"><span>ⓘ</span><div><b>Смена страны отключена.</b> FreeNet работает только с текущим logical-профилем и обновляет endpoint в этой же локации.</div></div>
            <div class="automation-main-grid">
              <div class="automation-control-block">
                <div class="automation-field"><div class="automation-label">Интервал проверки</div><select id="autoVpnInterval">
                  <option value="30m" ${interval==='30m'?'selected':''}>30 минут</option><option value="1h" ${interval==='1h'?'selected':''}>1 час</option><option value="3h" ${interval==='3h'?'selected':''}>3 часа</option><option value="6h" ${interval==='6h'?'selected':''}>6 часов</option><option value="manual" ${interval==='manual'?'selected':''}>Вручную</option>
                </select></div>
                <div class="automation-options">
                  <label class="automation-option automation-switch"><input type="checkbox" checked disabled><span class="automation-switch-track"></span><span>Проверять только текущий профиль</span></label>
                  <label class="automation-option automation-switch"><input type="checkbox" checked disabled><span class="automation-switch-track"></span><span>Автоматически обновлять endpoint</span></label>
                  <label class="automation-option automation-switch"><input type="checkbox" checked disabled><span class="automation-switch-track"></span><span>При неоднозначности ничего не менять</span></label>
                </div>
              </div>
              <div class="automation-metrics">
                <div class="automation-metric"><div class="automation-metric-label">Статус</div><div class="automation-metric-value ${enabled?'ok':''}">${enabled?'Активно':'Выключено'}</div></div>
                <div class="automation-metric"><div class="automation-metric-label">Текущий профиль</div><div class="automation-metric-value">${esc(profile)}</div></div>
                <div class="automation-metric"><div class="automation-metric-label">Последний запуск</div><div class="automation-metric-value">${esc(formatDate(data.last_run))}</div></div>
                <div class="automation-metric"><div class="automation-metric-label">Следующий запуск</div><div class="automation-metric-value accent">${enabled ? esc(formatDate(data.next_run)) : '—'}</div></div>
                <div class="automation-metric wide"><div class="automation-metric-label">Последнее решение</div><div class="automation-metric-value ${resultClass(data.last_result)}">${esc(data.last_reason || 'Проверки ещё не выполнялись')}</div></div>
              </div>
            </div>
            <div class="automation-actions"><button id="autoVpnCheckNow" class="btn primary" type="button">▶&nbsp;&nbsp;Проверить сейчас</button><button id="autoVpnSave" class="btn secondary" type="button">⚙&nbsp;&nbsp;Сохранить настройки</button></div>
            <div id="autoVpnFeedback" class="notice automation-feedback"></div>
          </section>

          <section class="card automation-card">
            <div class="card-head"><div class="automation-title"><span class="automation-icon">▣</span><div><h2>Дополнительные автоматизации</h2><p>Фоновые задачи и обновление данных</p></div></div></div>
            <div class="automation-extra-grid">
              <div class="automation-extra"><div class="automation-extra-head"><div><h3>Обновление подписки</h3><p>Автоматическая проверка актуальности списка VPN-профилей</p></div><label class="automation-switch"><input type="checkbox" ${data.subscription_auto?'checked':''} disabled><span class="automation-switch-track"></span></label></div><div class="automation-extra-meta">◷&nbsp; Управление будет подключено отдельным безопасным циклом</div><div class="automation-extra-note">Секретные данные подписки никогда не отображаются в интерфейсе или журнале.</div></div>
              <div class="automation-extra"><div class="automation-extra-head"><div><h3>GeoData / GeoIP</h3><p>Обновление geosite.dat, geoip.dat и rule-set данных</p></div><label class="automation-switch"><input type="checkbox" ${data.geodata_auto?'checked':''} disabled><span class="automation-switch-track"></span></label></div><div class="automation-extra-meta">◷&nbsp; ${esc(data.geodata_schedule || '1 раз в сутки')}</div><div class="automation-extra-note">Показывается фактическое состояние существующей XKeen GeoData automation. Расширенное управление — после memory-safe GeoData gate.</div></div>
              <div class="automation-extra"><div class="automation-extra-head"><div><h3>Обновление FreeNet</h3><p>Проверка наличия и установка обновлений системы</p></div><label class="automation-switch"><input type="checkbox" ${data.freenet_auto?'checked':''} disabled><span class="automation-switch-track"></span></label></div><div class="automation-extra-meta">◷&nbsp; Только проверка / вручную</div><div class="automation-extra-note">Автоматическая установка FreeNet без явного подтверждения отключена.</div></div>
            </div>
          </section>
        </div>

        <div class="automation-stack">
          <section class="card automation-card">
            <div class="card-head"><h2>Текущий профиль под наблюдением</h2><span class="status-badge ${enabled?'ok':'warn'}">${enabled?'● Под контролем':'○ Автоматика выключена'}</span></div>
            <div class="automation-profile">
              <div class="automation-profile-head"><div class="automation-profile-name">${cc ? `<span class="flag-icon flag-${esc(cc)}"></span>` : ''}<span>${esc(profile)}</span></div></div>
              <div class="automation-profile-body"><div class="automation-profile-row">Профиль<strong>${esc(profile)}</strong></div><div class="automation-profile-row">Endpoint<strong>${esc(endpoint)}</strong></div></div>
            </div>
            <div class="automation-status-grid"><div class="automation-status-box">Последняя проверка<strong>${esc(formatDate(data.last_run))}</strong></div><div class="automation-status-box">Результат<strong class="${resultClass(data.last_result)}">${esc(resultLabel(data.last_result))}</strong></div><div class="automation-status-box">Rollback ready<strong class="${data.rollback_ready?'ok':''}">${data.rollback_ready?'Да (snapshot подтверждён)':'По факту операции'}</strong></div></div>
          </section>

          <section class="card automation-card">
            <div class="card-head"><h2>Журнал автоматических операций</h2><button class="mini-btn" type="button" id="autoVpnReload">Все события</button></div>
            <div class="automation-log"><table><thead><tr><th>Время</th><th>Тип автоматизации</th><th>Результат</th><th>Комментарий</th></tr></thead><tbody>${eventRows}</tbody></table></div>
          </section>

          <section class="card automation-card">
            <div class="card-head"><h2>Правила безопасности</h2></div>
            <div class="automation-safety">
              <div class="automation-safety-item"><span class="automation-check">✓</span><span>Нет бесконечных повторов и blind retry при ошибках.</span></div>
              <div class="automation-safety-item"><span class="automation-check">✓</span><span>Перед применением сохраняется snapshot текущего VPN/profile state.</span></div>
              <div class="automation-safety-item"><span class="automation-check">✓</span><span>После применения выполняется post-check; при неудаче — rollback.</span></div>
              <div class="automation-safety-item"><span class="automation-check">✓</span><span>Subscription URL, UUID, Reality keys/shortId и пароли не попадают в UI или журнал.</span></div>
              <div class="automation-safety-item"><span class="automation-check">✓</span><span>Неоднозначность logical profile = STOP без mutation. Страна автоматически не меняется.</span></div>
            </div>
          </section>
        </div>
      </div>`;
  }

  function setFeedback(text, type='') {
    const node = qs('#autoVpnFeedback');
    if (!node) return;
    node.textContent = text || '';
    node.className = 'notice automation-feedback' + (text ? ' show' : '') + (type ? ' ' + type : '');
  }

  function setAutomationBusy(busy) {
    automationBusy = busy;
    ['#autoVpnCheckNow','#autoVpnSave','#autoVpnEnabled','#autoVpnInterval'].forEach((id) => { const n=qs(id); if(n) n.disabled=busy; });
  }

  async function api(path, options) {
    const res = await fetch(path, Object.assign({credentials:'same-origin'}, options || {}));
    let body = {};
    try { body = await res.json(); } catch (_) {}
    if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
    return body;
  }

  async function loadAutomation() {
    if (automationBusy) return;
    try {
      const data = await api('/api/automation');
      automationState = data;
      renderAutomation(data);
    } catch (err) {
      const page = qs('[data-page-view="automation"]');
      if (page) page.innerHTML = `<div class="page-head"><div><h1>Автоматизация</h1><p>Управление автоматическими задачами FreeNet.</p></div></div><div class="card"><div class="notice show bad">${esc(err.message || err)}</div></div>`;
    }
  }

  function renderAutomation(data) {
    mountAutomationStyles();
    const page = qs('[data-page-view="automation"]');
    if (!page) return;
    page.classList.add('automation-page');
    page.innerHTML = automationMarkup(data);
    bindAutomation();
  }

  function bindAutomation() {
    qs('#autoVpnSave')?.addEventListener('click', async () => {
      if (automationBusy) return;
      setAutomationBusy(true); setFeedback('Сохраняем настройки и перестраиваем только FreeNet-managed schedule…');
      try {
        const enabled = !!qs('#autoVpnEnabled')?.checked;
        const interval = qs('#autoVpnInterval')?.value || 'manual';
        const data = await api('/api/automation', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'save',enabled,interval})});
        automationState = data; renderAutomation(data); setFeedback('Настройки сохранены.', 'ok');
      } catch (err) { setFeedback(err.message || String(err), 'bad'); }
      finally { setAutomationBusy(false); }
    });
    qs('#autoVpnCheckNow')?.addEventListener('click', async () => {
      if (automationBusy) return;
      setAutomationBusy(true); setFeedback('Проверяем только текущий logical-профиль. Страна не меняется…');
      try {
        const data = await api('/api/automation', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'check'})});
        automationState = data; renderAutomation(data); setFeedback(data.last_reason || 'Проверка завершена.', 'ok');
      } catch (err) { await loadAutomation(); setFeedback(err.message || String(err), 'bad'); }
      finally { setAutomationBusy(false); }
    });
    qs('#autoVpnReload')?.addEventListener('click', loadAutomation);
    qs('#autoVpnEnabled')?.addEventListener('change', () => {
      if (!qs('#autoVpnEnabled')?.checked && qs('#autoVpnInterval')) qs('#autoVpnInterval').value='manual';
      if (qs('#autoVpnEnabled')?.checked && qs('#autoVpnInterval')?.value==='manual') qs('#autoVpnInterval').value='1h';
    });
  }

  function bootAutomation() {
    mountAutomationStyles();
    const nav = qs('.nav-btn[data-page="automation"]');
    nav?.addEventListener('click', () => setTimeout(loadAutomation, 0));
    if (location.hash === '#automation') loadAutomation();
    window.addEventListener('hashchange', () => { if (location.hash === '#automation') loadAutomation(); });
    setTimeout(() => {
      const page = qs('[data-page-view="automation"]');
      if (page && !page.dataset.automationMounted) { page.dataset.automationMounted='1'; loadAutomation(); }
    }, 500);
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', bootAutomation, {once:true});
  else bootAutomation();
})();
