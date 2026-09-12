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

const settings = {
  success: true,
  auto_vpn: {enabled:true,country_scope:'region',countries:['pl','de'],last_health:'2026-09-12T11:35:00Z',next_health:'2026-09-12T11:40:00Z'},
  automation: {
    current_profile:'Варшава, Польша, Extra',current_endpoint:'192.0.2.42:443',country_code:'pl',
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
  events:[
    {at:'2026-09-12T11:35:00Z',kind:'auto_vpn',result:'success',message:'Текущий VPN работает нормально, смена не требуется.'},
    {at:'2026-09-12T10:35:00Z',kind:'auto_vpn',result:'same',message:'Для текущего VPN нет нового адреса подключения.'}
  ]
};

const status = {
  version:'0.3.43',country:'Польша',city:'Варшава',country_code:'pl',profile_label:'Варшава, Польша, Extra',endpoint:'192.0.2.42:443',
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
  if (url.pathname === '/api/settings-v3') return json(res, settings);
  if (url.pathname === '/api/subscription') return json(res, {success:true,configured:true});
  if (url.pathname === '/api/geodata/files') return json(res, {success:true,files:[],search_enabled:false});
  if (url.pathname === '/api/network-profile/plan') return json(res, {success:true,supported:true,active:true,extra_profiles:[]});
  if (url.pathname === '/api/automation') return json(res, {success:true,settings:{enabled:true},events:[]});
  if (url.pathname === '/api/automation/check') return json(res, {success:true,active:false});
  if (url.pathname === '/api/operation/state') return json(res, {success:true,active:false});
  return json(res, {success:true,available:false,configured:true,active:false});
});

(async () => {
  const automationSource = fs.readFileSync(path.join(web, 'automation.js'), 'utf8');
  const asyncSource = fs.readFileSync(path.join(web, 'automation-async.js'), 'utf8');
  assert.doesNotMatch(automationSource, /dataset\.pageView\s*=\s*['"]settings['"]/, 'legacy Automation must never be renamed to Settings');
  assert.doesNotMatch(automationSource + asyncSource, /dispatchEvent\(new Event\(['"]hashchange['"]\)\)/, 'Settings lifecycle must not use synthetic hashchange');

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
      activePage: document.querySelector('[data-page-view].active')?.dataset.pageView || ''
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
      profile: document.querySelector('#fn3Profile')?.textContent || '',
      endpoint: document.querySelector('#fn3Endpoint')?.textContent || '',
      saveDisabled: document.querySelector('#fn3Save')?.disabled,
      saveText: document.querySelector('#fn3Save')?.textContent || '',
      autoChecked: document.querySelector('#fn3AutoEnabled')?.checked,
      scope: document.querySelector('input[name="fn3Scope"]:checked')?.value || '',
      settingsActive: document.querySelector('[data-page-view="settings"]')?.classList.contains('active'),
      settingsV3: document.querySelector('[data-page-view="settings"]')?.dataset.settingsV3 || '',
      nav: [...document.querySelectorAll('.sidebar .nav-btn')].map(n => (n.textContent || '').trim()),
      svgWidths: ['#fn3Save svg','#fn3Check svg','.fn3-extra-action svg'].map(s => {
        const el = document.querySelector(s); return el ? Math.round(el.getBoundingClientRect().width) : 0;
      }),
      checkHeight: Math.round(document.querySelector('#fn3Check')?.getBoundingClientRect().height || 0)
    }));
    console.log('SETTINGS_V3_RUNTIME', JSON.stringify(runtime));
    console.log('SETTINGS_V3_CALLS', JSON.stringify(calls.filter(call => call.includes('/api/'))));
    console.log('SETTINGS_V3_PAGE_ERRORS', JSON.stringify(errors));
    console.log('SETTINGS_V3_CONSOLE_ERRORS', JSON.stringify(consoleErrors));

    assert.equal(errors.length, 0, errors.join('\n'));
    assert.equal(consoleErrors.length, 0, consoleErrors.join('\n'));
    assert.match(runtime.profile, /Варшава/, `runtime snapshot not applied: ${JSON.stringify(runtime)}`);
    assert.equal(runtime.saveDisabled, true, `save baseline not settled: ${JSON.stringify(runtime)}`);
    assert.deepEqual(runtime.nav, ['Обзор','Подписка','Настройки','Маршрутизация','Журнал']);
    assert.ok(runtime.svgWidths.every(width => width > 0 && width <= 24), `oversized action icon detected: ${runtime.svgWidths}`);
    assert.ok(runtime.checkHeight >= 40 && runtime.checkHeight <= 70, `check action has wrong height: ${runtime.checkHeight}`);

    const settingsPage = page.locator('[data-page-view="settings"]');
    assert.equal((await settingsPage.locator('h1').first().textContent()).trim(), 'Настройки / Система');
    assert.equal(await settingsPage.getByText('Интернет и DNS', {exact:true}).count(), 0);
    assert.equal(await settingsPage.getByText('Только endpoint', {exact:true}).count(), 0);
    assert.equal(await settingsPage.getByText('Лучший VPN автоматически', {exact:true}).count(), 0);
    assert.equal(await settingsPage.locator('[data-scope-card]').count(), 3);
    assert.equal(await settingsPage.locator('.fn3-extra-card').count(), 4);
    assert.equal(await page.locator('.sidebar .nav-btn[data-page="system"]').count(), 0);
    assert.equal(await page.locator('.sidebar .nav-btn[data-page="journal"]').count(), 1);

    const visibleProvider = await page.evaluate(() => [...document.querySelectorAll('.topbar *')].some(el => {
      if ((el.textContent || '').trim() !== 'Владлинк') return false;
      const style = getComputedStyle(el);
      return style.display !== 'none' && style.visibility !== 'hidden' && el.getClientRects().length > 0;
    }));
    assert.equal(visibleProvider, false, 'ISP/provider must not remain visible in topbar');

    const save = page.locator('#fn3Save');
    assert.equal(await save.isDisabled(), true);
    assert.match(await save.textContent(), /Сохранено/);
    await page.locator('input[name="fn3Scope"][value="current"]').check();
    assert.equal(await save.isDisabled(), false);
    assert.match(await save.textContent(), /Сохранить изменения/);

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

    fs.mkdirSync(artifacts, {recursive:true});
    await page.screenshot({path:path.join(artifacts, 'settings-v3-desktop.png'), fullPage:true});
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'Settings has horizontal overflow');
  } finally {
    await browser.close(); server.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
