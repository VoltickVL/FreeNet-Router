// Browser regression for Settings Current VPN single-flag presentation.
// Uses documentation-only fixtures; no router or live VPN is contacted.
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');

const root = path.resolve(__dirname, '..');
const web = path.join(root, 'freenet-ui', 'web');
const scripts = ['settings-v3.js', 'profile-label-hygiene.js'];

const settings = {
  success:true,
  auto_vpn:{enabled:true,country_scope:'region',countries:[],next_health:'2026-09-17T08:00:00Z'},
  automation:{
    current_profile:'DE 🇩🇪 🇩🇪 Франкфурт-на-Майне, Германия, Extra',
    current_endpoint:'192.0.2.68:443',
    country_code:'de',
    current_quality_known:true,
    current_quality_checked_at:'2026-09-17T07:15:00Z',
    current_latency_ms:166,
    current_download_mbps:120,
    current_jitter_ms:2,
    events:[]
  },
  subscription:{enabled:false}, geodata:{enabled:false}, freenet:{enabled:false}, backup:{enabled:false}, events:[]
};

const status = {
  version:'0.3.68',country:'Германия',city:'Франкфурт-на-Майне',country_code:'de',
  profile_label:'DE 🇩🇪 🇩🇪 Франкфурт-на-Майне, Германия, Extra',endpoint:'192.0.2.68:443',
  xray_online:true,dns_mode:'xkeen',dns_out_present:true,busy:false,updater_busy:false
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
    res.writeHead(200, {'Content-Type':'application/javascript; charset=utf-8','Cache-Control':'no-store'});
    res.end(fs.readFileSync(path.join(web, url.pathname.slice(1)), 'utf8'));
    return;
  }
  if (url.pathname === '/api/settings-v3') return json(res, settings);
  if (url.pathname === '/api/status') return json(res, status);
  if (url.pathname === '/api/settings-v3/countries') return json(res, {success:true,fresh:true,selected:[],countries:[]});
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
    await page.waitForSelector('#fn3Profile', {state:'visible'});
    await page.waitForFunction(() => (document.querySelector('#fn3Profile')?.textContent || '').includes('Франкфурт-на-Майне'));
    await page.waitForFunction(() => !(document.querySelector('#fn3Profile')?.textContent || '').includes('🇩🇪'));

    const runtime = await page.evaluate(() => ({
      profile: document.querySelector('#fn3Profile')?.textContent || '',
      profileSmall: document.querySelector('#fn3ProfileSmall')?.textContent || '',
      flagText: document.querySelector('#fn3Flag')?.textContent || '',
      flagClass: document.querySelector('#fn3Flag')?.className || '',
      visualFlagCount: document.querySelectorAll('.fn3-profile-top .flag-icon').length,
      loaded: !!window.__freenetProfileLabelHygieneLoaded
    }));

    assert.equal(errors.length, 0, errors.join('\n'));
    assert.equal(runtime.loaded, true, JSON.stringify(runtime));
    assert.equal(runtime.visualFlagCount, 1, `Current VPN must have one canonical flag: ${JSON.stringify(runtime)}`);
    assert.match(runtime.flagClass, /\bflag-de\b/, `German canonical flag missing: ${runtime.flagClass}`);
    assert.equal(runtime.flagText, '', `canonical CSS flag must not contain platform emoji: ${runtime.flagText}`);
    assert.equal(runtime.profile, 'Франкфурт-на-Майне, Германия, Extra');
    assert.equal(runtime.profileSmall, 'Франкфурт-на-Майне, Германия, Extra');
    assert.doesNotMatch(runtime.profile + runtime.profileSmall, /[🇦-🇿]/u, 'profile text still contains regional-indicator flag decoration');
    assert.doesNotMatch(runtime.profile + runtime.profileSmall, /^(?:DE|de)\b/, 'profile text still contains ISO prefix');

    console.log('SETTINGS_SINGLE_FLAG_RUNTIME', JSON.stringify(runtime));
  } finally {
    await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
})().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
