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
const scripts = ['self-update.js', 'vpn-ux-fix.js', 'operation-coordinator.js', 'accepted-ux.js', 'settings-v3.js'];

const settings = {
  success: true,
  auto_vpn: {
    enabled: true,
    country_scope: 'region',
    countries: ['pl', 'de'],
    last_health: '2026-09-12T11:35:00Z',
    next_health: '2026-09-12T11:40:00Z'
  },
  automation: {
    current_profile: 'Лондон, Великобритания, Extra',
    current_endpoint: '192.0.2.42:443',
    country_code: 'gb',
    current_quality_known: true,
    current_quality_checked_at: '2026-09-12T11:35:00Z',
    current_latency_ms: 163,
    current_download_mbps: 111,
    current_jitter_ms: 14,
    events: [
      {at:'2026-09-12T11:35:00Z',kind:'auto_vpn',result:'success',message:'Текущий VPN работает нормально, смена не требуется.'},
      {at:'2026-09-12T10:35:00Z',kind:'auto_vpn',result:'same',message:'Для текущего VPN нет нового адреса подключения.'}
    ]
  },
  subscription: {enabled:true,interval:'6h',last_run:'2026-09-12T10:30:00Z',next_run:'2026-09-12T16:30:00Z',result:'success'},
  geodata: {enabled:true,interval:'3h',last_run:'2026-09-12T10:05:00Z',next_run:'2026-09-12T13:05:00Z',result:'success'},
  freenet: {enabled:true,interval:'12h',last_run:'2026-09-12T00:15:00Z',next_run:'2026-09-12T12:15:00Z',result:'success'},
  backup: {enabled:true,interval:'24h',last_run:'2026-09-12T02:30:00Z',next_run:'2026-09-13T02:30:00Z',result:'success'},
  events: [
    {at:'2026-09-12T11:35:00Z',kind:'auto_vpn',result:'success',message:'Текущий VPN работает нормально, смена не требуется.'},
    {at:'2026-09-12T10:35:00Z',kind:'auto_vpn',result:'same',message:'Для текущего VPN нет нового адреса подключения.'}
  ]
};

const status = {
  version:'0.3.42', country:'Великобритания', city:'Лондон', country_code:'gb',
  profile_label:'Лондон, Великобритания, Extra', endpoint:'192.0.2.42:443',
  xray_online:true, xkeen_ui_online:true, dns_out_present:true, dns_mode:'xkeen',
  isp:'vladlink', isp_label:'Владлинк', recommended_dns_mode:'xkeen', setup_complete:true,
  install_scenario:'existing_stack', subscription_configured:true, busy:false, updater_busy:false,
  last_action:{success:true}
};

function json(res, body, code = 200) {
  res.writeHead(code, {'Content-Type':'application/json; charset=utf-8'});
  res.end(JSON.stringify(body));
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://localhost');
  if (url.pathname === '/') {
    const html = fs.readFileSync(path.join(web, 'index.html'), 'utf8')
      .replace('</body>', scripts.map(name => `<script src="/${name}"></script>`).join('') + '</body>');
    res.writeHead(200, {'Content-Type':'text/html; charset=utf-8'});
    res.end(html);
    return;
  }
  if (scripts.some(name => url.pathname === `/${name}`)) {
    res.writeHead(200, {'Content-Type':'application/javascript; charset=utf-8'});
    res.end(fs.readFileSync(path.join(web, url.pathname.slice(1)), 'utf8'));
    return;
  }
  if (url.pathname === '/api/auth/status') return json(res, {configured:true, authenticated:true});
  if (url.pathname === '/api/status') return json(res, status);
  if (url.pathname === '/api/settings-v3') return json(res, settings);
  if (url.pathname === '/api/subscription') return json(res, {success:true, configured:true});
  if (url.pathname === '/api/geodata/files') return json(res, {success:true, files:[], search_enabled:false});
  if (url.pathname === '/api/network-profile/plan') return json(res, {success:true, supported:true, active:true, extra_profiles:[]});
  if (url.pathname === '/api/automation') return json(res, {success:true, settings:{enabled:true}, events:[]});
  if (url.pathname === '/api/automation/check') return json(res, {success:true, active:false});
  if (url.pathname === '/api/operation/state') return json(res, {success:true, active:false});
  return json(res, {success:true, available:false, configured:true, active:false});
});

(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch({headless:true});
  try {
    const page = await browser.newPage({viewport:{width:1600,height:960}});
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const base = `http://127.0.0.1:${server.address().port}`;
    await page.goto(`${base}/#settings`);
    await page.waitForSelector('#fn3AutoEnabled', {state:'attached'});
    await page.waitForFunction(() => document.querySelector('[data-page-view="settings"]')?.dataset.settingsV3 === '1');

    assert.equal(errors.length, 0, errors.join('\n'));
    assert.equal((await page.locator('[data-page-view="settings"] h1').first().textContent()).trim(), 'Настройки / Система');
    assert.equal(await page.locator('[data-page-view="settings"] text=Интернет и DNS').count(), 0, 'legacy ISP/DNS card must not survive Settings v3 mount');
    assert.equal(await page.locator('[data-page-view="settings"] text=Только endpoint').count(), 0);
    assert.equal(await page.locator('[data-page-view="settings"] text=Лучший VPN автоматически').count(), 0);
    assert.equal(await page.locator('[data-page-view="settings"] [data-scope-card]').count(), 3);
    assert.equal(await page.locator('[data-page-view="settings"] .fn3-extra-card').count(), 4);
    assert.equal(await page.locator('.sidebar .nav-btn[data-page="system"]').count(), 0, 'separate System nav must be removed');
    assert.equal(await page.locator('.sidebar .nav-btn[data-page="journal"]').count(), 1, 'Journal nav must be present');

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

    const pageText = await page.locator('[data-page-view="settings"]').innerText();
    for (const forbidden of ['hysteresis', 'cooldown', 'Eligible', 'logical-profile', 'Автоматически применять подтверждённое решение']) {
      assert.equal(pageText.includes(forbidden), false, `developer wording leaked: ${forbidden}`);
    }

    fs.mkdirSync(artifacts, {recursive:true});
    await page.screenshot({path:path.join(artifacts, 'settings-v3-desktop.png'), fullPage:true});
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'Settings v3 has horizontal overflow');
  } finally {
    await browser.close();
    server.close();
  }
})().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
