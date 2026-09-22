// Browser acceptance for the shipped Settings v3 presentation layer.
// Uses documentation-only addresses and fixtures; no router or live VPN is contacted.
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');

const root = path.resolve(__dirname, '..');
const web = path.join(root, 'freenet-ui', 'web');
const artifacts = process.env.FREENET_UI_ARTIFACTS || path.join(root, 'test-artifacts');
const scripts = [
  'self-update.js', 'vpn-ux-fix.js', 'operation-coordinator.js', 'accepted-ux.js',
  'runtime-acceptance.js', 'automation.js', 'automation-async.js', 'settings-v3.js'
];
const calls = [];
let versionApplyPosts = 0;
let installedFreeNetVersion = 'v0.3.43';
let activeFreeNetTarget = '';
let backupCreatePosts = 0;
let backupRestorePosts = 0;

const settings = {
  success: true,
  auto_vpn: {enabled:true,mode:'best',endpoint_interval:'1h',country_scope:'region',countries:['pl','fr'],last_health:'2026-09-12T11:35:00Z',next_health:'2026-09-12T11:40:00Z'},
  automation: {
    current_profile:'PL Варшава, Польша, Extra',current_endpoint:'192.0.2.42:443',country_code:'pl',
    current_quality_known:true,current_quality_checked_at:'2026-09-12T11:35:00Z',current_latency_ms:171,current_download_mbps:168,current_jitter_ms:7,
    events:[
      {at:'2026-09-12T11:35:00Z',kind:'auto_vpn',result:'success',message:'Текущий VPN работает нормально, смена не требуется.'},
      {at:'2026-09-12T10:35:00Z',kind:'auto_vpn',result:'same',message:'Для текущего VPN нет нового адреса подключения.'}
    ]
  },
  subscription:{enabled:true,interval:'6h',last_run:'2026-09-12T10:30:00Z',next_run:'2026-09-12T16:30:00Z',result:'success'},
  geodata:{enabled:true,interval:'3h',last_run:'2026-09-12T10:05:00Z',next_run:'2026-09-12T13:05:00Z',result:'success'},
  freenet:{enabled:true,interval:'12h',last_run:'2026-09-12T00:15:00Z',next_run:'2026-09-12T12:15:00Z',result:'success'},
  backup:{enabled:true,interval:'24h',last_run:'2026-09-12T02:30:00Z',next_run:'2026-09-13T02:30:00Z',result:'success'},
  backup_info:{
    root:'/opt/backups/freenet-settings',
    latest:'backup-20260912-023000.000000000',
    latest_path:'/opt/backups/freenet-settings/backup-20260912-023000.000000000',
    tracked:4
  },
  events:[
    {at:'2026-09-12T11:35:00Z',kind:'auto_vpn',result:'success',message:'Текущий VPN работает нормально, смена не требуется.'},
    {at:'2026-09-12T10:35:00Z',kind:'auto_vpn',result:'same',message:'Для текущего VPN нет нового адреса подключения.'},
    {at:'2026-09-12T09:35:00Z',kind:'geodata',result:'updated',message:'GeoData / GeoIP обновлены.'},
    {at:'2026-09-12T08:35:00Z',kind:'backup',result:'success',message:'Резервная копия FreeNet создана.'},
    {at:'2026-09-12T07:35:00Z',kind:'freenet',result:'failed',message:'Проверка обновления FreeNet завершилась ошибкой.'},
    {at:'2026-09-12T06:35:00Z',kind:'subscription',result:'updated',message:'Список Extra-профилей обновлён.'}
  ]
};

const countryCatalog = {
  success:true,
  fresh:true,
  selected:['pl','fr'],
  countries:[
    {code:'gb',name:'Великобритания',available:true},
    {code:'nl',name:'Нидерланды',available:true},
    {code:'pl',name:'Польша',available:true},
    {code:'us',name:'США',available:true},
    {code:'fr',name:'Ранее выбранная страна',available:false}
  ]
};

const status = {
  version:'0.3.43',country:'Польша',city:'Варшава',country_code:'pl',profile_label:'PL Варшава, Польша, Extra',endpoint:'192.0.2.42:443',
  xray_online:true,xkeen_ui_online:true,dns_out_present:true,dns_mode:'xkeen',isp:'vladlink',isp_label:'Владлинк',recommended_dns_mode:'xkeen',
  setup_complete:true,install_scenario:'existing_stack',subscription_configured:true,busy:false,updater_busy:false,last_action:{success:true}
};

function json(res, body, code = 200) {
  res.writeHead(code, {'Content-Type':'application/json; charset=utf-8'});
  res.end(JSON.stringify(body));
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://localhost');
  calls.push(`${req.method} ${url.pathname}${url.search}`);
  if (url.pathname === '/' || url.pathname === '/settings' || url.pathname === '/routing') {
    const html = fs.readFileSync(path.join(web, 'index.html'), 'utf8')
      .replace('</body>', scripts.map(name => `<script src="/${name}"></script>`).join('') + '</body>');
    res.writeHead(200, {'Content-Type':'text/html; charset=utf-8'}); res.end(html); return;
  }
  if (scripts.some(name => url.pathname === `/${name}`)) {
    res.writeHead(200, {'Content-Type':'application/javascript; charset=utf-8'}); res.end(fs.readFileSync(path.join(web, url.pathname.slice(1)), 'utf8')); return;
  }
  if (url.pathname === '/api/auth/status') return json(res, {configured:true,authenticated:true});
  if (url.pathname === '/api/status') return json(res, status);
  if (url.pathname === '/api/settings-v3') {
    if (req.method !== 'POST') return json(res, settings);
    let raw = '';
    req.on('data', chunk => { raw += chunk; });
    req.on('end', () => {
      const body = JSON.parse(raw || '{}');
      settings.auto_vpn.enabled = !!body.auto_vpn_enabled;
      settings.auto_vpn.mode = body.auto_vpn_mode === 'endpoint' ? 'endpoint' : 'best';
      settings.auto_vpn.endpoint_interval = body.auto_vpn_endpoint_interval || '1h';
      settings.auto_vpn.country_scope = body.country_scope || settings.auto_vpn.country_scope;
      settings.auto_vpn.countries = Array.isArray(body.countries) ? body.countries : settings.auto_vpn.countries;
      return json(res, settings);
    });
    return;
  }
  if (url.pathname === '/api/settings-v3/action' && req.method === 'POST') {
    let raw = '';
    req.on('data', chunk => { raw += chunk; });
    req.on('end', () => {
      const body = JSON.parse(raw || '{}');
      if (body.action === 'backup_create') {
        backupCreatePosts += 1;
        settings.backup_info = {
          root:'/opt/backups/freenet-settings',
          latest:'backup-20260921-174500.123456789',
          latest_path:'/opt/backups/freenet-settings/backup-20260921-174500.123456789',
          tracked:4
        };
        settings.backup.last_run = '2026-09-21T07:45:00Z';
        settings.backup.result = 'success';
        return json(res, {success:true,message:'Снимок настроек FreeNet создан.',backup_info:settings.backup_info});
      }
      if (body.action === 'backup_restore') {
        backupRestorePosts += 1;
        return json(res, {success:true,message:'Последний снимок восстановлен и проверен.',backup_info:settings.backup_info});
      }
      return json(res, {success:true,message:'Операция завершена.'});
    });
    return;
  }
  if (url.pathname === '/api/settings-v3/countries') return json(res, countryCatalog);
  if (url.pathname === '/api/subscription') return json(res, {success:true,configured:true});
  if (url.pathname === '/api/geodata/files') return json(res, {success:true,files:[],search_enabled:false});
  if (url.pathname === '/api/network-profile/plan') return json(res, {success:true,supported:true,active:true,extra_profiles:[]});
  if (url.pathname === '/api/automation') return json(res, {success:true,settings:{enabled:true},events:[]});
  if (url.pathname === '/api/automation/check') return json(res, {success:true,active:false});
  if (url.pathname === '/api/operation/state') return json(res, {success:true,active:false});
  if (url.pathname === '/versionz') {
    res.writeHead(200, {'Content-Type':'text/plain; charset=utf-8'}); res.end(installedFreeNetVersion + '\n'); return;
  }
  if (url.pathname === '/api/system/update/releases') return json(res, {
    success:true,current_version:installedFreeNetVersion,latest_version:'v0.3.98',
    releases:[
      {version:'v0.3.98',published_at:'2026-09-19T00:00:00Z',current:false,latest:true},
      {version:'v0.3.43',published_at:'2026-08-19T00:00:00Z',current:installedFreeNetVersion==='v0.3.43',latest:false},
      {version:'v0.3.42',published_at:'2026-08-18T00:00:00Z',current:installedFreeNetVersion==='v0.3.42',latest:false},
      {version:'v0.3.41',published_at:'2026-08-17T00:00:00Z',current:false,latest:false},
      {version:'v0.3.40',published_at:'2026-08-16T00:00:00Z',current:false,latest:false},
      {version:'v0.3.39',published_at:'2026-08-15T00:00:00Z',current:false,latest:false},
      {version:'v0.3.38',published_at:'2026-08-14T00:00:00Z',current:false,latest:false}
    ]
  });
  if (url.pathname === '/api/system/update/plan' && req.method === 'GET') {
    const target = url.searchParams.get('target') || 'v0.3.98';
    const direction = target === installedFreeNetVersion ? 'same' : target === 'v0.3.42' ? 'downgrade' : 'upgrade';
    return json(res, {
      success:true,ready:true,current_version:installedFreeNetVersion,latest_version:'v0.3.98',
      target_tag:target,update_available:direction!=='same',direction,manifest_verified:true,
      expected_no_delta:'subscription secret; Xray credentials/config; ISP/DNS/routing state'
    });
  }
  if (url.pathname === '/api/system/update/apply' && req.method === 'POST') {
    let raw = '';
    req.on('data', chunk => { raw += chunk; });
    req.on('end', () => {
      const body = JSON.parse(raw || '{}');
      versionApplyPosts += 1;
      activeFreeNetTarget = body.target_tag || '';
      installedFreeNetVersion = activeFreeNetTarget || installedFreeNetVersion;
      return json(res, {success:true,action:'self-update',operation_id:activeFreeNetTarget,message:'Изменение версии FreeNet запущено.'}, 202);
    });
    return;
  }
  if (url.pathname === '/api/system/update/state') return json(res, activeFreeNetTarget ? {
    state:'SUCCESS',from_version:'v0.3.43',target_version:activeFreeNetTarget,message:'Выбранная версия FreeNet установлена и проверена',rollback_state:'NOT_NEEDED',update_lock_held:false,update_lock_stale:false
  } : {state:'IDLE',update_lock_held:false,update_lock_stale:false});
  return json(res, {success:true,available:false,configured:true,active:false});
});

(async () => {
  const automationSource = fs.readFileSync(path.join(web, 'automation.js'), 'utf8');
  const asyncSource = fs.readFileSync(path.join(web, 'automation-async.js'), 'utf8');
  const settingsSource = fs.readFileSync(path.join(web, 'settings-v3.js'), 'utf8');
  assert.doesNotMatch(automationSource, /dataset\.pageView\s*=\s*['"]settings['"]/, 'legacy Automation must never be renamed to Settings');
  assert.doesNotMatch(automationSource + asyncSource, /dispatchEvent\(new Event\(['"]hashchange['"]\)\)/, 'Settings lifecycle must not use synthetic hashchange');
  assert.doesNotMatch(asyncSource, /body\.fn-settings-accepted \.sidebar\{|body\.fn-settings-accepted \.nav-btn\{/, 'Settings presentation must not resize the shared shell');
  assert.doesNotMatch(settingsSource, /\[\['pl','Польша'\]/, 'Settings country picker must not use a hardcoded country list');
  assert.doesNotMatch(settingsSource, /<div class="fn3-right">|id="fn3Profile"|id="fn3Endpoint"|id="fn3VPNState"/, 'Settings must not ship a duplicate Current VPN card');

  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch({headless:true});
  try {
    const page = await browser.newPage({viewport:{width:1600,height:1000}});
    const errors = [];
    const consoleErrors = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('console', msg => { if (msg.type() === 'error') consoleErrors.push(msg.text()); });
    page.on('requestfailed', req => consoleErrors.push(`requestfailed ${req.method()} ${req.url()} ${req.failure()?.errorText || ''}`));
    const base = `http://127.0.0.1:${server.address().port}`;

    await page.goto(`${base}/#overview`);
    await page.waitForFunction(() => document.querySelector('[data-page-view="settings"]')?.dataset.settingsV3 === '1');
    await page.waitForFunction(() => document.querySelector('[data-page-view="overview"]')?.classList.contains('active'));
    await page.waitForTimeout(1000);

    const cold = await page.evaluate(() => ({
      settingsCount: document.querySelectorAll('[data-page-view="settings"]').length,
      settingsActive: document.querySelector('[data-page-view="settings"]')?.classList.contains('active'),
      settingsV3: document.querySelector('[data-page-view="settings"]')?.dataset.settingsV3 || '',
      automationCount: document.querySelectorAll('[data-page-view="automation"]').length,
      automationPage: document.querySelector('[data-page-view="automation"]')?.dataset.pageView || '',
      settingsNavCount: document.querySelectorAll('.sidebar .nav-btn[data-page="settings"]').length,
      automationNavCount: document.querySelectorAll('.sidebar .nav-btn[data-page="automation"]').length,
      activePage: document.querySelector('[data-page-view].active')?.dataset.pageView || '',
      sidebarWidth: Math.round(document.querySelector('.sidebar')?.getBoundingClientRect().width || 0),
      navFontSize: getComputedStyle(document.querySelector('.nav-btn[data-page="overview"]')).fontSize,
      navHeight: Math.round(document.querySelector('.nav-btn[data-page="overview"]')?.getBoundingClientRect().height || 0)
    }));
    assert.equal(cold.settingsCount, 1, `Settings must exist before first navigation: ${JSON.stringify(cold)}`);
    assert.equal(cold.settingsActive, false, `Settings must stay hidden on Overview: ${JSON.stringify(cold)}`);
    assert.equal(cold.settingsV3, '1', `Settings v3 must own the canonical page: ${JSON.stringify(cold)}`);
    assert.equal(cold.automationCount, 1, `legacy Automation must stay separate: ${JSON.stringify(cold)}`);
    assert.equal(cold.automationPage, 'automation', `legacy Automation was renamed: ${JSON.stringify(cold)}`);
    assert.equal(cold.settingsNavCount, 1, `canonical Settings nav missing: ${JSON.stringify(cold)}`);
    assert.equal(cold.automationNavCount, 0, `legacy Automation route must not remain user-facing: ${JSON.stringify(cold)}`);
    assert.equal(cold.activePage, 'overview', `cold route changed unexpectedly: ${JSON.stringify(cold)}`);

    await page.locator('.nav-btn[data-page="settings"]').click();
    await page.waitForFunction(() => document.querySelector('[data-page-view="settings"]')?.classList.contains('active'));
    await page.waitForSelector('#fn3AutoEnabled', {state:'visible'});

    const runtime = await page.evaluate(() => ({
      saveDisabled: document.querySelector('#fn3Save')?.disabled,
      saveText: document.querySelector('#fn3Save')?.textContent || '',
      maintenanceSaveDisabled: document.querySelector('#fn3MaintenanceSave')?.disabled,
      maintenanceSaveText: document.querySelector('#fn3MaintenanceSave')?.textContent || '',
      maintenanceSaveVisible: !!document.querySelector('#fn3MaintenanceSave') && document.querySelector('#fn3MaintenanceSave').getClientRects().length > 0,
      autoChecked: document.querySelector('#fn3AutoEnabled')?.checked,
      mode: document.querySelector('input[name="fn3Mode"]:checked')?.value || '',
      modeCards: document.querySelectorAll('[data-mode-card]').length,
      endpointScheduleHidden: document.querySelector('#fn3EndpointSchedule')?.hidden,
      replacementHidden: document.querySelector('#fn3ReplacementSettings')?.hidden,
      scope: document.querySelector('input[name="fn3Scope"]:checked')?.value || '',
      settingsActive: document.querySelector('[data-page-view="settings"]')?.classList.contains('active'),
      settingsV3: document.querySelector('[data-page-view="settings"]')?.dataset.settingsV3 || '',
      nav: [...document.querySelectorAll('.sidebar .nav-btn')].map(n => (n.textContent || '').trim()),
      svgWidths: ['#fn3Save svg','#fn3MaintenanceSave svg','#fn3Check svg','.fn3-extra-action svg'].map(s => {
        const el = document.querySelector(s); return el ? Math.round(el.getBoundingClientRect().width) : 0;
      }),
      saveHeight: Math.round(document.querySelector('#fn3Save')?.getBoundingClientRect().height || 0),
      maintenanceSaveHeight: Math.round(document.querySelector('#fn3MaintenanceSave')?.getBoundingClientRect().height || 0),
      checkHeight: Math.round(document.querySelector('#fn3Check')?.getBoundingClientRect().height || 0),
      sidebarWidth: Math.round(document.querySelector('.sidebar')?.getBoundingClientRect().width || 0),
      navFontSize: getComputedStyle(document.querySelector('.nav-btn[data-page="overview"]')).fontSize,
      navHeight: Math.round(document.querySelector('.nav-btn[data-page="overview"]')?.getBoundingClientRect().height || 0),
      autoLabelVisible: !!document.querySelector('#fn3AutoLabel') && getComputedStyle(document.querySelector('#fn3AutoLabel')).display !== 'none',
      currentVpnCardCount: document.querySelectorAll('.fn3-right, #fn3Profile, #fn3Endpoint, #fn3LastQuality, #fn3VPNState').length,
      controlTitle: document.querySelector('#fn3ControlTitle')?.textContent || '',
      backupRoot: document.querySelector('#fn3_backup_root')?.textContent || '',
      backupSnapshot: document.querySelector('#fn3_backup_snapshot')?.textContent || '',
      backupPath: document.querySelector('#fn3_backup_path')?.textContent || '',
      backupLast: document.querySelector('#fn3_backup_last')?.textContent || ''
    }));
    console.log('SETTINGS_V3_RUNTIME', JSON.stringify(runtime));
    console.log('SETTINGS_V3_CALLS', JSON.stringify(calls.filter(call => call.includes('/api/'))));
    console.log('SETTINGS_V3_PAGE_ERRORS', JSON.stringify(errors));
    console.log('SETTINGS_V3_CONSOLE_ERRORS', JSON.stringify(consoleErrors));

    assert.equal(errors.length, 0, errors.join('\n'));
    assert.equal(consoleErrors.length, 0, consoleErrors.join('\n'));
    assert.equal(runtime.saveDisabled, true, `save baseline not settled: ${JSON.stringify(runtime)}`);
    assert.equal(runtime.maintenanceSaveDisabled, true, `maintenance save baseline not settled: ${JSON.stringify(runtime)}`);
    assert.equal(runtime.maintenanceSaveVisible, true, `maintenance save must be visible inside System maintenance: ${JSON.stringify(runtime)}`);
    assert.match(runtime.maintenanceSaveText, /Сохранено/);
    assert.deepEqual(runtime.nav, ['Обзор','Подписка','Настройки','Маршрутизация','Журнал']);
    assert.ok(runtime.svgWidths.every(width => width > 0 && width <= 24), `oversized action icon detected: ${runtime.svgWidths}`);
    assert.ok(runtime.saveHeight >= 34 && runtime.saveHeight <= 48, `header save has wrong height: ${runtime.saveHeight}`);
    assert.ok(runtime.maintenanceSaveHeight >= 32 && runtime.maintenanceSaveHeight <= 44, `maintenance save has wrong height: ${runtime.maintenanceSaveHeight}`);
    assert.ok(runtime.checkHeight >= 40 && runtime.checkHeight <= 70, `check action has wrong height: ${runtime.checkHeight}`);
    assert.equal(runtime.sidebarWidth, cold.sidebarWidth, `Settings changed sidebar width: overview=${cold.sidebarWidth}, settings=${runtime.sidebarWidth}`);
    assert.equal(runtime.navFontSize, cold.navFontSize, `Settings changed sidebar font size: overview=${cold.navFontSize}, settings=${runtime.navFontSize}`);
    assert.equal(runtime.navHeight, cold.navHeight, `Settings changed sidebar item height: overview=${cold.navHeight}, settings=${runtime.navHeight}`);
    assert.equal(runtime.autoLabelVisible, false, 'duplicate AUTO VPN enabled label must be hidden');
    assert.equal(runtime.currentVpnCardCount, 0, 'Settings must not render the duplicate Current VPN card or its metrics');
    assert.equal(runtime.mode, 'best', `full AUTO VPN must be the saved fixture mode: ${JSON.stringify(runtime)}`);
    assert.equal(runtime.modeCards, 2, `Settings must show exactly two AUTO VPN modes: ${JSON.stringify(runtime)}`);
    assert.equal(runtime.endpointScheduleHidden, true, `endpoint interval must stay hidden in full mode: ${JSON.stringify(runtime)}`);
    assert.equal(runtime.replacementHidden, false, `replacement geography must stay visible in full mode: ${JSON.stringify(runtime)}`);
    assert.equal(runtime.controlTitle, 'Полный AUTO VPN', `unexpected AUTO VPN control copy: ${runtime.controlTitle}`);
    assert.equal(runtime.backupRoot, '/opt/backups/freenet-settings', `backup root must be visible: ${JSON.stringify(runtime)}`);
    assert.equal(runtime.backupSnapshot, 'backup-20260912-023000.000000000', `latest snapshot id must be visible: ${JSON.stringify(runtime)}`);
    assert.equal(runtime.backupPath, '/opt/backups/freenet-settings/backup-20260912-023000.000000000', `latest snapshot path must be visible: ${JSON.stringify(runtime)}`);
    assert.match(runtime.backupLast, /Снимок создан/, `backup schedule must explain success: ${runtime.backupLast}`);

    const settingsPage = page.locator('[data-page-view="settings"]');
    assert.equal((await settingsPage.locator('h1').first().textContent()).trim(), 'Настройки / Система');
    assert.equal(await settingsPage.getByText('Интернет и DNS', {exact:true}).count(), 0);
    assert.equal(await settingsPage.getByText('Только endpoint', {exact:true}).count(), 0);
    assert.equal(await settingsPage.getByText('Лучший VPN автоматически', {exact:true}).count(), 0);
    assert.equal(await settingsPage.getByText('Системное обслуживание', {exact:true}).count(), 1);
    assert.equal(await settingsPage.locator('[data-mode-card]').count(), 2);
    assert.equal(await settingsPage.locator('[data-scope-card]').count(), 3);
    assert.equal(await settingsPage.locator('.fn3-extra-card').count(), 4);
    assert.equal(await settingsPage.locator('#fn3MaintenanceSave').count(), 1, 'System maintenance must expose one explicit save action');
    assert.equal(await page.locator('.sidebar .nav-btn[data-page="system"]').count(), 0);
    assert.equal(await page.locator('.sidebar .nav-btn[data-page="journal"]').count(), 1);
    assert.equal(await page.locator('#fn3Journal').count(), 0, 'Settings must not duplicate the AUTO VPN journal table');
    assert.equal(await page.locator('#fn3AllEvents').count(), 1, 'Settings must keep one compact link to the shared Journal');
    assert.equal(await settingsPage.getByText('Текущий VPN', {exact:true}).count(), 0, 'duplicate Current VPN title must be removed from Settings');
    assert.equal(await settingsPage.locator('.fn3-right').count(), 0, 'Current VPN layout column must be removed from Settings');
    const settingsGeometry = await page.evaluate(() => {
      const auto = document.querySelector('.fn3-left .fn3-card')?.getBoundingClientRect();
      const maintenance = document.querySelector('.fn3-extra')?.getBoundingClientRect();
      return {
        autoWidth: Math.round(auto?.width || 0),
        maintenanceWidth: Math.round(maintenance?.width || 0),
        autoTop: Math.round(auto?.top || 0),
        autoBottom: Math.round(auto?.bottom || 0),
        maintenanceTop: Math.round(maintenance?.top || 0)
      };
    });
    assert.ok(Math.abs(settingsGeometry.autoWidth - settingsGeometry.maintenanceWidth) <= 2, `AUTO VPN and System maintenance must use the same full width: ${JSON.stringify(settingsGeometry)}`);
    assert.ok(settingsGeometry.maintenanceTop > settingsGeometry.autoBottom, `System maintenance must follow AUTO VPN without a duplicate Current VPN block: ${JSON.stringify(settingsGeometry)}`);

    // AUTO VPN exposes two explicit user modes. Endpoint-only must hide all
    // replacement geography and reveal only the same-profile refresh interval.
    await page.locator('input[name="fn3Mode"][value="endpoint"]').check();
    await page.waitForFunction(() => !document.querySelector('#fn3EndpointSchedule')?.hidden && document.querySelector('#fn3ReplacementSettings')?.hidden);
    assert.equal(await page.locator('#fn3EndpointInterval').isVisible(), true, 'endpoint-only mode must expose its refresh interval');
    assert.equal(await page.locator('input[name="fn3Scope"][value="region"]').isDisabled(), true, 'replacement geography must be inactive in endpoint-only mode');
    assert.equal((await page.locator('#fn3ControlTitle').textContent()).trim(), 'Только текущий VPN');
    assert.match((await page.locator('#fn3SafetyText').textContent()) || '', /STOP без смены сервера/);
    await page.locator('#fn3EndpointInterval').selectOption('30m');
    assert.equal(await page.locator('#fn3Save').isDisabled(), false, 'mode/interval change must become a saveable Settings draft');

    await page.locator('input[name="fn3Mode"][value="best"]').check();
    await page.waitForFunction(() => document.querySelector('#fn3EndpointSchedule')?.hidden && !document.querySelector('#fn3ReplacementSettings')?.hidden);
    assert.equal(await page.locator('input[name="fn3Scope"][value="region"]').isDisabled(), false, 'full mode must restore replacement geography controls');
    assert.equal((await page.locator('#fn3ControlTitle').textContent()).trim(), 'Полный AUTO VPN');

    // Backup actions must explain exactly which local snapshot was created/restored.
    await page.locator('[data-v3-action="backup_create"]').click();
    await page.waitForFunction(() => {
      const node = document.querySelector('#fn3_backup_result');
      return node && !node.hidden && node.textContent.includes('Снимок создан') && node.textContent.includes('backup-20260921-174500.123456789');
    });
    assert.equal(backupCreatePosts, 1, 'Create snapshot must issue exactly one backup action');
    assert.equal(await page.locator('#fn3_backup_snapshot').textContent(), 'backup-20260921-174500.123456789');
    assert.equal(await page.locator('#fn3_backup_path').textContent(), '/opt/backups/freenet-settings/backup-20260921-174500.123456789');
    const createResult = (await page.locator('#fn3_backup_result').textContent()) || '';
    assert.match(createResult, /Зафиксировано состояние 4 контролируемых компонентов/);
    assert.doesNotMatch(createResult, /token=|vless:\/\/|uuid|pbk=|sid=/i, 'backup result must not expose secret-bearing values');

    page.once('dialog', async dialog => {
      assert.match(dialog.message(), /backup-20260921-174500\.123456789/, 'restore confirmation must name the selected snapshot');
      await dialog.accept();
    });
    await page.locator('[data-v3-action="backup_restore"]').click();
    await page.waitForFunction(() => document.querySelector('#fn3_backup_result')?.textContent.includes('Снимок восстановлен и проверен'));
    assert.equal(backupRestorePosts, 1, 'Restore snapshot must issue exactly one backup action');
    const restoreResult = (await page.locator('#fn3_backup_result').textContent()) || '';
    assert.match(restoreResult, /проверил восстановленные файлы/);
    assert.match(restoreResult, /backup-20260921-174500\.123456789/);

    const visibleProvider = await page.evaluate(() => [...document.querySelectorAll('.topbar *')].some(el => {
      if ((el.textContent || '').trim() !== 'Владлинк') return false;
      const style = getComputedStyle(el);
      return style.display !== 'none' && style.visibility !== 'hidden' && el.getClientRects().length > 0;
    }));
    assert.equal(visibleProvider, false, 'ISP/provider must not remain visible in topbar');

    await page.locator('input[name="fn3Scope"][value="allowlist"]').check();
    await page.waitForSelector('#fn3CountryPop:not([hidden]) .fn3-country-item');
    await page.waitForFunction(() => document.querySelector('#fn3CountryPop .flag-us'));
    const countryPicker = await page.evaluate(() => ({
      texts: [...document.querySelectorAll('#fn3CountryPop .fn3-country-item')].map(n => (n.textContent || '').trim()),
      classes: [...document.querySelectorAll('#fn3CountryPop .fn3-country-flag')].map(n => n.className),
      checked: [...document.querySelectorAll('#fn3CountryPop .fn3-country-item input:checked')].map(n => n.value).sort(),
      itemFont: parseFloat(getComputedStyle(document.querySelector('#fn3CountryPop .fn3-country-item')).fontSize),
      extraFont: parseFloat(getComputedStyle(document.querySelector('.fn3-extra-card p')).fontSize)
    }));
    assert.ok(countryPicker.texts.some(text => text.includes('США')), `US missing from live catalog: ${JSON.stringify(countryPicker)}`);
    assert.ok(countryPicker.texts.some(text => text.includes('Нидерланды')), `NL missing from live catalog: ${JSON.stringify(countryPicker)}`);
    assert.ok(countryPicker.texts.some(text => text.includes('Великобритания')), `GB missing from live catalog: ${JSON.stringify(countryPicker)}`);
    assert.ok(countryPicker.texts.some(text => text.includes('Ранее выбранная страна')), `unavailable selected country not preserved: ${JSON.stringify(countryPicker)}`);
    assert.ok(countryPicker.classes.some(value => /\bflag-us\b/.test(value)), `US CSS flag missing: ${countryPicker.classes}`);
    assert.ok(countryPicker.classes.some(value => /\bflag-nl\b/.test(value)), `NL CSS flag missing: ${countryPicker.classes}`);
    assert.ok(countryPicker.classes.some(value => /\bflag-gb\b/.test(value)), `GB CSS flag missing: ${countryPicker.classes}`);
    assert.ok(countryPicker.texts.every(text => !/^(PL|NL|FR|GB|US)\b/.test(text)), `country code leaked into picker copy: ${countryPicker.texts}`);
    assert.deepEqual(countryPicker.checked, ['fr','pl'], `saved selected countries were not preserved: ${countryPicker.checked}`);
    assert.ok(countryPicker.itemFont >= 11, `country picker typography too small: ${countryPicker.itemFont}px`);
    assert.ok(countryPicker.extraFont >= 10, `maintenance-card typography too small: ${countryPicker.extraFont}px`);
    await page.locator('#fn3CountriesApply').click();

    const save = page.locator('#fn3Save');
    const maintenanceSave = page.locator('#fn3MaintenanceSave');
    assert.equal(await save.isDisabled(), false);
    assert.equal(await maintenanceSave.isDisabled(), false);
    assert.match(await save.textContent(), /Сохранить изменения/);
    assert.match(await maintenanceSave.textContent(), /Сохранить изменения/);
    await page.locator('input[name="fn3Scope"][value="current"]').check();
    assert.equal(await save.isDisabled(), false);
    assert.equal(await maintenanceSave.isDisabled(), false);
    assert.match(await save.textContent(), /Сохранить изменения/);
    assert.match(await maintenanceSave.textContent(), /Сохранить изменения/);
    await maintenanceSave.click();
    await page.waitForFunction(() => document.querySelector('#fn3MaintenanceSave')?.disabled && /Сохранено/.test(document.querySelector('#fn3MaintenanceSave')?.textContent || ''));
    assert.equal(await save.isDisabled(), true);
    assert.equal(await maintenanceSave.isDisabled(), true);
    assert.match(await save.textContent(), /Сохранено/);
    assert.match(await maintenanceSave.textContent(), /Сохранено/);
    assert.ok(calls.filter(call => call === 'POST /api/settings-v3').length >= 1, 'maintenance save must persist through canonical settings API');

    const maintenanceSaveGeometry = await page.evaluate(() => {
      const button = document.querySelector('#fn3MaintenanceSave')?.getBoundingClientRect();
      const section = document.querySelector('.fn3-extra')?.getBoundingClientRect();
      const icon = document.querySelector('#fn3MaintenanceSave svg')?.getBoundingClientRect();
      return {
        buttonWidth: Math.round(button?.width || 0),
        buttonHeight: Math.round(button?.height || 0),
        iconWidth: Math.round(icon?.width || 0),
        iconHeight: Math.round(icon?.height || 0),
        sectionWidth: Math.round(section?.width || 0)
      };
    });
    assert.ok(maintenanceSaveGeometry.buttonWidth >= 140 && maintenanceSaveGeometry.buttonWidth <= 260, `desktop maintenance save must stay compact: ${JSON.stringify(maintenanceSaveGeometry)}`);
    assert.ok(maintenanceSaveGeometry.buttonHeight >= 32 && maintenanceSaveGeometry.buttonHeight <= 44, `desktop maintenance save height escaped control range: ${JSON.stringify(maintenanceSaveGeometry)}`);
    assert.ok(maintenanceSaveGeometry.iconWidth <= 18 && maintenanceSaveGeometry.iconHeight <= 18, `maintenance save icon must remain a normal button icon: ${JSON.stringify(maintenanceSaveGeometry)}`);
    assert.ok(maintenanceSaveGeometry.buttonWidth < maintenanceSaveGeometry.sectionWidth, `desktop maintenance save must remain a footer action: ${JSON.stringify(maintenanceSaveGeometry)}`);

    await page.locator('.nav-btn[data-page="overview"]').click();
    await page.waitForFunction(() => document.querySelector('[data-page-view="overview"]')?.classList.contains('active'));
    assert.equal(await settingsPage.isVisible(), false, 'Settings remains visible on Overview');

    await page.locator('.nav-btn[data-page="settings"]').click();
    await page.waitForFunction(() => document.querySelector('[data-page-view="settings"]')?.classList.contains('active'));
    assert.equal(await page.locator('#fn3AutoEnabled').isVisible(), true);

    await page.locator('.nav-btn[data-page="routing"]').click();
    await page.waitForFunction(() => document.querySelector('[data-page-view="routing"]')?.classList.contains('active'));
    assert.equal((await page.locator('[data-page-view="routing"] h1').first().textContent()).trim(), 'Маршрутизация');
    const legacyNetworkCardVisible = await page.locator('[data-page-view="routing"] > .card').first().isVisible();
    assert.equal(legacyNetworkCardVisible, false, 'legacy ISP/DNS Network card must stay hidden on Routing');

    await page.locator('.nav-btn[data-page="settings"]').click();
    await page.waitForFunction(() => document.querySelector('[data-page-view="settings"]')?.classList.contains('active'));
    assert.equal((await settingsPage.locator('h1').first().textContent()).trim(), 'Настройки / Система');
    assert.equal(await page.locator('#fn3AutoEnabled').isVisible(), true, 'Settings v3 did not survive Routing -> Settings navigation');

    await page.locator('.nav-btn[data-page="overview"]').click();
    await page.waitForFunction(() => document.querySelector('[data-page-view="overview"]')?.classList.contains('active'));
    await page.locator('.nav-btn[data-page="settings"]').click();
    await page.waitForFunction(() => document.querySelector('[data-page-view="settings"]')?.classList.contains('active'));
    assert.equal((await settingsPage.locator('h1').first().textContent()).trim(), 'Настройки / Система');
    assert.equal(await page.locator('#fn3AutoEnabled').isVisible(), true, 'Settings v3 did not survive repeated canonical navigation');

    await page.locator('.nav-btn[data-page="settings"]').click();
    await page.waitForFunction(() => document.querySelector('[data-page-view="settings"]')?.classList.contains('active'));
    await page.locator('#fn3AllEvents').click();
    await page.waitForFunction(() => document.querySelector('[data-page-view="journal"]')?.classList.contains('active'));
    await page.waitForSelector('#fn3JournalSummary');
    const journalPage = await page.evaluate(() => ({
      rows: document.querySelectorAll('#fn3JournalFull tr').length,
      stats: [...document.querySelectorAll('#fn3JournalSummary .fn3-journal-stat strong')].map(node => node.textContent.trim()),
      tableFont: parseFloat(getComputedStyle(document.querySelector('.fn3-journal-page .fn3-table')).fontSize),
      kindBadges: document.querySelectorAll('#fn3JournalFull .fn3-kind').length,
      resultBadges: document.querySelectorAll('#fn3JournalFull .fn3-result').length,
      badBadges: document.querySelectorAll('#fn3JournalFull .fn3-result.bad').length
    }));
    assert.equal(journalPage.rows, 6, `full Journal must show all fixture events: ${JSON.stringify(journalPage)}`);
    assert.deepEqual(journalPage.stats, ['6','4','1','1'], `Journal summary counts are wrong: ${JSON.stringify(journalPage)}`);
    assert.ok(journalPage.tableFont >= 13.5, `full Journal typography is still too small: ${journalPage.tableFont}px`);
    assert.equal(journalPage.kindBadges, 6, 'Journal event kinds must use badges');
    assert.equal(journalPage.resultBadges, 6, 'Journal results must use badges');
    assert.equal(journalPage.badBadges, 1, 'failed Journal event must use error treatment');

    await page.locator('.nav-btn[data-page="settings"]').click();
    await page.waitForFunction(() => document.querySelector('[data-page-view="settings"]')?.classList.contains('active'));

    // FreeNet topbar version manager: selecting an older stable release is read-only
    // until exact target plan passes and explicit downgrade confirmation is clicked.
    await page.waitForSelector('#topFreenetUpdate');
    await page.waitForFunction(() => document.querySelector('#topFreenetUpdate')?.textContent.includes('v0.3.43'));
    await page.locator('#topFreenetUpdate').click();
    await page.waitForSelector('#fnVersionList .fn-version-release[data-version="v0.3.42"]');
    assert.equal(await page.locator('#fnVersionList .fn-version-release').count(), 5, 'FreeNet version dropdown must show only five releases initially');
    await page.locator('#fnVersionSearch').fill('v0.3.38');
    await page.waitForSelector('#fnVersionList .fn-version-release[data-version="v0.3.38"]');
    assert.equal(await page.locator('#fnVersionList .fn-version-release').count(), 1, 'version search must query the full catalog beyond the initial five');
    await page.locator('#fnVersionSearch').fill('');
    await page.waitForSelector('#fnVersionList .fn-version-release[data-version="v0.3.42"]');
    assert.equal(await page.locator('#fnVersionList .fn-version-release').count(), 5, 'clearing search must restore the five-release compact list');
    assert.equal(versionApplyPosts, 0, 'opening/searching FreeNet version catalog must be read-only');
    assert.match(await page.locator('#topFreenetUpdate').textContent(), /v0\.3\.43/, 'catalog open must not change current topbar version');
    assert.equal(await page.locator('#fnModalRoot').evaluate(el => el.classList.contains('fn-version-picker-mode')), true, 'FreeNet versions must use anchored dropdown mode');
    assert.equal(await page.locator('#fnModalRoot .fn-modal-backdrop').evaluate(el => getComputedStyle(el).display), 'none', 'FreeNet version browse must not dim the whole page');
    assert.equal(await page.locator('#topFreenetUpdate').getAttribute('aria-expanded'), 'true', 'FreeNet chip must expose dropdown state');
    const freeNetPickerGeometry = await page.evaluate(() => {
      const panel = document.querySelector('#fnModalRoot .fn-modal').getBoundingClientRect();
      const chip = document.querySelector('#topFreenetUpdate').getBoundingClientRect();
      return {width:panel.width,right:panel.right,bottom:panel.bottom,top:panel.top,chipBottom:chip.bottom,viewportWidth:innerWidth,viewportHeight:innerHeight,overflow:document.documentElement.scrollWidth>innerWidth,rootPointer:getComputedStyle(document.querySelector('#fnModalRoot')).pointerEvents,panelPointer:getComputedStyle(document.querySelector('#fnModalRoot .fn-modal')).pointerEvents};
    });
    assert.ok(freeNetPickerGeometry.width <= 540.5, `FreeNet version dropdown must match the VPN-style compact width: ${JSON.stringify(freeNetPickerGeometry)}`);
    assert.ok(freeNetPickerGeometry.right <= freeNetPickerGeometry.viewportWidth && freeNetPickerGeometry.bottom <= freeNetPickerGeometry.viewportHeight, `FreeNet dropdown must stay inside viewport: ${JSON.stringify(freeNetPickerGeometry)}`);
    assert.equal(freeNetPickerGeometry.rootPointer, 'none', 'FreeNet dropdown root must not block the page');
    assert.notEqual(freeNetPickerGeometry.panelPointer, 'none', 'FreeNet dropdown panel must remain interactive');
    assert.equal(freeNetPickerGeometry.overflow, false, 'FreeNet dropdown must not create horizontal overflow');
    await page.locator('#fnVersionList .fn-version-release[data-version="v0.3.42"]').click();
    await page.waitForFunction(() => document.querySelector('#fnVersionDetail')?.textContent.includes('Откатить до v0.3.42'));
    assert.equal(versionApplyPosts, 0, 'target compatibility plan must remain read-only');
    assert.match(await page.locator('#topFreenetUpdate').textContent(), /v0\.3\.43/, 'selected downgrade target must not replace current version');
    const actionGeometry = await page.evaluate(() => {
      const panel = document.querySelector('#fnModalRoot .fn-modal');
      const action = document.querySelector('#fnVersionDetail .fn-version-apply');
      const pr = panel.getBoundingClientRect();
      const ar = action.getBoundingClientRect();
      return {panelBottom:pr.bottom,actionBottom:ar.bottom,scrollHeight:panel.scrollHeight,clientHeight:panel.clientHeight,viewportHeight:innerHeight};
    });
    assert.ok(actionGeometry.actionBottom <= actionGeometry.panelBottom + 1, `version action button must be fully visible inside dropdown: ${JSON.stringify(actionGeometry)}`);
    assert.ok(actionGeometry.scrollHeight <= actionGeometry.clientHeight + 1, `desktop version browse/plan state must fit without panel scrolling: ${JSON.stringify(actionGeometry)}`);
    const versionDetailCopy = (await page.locator('#fnVersionDetail').textContent()) || '';
    assert.match(versionDetailCopy, /Версия проверена и готова к установке/);
    assert.doesNotMatch(versionDetailCopy, /SHA-256|Manifest|exact release|assets|credentials|routing state|cron|staging|snapshot|expected delta/i, 'ordinary version detail must not expose implementation jargon');
    assert.equal(await page.locator('#fnModalConfirm').isHidden(), true, 'version manager must not expose a second confirmation action');
    await page.locator('#fnVersionDetail .fn-version-apply').click();
    await page.waitForFunction(() => window.location.href && document.querySelector('#fnModalTitle')?.textContent.includes('Обновление установлено'), null, {timeout:5000});
    assert.equal(versionApplyPosts, 1, 'one explicit version action must start exactly one version mutation');
    assert.equal(installedFreeNetVersion, 'v0.3.42', 'fixture target must become installed only after apply');

    fs.mkdirSync(artifacts, {recursive:true});
    await page.screenshot({path:path.join(artifacts, 'settings-v3-desktop.png'), fullPage:true});
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'Settings has horizontal overflow');
  } finally {
    await browser.close(); server.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
