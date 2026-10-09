// Browser regression for v0.4.56 DIRECT repair UX.
// The dedicated repair action must remain reachable even when the generic
// Settings Save button is disabled by Settings v3 core.
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');

const root = path.resolve(__dirname, '..');
const web = path.join(root, 'freenet-ui', 'web');
const scripts = ['settings-v3.js', 'settings-dns-ui.js'];
let dnsPosts = 0;
let settingsPosts = 0;
let lastDNSBody = null;

const settings = {
  success:true,
  auto_vpn:{enabled:true,mode:'best',endpoint_interval:'1h',country_scope:'region',countries:['at'],last_health:'2026-10-02T07:00:00Z',next_health:'2026-10-02T07:05:00Z'},
  automation:{current_profile:'Вена, Австрия, Extra',current_endpoint:'192.0.2.56:443',country_code:'at',current_quality_known:true,current_latency_ms:150,current_download_mbps:146,current_jitter_ms:23,events:[]},
  subscription:{enabled:true,interval:'6h'}, geodata:{enabled:true,interval:'3h'}, freenet:{enabled:true,interval:'12h'}, backup:{enabled:true,interval:'24h'}, events:[]
};

const status = {
  version:'0.4.60',country:'Австрия',city:'Вена',country_code:'at',profile_label:'Вена, Австрия, Extra',endpoint:'192.0.2.56:443',
  xray_online:true,dns_mode:'xkeen',dns_out_present:true,busy:false,updater_busy:false
};

let dnsControl = {
  success:true,
  mode:'xkeen',
  active_mode:'xkeen',
  direct_provider:'yandex-doh',
  vpn_provider:'google-doh',
  active_direct_provider:'yandex-doh',
  active_vpn_provider:'google-doh',
  direct_options:[
    {id:'yandex-doh',label:'Яндекс DNS',endpoint:'77.88.8.8:53'},
    {id:'google-doh',label:'Google DNS',endpoint:'8.8.8.8:53'}
  ],
  vpn_options:[
    {id:'google-doh',label:'Google DoH',endpoint:'https://dns.google/dns-query'},
    {id:'yandex-doh',label:'Яндекс DoH',endpoint:'https://dns.yandex.ru/dns-query'}
  ],
  runtime_state:'repairable',
  direct_egress_state:'accepted',
  repair_required:true,
  split_supported:true,
  apply_supported:true,
  warning:'DIRECT DNS использует hostname DoH и требует bootstrap-safe восстановления.'
};

function json(res, body, code = 200) {
  res.writeHead(code, {'Content-Type':'application/json; charset=utf-8','Cache-Control':'no-store'});
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
  if (scripts.some(name => url.pathname === `/${name}`) || url.pathname === '/settings-v3-core.js') {
    res.writeHead(200, {'Content-Type':'application/javascript; charset=utf-8','Cache-Control':'no-store'});
    res.end(fs.readFileSync(path.join(web, url.pathname.slice(1)), 'utf8'));
    return;
  }
  if (url.pathname === '/api/auth/status') return json(res, {configured:true,authenticated:true});
  if (url.pathname === '/api/status') return json(res, status);
  if (url.pathname === '/api/settings-v3') {
    if (req.method === 'POST') settingsPosts += 1;
    return json(res, settings);
  }
  if (url.pathname === '/api/settings-v3/countries') return json(res, {success:true,fresh:true,selected:['at'],countries:[{code:'at',name:'Австрия',available:true}]});
  if (url.pathname === '/api/settings-v3/dns/control') {
    if (req.method === 'GET') return json(res, dnsControl);
    let raw = '';
    req.on('data', chunk => { raw += chunk; });
    req.on('end', () => {
      dnsPosts += 1;
      lastDNSBody = JSON.parse(raw || '{}');
      dnsControl = {...dnsControl,runtime_state:'accepted',direct_egress_state:'accepted',repair_required:false,warning:''};
      return json(res, dnsControl);
    });
    return;
  }
  if (url.pathname === '/api/subscription') return json(res, {success:true,configured:true});
  if (url.pathname === '/api/geodata/files') return json(res, {success:true,files:[],search_enabled:false});
  if (url.pathname === '/api/network-profile/plan') return json(res, {success:true,supported:true,active:true,extra_profiles:[]});
  if (url.pathname === '/api/automation') return json(res, {success:true,settings:{enabled:true},events:[]});
  if (url.pathname === '/api/automation/check') return json(res, {success:true,active:false});
  if (url.pathname === '/api/operation/state') return json(res, {success:true,active:false});
  return json(res, {success:true,available:false,configured:true,active:false});
});

(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch({headless:true});
  try {
    const page = await browser.newPage({viewport:{width:1440,height:900}});
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const base = `http://127.0.0.1:${server.address().port}`;

    await page.goto(`${base}/#settings`);
    await page.waitForSelector('#fn3DnsRepair', {state:'attached'});
    const dnsHeader = page.locator('#fn3DnsCard .fn3-dns-head');
    assert.equal(await dnsHeader.getAttribute('aria-expanded'), 'false',
      'Settings must enter with DNS folded, even when repair is available');
    await dnsHeader.click();
    await page.waitForSelector('#fn3DnsRepair', {state:'visible'});
    const icons = await page.evaluate(() => ['#fn3DnsCard .fn3-dns-icon','#fn3AutoCard .fn3-icon'].map(selector => {
      const element = document.querySelector(selector);
      const glyph = element?.querySelector('svg');
      return {box:Math.round(element?.getBoundingClientRect().width || 0),
        glyph:Math.round(glyph?.getBoundingClientRect().width || 0)};
    }));
    assert.deepEqual(icons, [{box:48,glyph:26},{box:48,glyph:26}],
      'DNS and AUTO must share identical product icon tokens with SVG glyphs');
    await page.waitForFunction(() => document.querySelector('#fn3DnsWarning')?.textContent.includes('DIRECT DNS'));

    assert.equal(await page.locator('#fn3Save').isDisabled(), true, 'generic Settings Save must remain independently disabled');
    assert.match((await page.locator('#fn3Save').textContent()) || '', /Сохранено/);
    assert.equal(await page.locator('#fn3DnsRepair').isDisabled(), false, 'DIRECT repair must be independently actionable');
    assert.match((await page.locator('#fn3DnsRepair').textContent()) || '', /Восстановить DIRECT/);

    await page.locator('#fn3DnsRepair').click();
    await page.waitForFunction(() => document.querySelector('#fn3DnsRepairRow')?.hidden === true);

    assert.equal(dnsPosts, 1, 'DIRECT repair must issue exactly one DNS control mutation');
    assert.equal(settingsPosts, 0, 'DIRECT repair must not submit generic Settings state');
    assert.deepEqual(lastDNSBody, {
      mode:'xkeen',
      direct_provider:'yandex-doh',
      vpn_provider:'google-doh',
      confirm:true
    }, 'DIRECT repair must preserve the selected Split DNS providers');
    assert.equal(await page.locator('#fn3Save').isDisabled(), true, 'generic Settings Save must stay untouched after repair');
    assert.match((await page.locator('#fn3Save').textContent()) || '', /Сохранено/);
    assert.equal((await page.locator('#fn3DnsWarning').textContent()) || '', '');
    assert.equal(await page.locator('#fn3DnsState').evaluate(el => el.classList.contains('ok')), true, 'DNS state must become healthy after accepted DIRECT repair');
    assert.equal(errors.length, 0, errors.join('\n'));

    console.log('SETTINGS_DNS_DIRECT_REPAIR_RUNTIME', JSON.stringify({dnsPosts,settingsPosts,lastDNSBody}));
  } finally {
    await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
})().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
