// Browser regression for Config Studio parity: tabs, protected secrets, dirty state,
// JSON diagnostics, formatting, Xray validation and exactly one controlled apply.
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');

const root = path.resolve(__dirname, '..');
const web = path.join(root, 'freenet-ui', 'web');
const calls = [];

let live = {
  '01_log': {log:{loglevel:'warning'}},
  '02_dns': {dns:{servers:['1.1.1.1']}},
  '05_routing': {routing:{domainStrategy:'AsIs',rules:[]}},
  '06_policy': {policy:{}}
};

function canonicalRoutingV2Source() {
  return fs.readFileSync(path.join(web, 'routing-v2.js'), 'utf8')
    .replaceAll('`04_outbounds.json`', '04_outbounds.json')
    .replace(
      '[data-page-view="network"].fn-routing-v2>.card.fn-routing-v2-legacy',
      ':is([data-page-view="routing"],[data-page-view="network"]).fn-routing-v2>.card.fn-routing-v2-legacy'
    )
    .replace(
      "const page = qs('[data-page-view=\"network\"]');",
      "const page = qs('[data-page-view=\"routing\"],[data-page-view=\"network\"]');"
    );
}

function json(res, body, code = 200) {
  res.writeHead(code, {'Content-Type':'application/json; charset=utf-8'});
  res.end(JSON.stringify(body));
}

function bodyJSON(req) {
  return new Promise(resolve => {
    let raw = '';
    req.on('data', chunk => { raw += chunk; });
    req.on('end', () => {
      try { resolve(JSON.parse(raw || '{}')); }
      catch (_) { resolve({}); }
    });
  });
}

function configStudioBody() {
  return {
    success:true, mutation:'NONE', xray:{online:true, version:'26.9.9'},
    tabs:[
      {name:'01_log',kind:'json',access:'editable',present:true,size:33,sha256:'a'.repeat(64),roots:['log'],content:live['01_log'],mutation:'NONE'},
      {name:'02_dns',kind:'json',access:'editable',present:true,size:40,sha256:'b'.repeat(64),roots:['dns'],content:live['02_dns'],mutation:'NONE'},
      {name:'03_inbounds',kind:'json',access:'protected',present:true,size:321,sha256:'c'.repeat(64),roots:['inbounds'],mutation:'NONE'},
      {name:'04_outbounds',kind:'json',access:'protected',present:true,size:654,sha256:'d'.repeat(64),roots:['outbounds'],mutation:'NONE'},
      {name:'05_routing',kind:'json',access:'routing-managed',present:true,size:55,sha256:'e'.repeat(64),roots:['routing'],content:live['05_routing'],mutation:'NONE'},
      {name:'06_policy',kind:'json',access:'routing-managed',present:true,size:20,sha256:'f'.repeat(64),roots:['policy'],content:live['06_policy'],mutation:'NONE'},
      {name:'ip_exclude',kind:'list',access:'read-only',present:true,size:18,sha256:'1'.repeat(64),text:'192.0.2.0/24\n',mutation:'NONE'}
    ]
  };
}

const status = {
  version:'0.3.69',country:'Германия',city:'Франкфурт-на-Майне',country_code:'de',
  profile_label:'Франкфурт-на-Майне, Германия, Extra',endpoint:'192.0.2.67:443',
  xray_online:true,xkeen_ui_online:true,dns_out_present:true,dns_mode:'xkeen',isp:'custom',isp_label:'Свой',
  recommended_dns_mode:'xkeen',setup_complete:true,subscription_configured:true,busy:false,updater_busy:false,last_action:{success:true}
};

const scripts = ['vpn-ux-fix.js','automation.js','routing-v2-canonical.js','config-studio-parity.js','routing-apply-ui.js'];
const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://localhost');
  calls.push(`${req.method} ${url.pathname}`);

  if (url.pathname === '/') {
    const html = fs.readFileSync(path.join(web, 'index.html'), 'utf8')
      .replace('</body>', scripts.map(name => `<script src="/${name}"></script>`).join('') + '</body>');
    res.writeHead(200, {'Content-Type':'text/html; charset=utf-8'});
    res.end(html);
    return;
  }
  if (url.pathname === '/routing-v2-canonical.js') {
    res.writeHead(200, {'Content-Type':'application/javascript; charset=utf-8'});
    res.end(canonicalRoutingV2Source());
    return;
  }
  if (scripts.some(name => name !== 'routing-v2-canonical.js' && url.pathname === `/${name}`)) {
    res.writeHead(200, {'Content-Type':'application/javascript; charset=utf-8'});
    res.end(fs.readFileSync(path.join(web, url.pathname.slice(1)), 'utf8'));
    return;
  }
  if (url.pathname === '/api/auth/status') return json(res, {configured:true,authenticated:true});
  if (url.pathname === '/api/status') return json(res, status);
  if (url.pathname === '/api/network-profile/plan') return json(res, {success:true,supported:true,active:true,extra_profiles:[]});
  if (url.pathname === '/api/geodata/files') return json(res, {success:true,files:[],search_enabled:false});
  if (url.pathname === '/api/capabilities') return json(res, {success:true,split_dns_supported:true,memory_total_mib:1024,split_dns_min_mib:768});
  if (url.pathname === '/api/subscription') return json(res, {success:true,configured:true});
  if (url.pathname === '/api/routing/config') return json(res, {success:true,mutation:'NONE',routing:live['05_routing'],policy:live['06_policy'],routing_present:true,policy_present:true,routing_sha256:'e'.repeat(64),policy_sha256:'f'.repeat(64)});
  if (url.pathname === '/api/config-studio' && req.method === 'GET') return json(res, configStudioBody());
  if (url.pathname === '/api/config-studio/validate' && req.method === 'POST') {
    const candidate = await bodyJSON(req);
    assert.ok(['01_log.json','02_dns.json'].includes(candidate.file));
    return json(res, {success:true,mutation:'NONE',xray_valid:true,rollback:'NOT_NEEDED',core_restart:false,sha256:'9'.repeat(64),result:'candidate is valid; live config unchanged'});
  }
  if (url.pathname === '/api/config-studio/apply' && req.method === 'POST') {
    const candidate = await bodyJSON(req);
    if (candidate.file === '01_log.json') live['01_log'] = candidate.content;
    if (candidate.file === '02_dns.json') live['02_dns'] = candidate.content;
    return json(res, {success:true,mutation:'APPLIED',xray_valid:true,applied:true,rollback:'NOT_NEEDED',core_restart:false,snapshot:'/safe/snapshot',sha256:'8'.repeat(64),result:'config written and post-validated; Xray restart was not performed'});
  }
  if (url.pathname === '/api/routing/validate' && req.method === 'POST') {
    const candidate = await bodyJSON(req);
    return json(res, {success:true,mutation:'NONE',xray_valid:true,routing:candidate.routing,policy:candidate.policy});
  }
  if (url.pathname === '/api/routing/apply' && req.method === 'POST') {
    const candidate = await bodyJSON(req);
    live['05_routing'] = candidate.routing; live['06_policy'] = candidate.policy;
    return json(res, {success:true,mutation:'APPLIED',xray_valid:true,applied:true,rollback:'NOT_NEEDED',result:'routing applied'});
  }
  return json(res, {success:true});
});

(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch({headless:true});
  try {
    const page = await browser.newPage({viewport:{width:1600,height:1000}});
    const errors = [];
    page.on('pageerror', error => { errors.push(error.message); console.error('CONFIG_STUDIO_PAGEERROR', error.message); });
    const base = `http://127.0.0.1:${server.address().port}`;
    await page.goto(`${base}/#routing`);
    await page.waitForSelector('#routingV2Workspace', {state:'visible', timeout:10000});
    await page.locator('.rv2-mode[data-mode="config"]').click();
    await page.waitForSelector('#csTabs', {state:'visible', timeout:10000});
    await page.waitForFunction(() => document.querySelectorAll('.cs-tab').length >= 6);

    const labels = await page.locator('.cs-tab').allTextContents();
    for (const expected of ['01_log','02_dns','03_inbounds','04_outbounds','05_routing','06_policy','ip_exclude']) {
      assert.ok(labels.some(label => label.includes(expected)), `missing tab ${expected}: ${labels.join(', ')}`);
    }
    assert.match(await page.locator('#csXray').textContent(), /Xray запущен/);
    assert.match(await page.locator('#csXray').textContent(), /26\.9\.9/);

    await page.locator('.cs-tab[data-tab="04_outbounds"]').click();
    assert.match(await page.locator('#csBody').textContent(), /защищённая вкладка/);
    assert.equal(await page.locator('#csInput').count(), 0, 'protected outbounds must not expose raw editor');
    assert.doesNotMatch(await page.locator('#csBody').textContent(), /SECRET|UUID|shortId/i, 'protected panel must not expose credential values');

    await page.locator('.cs-tab[data-tab="02_dns"]').click();
    const input = page.locator('#csInput');
    await input.fill('{\n  "dns": {\n    "servers": ["1.1.1.1"]\n    "queryStrategy": "UseIP"\n  }\n}\n');
    await page.waitForSelector('#csDiagnostic.show');
    const diagnostic = await page.locator('#csDiagnostic').textContent();
    assert.match(diagnostic, /Ошибка JSON/);
    assert.match(diagnostic, /Строка \d+/);
    assert.equal(await page.locator('#csApply').isDisabled(), true, 'broken JSON must block apply');
    assert.equal(await page.locator('.cs-tab[data-tab="02_dns"] .dirty').count(), 1, 'dirty tab marker missing');

    await input.fill('{"dns":{"servers":["8.8.8.8"],"queryStrategy":"UseIP"}}');
    await page.locator('#csFormat').click();
    assert.match(await input.inputValue(), /\n  "dns": \{/);
    assert.match(await input.inputValue(), /\n    "servers": \[/);

    await page.locator('#csValidate').click();
    await page.waitForFunction(() => document.querySelector('#csState')?.textContent.includes('XRAY VALID'));
    assert.equal(await page.locator('#csApply').isDisabled(), false, 'validated dirty candidate must enable apply');

    const beforeApply = calls.filter(call => call === 'POST /api/config-studio/apply').length;
    await page.locator('#csApply').click();
    await page.waitForFunction(() => (document.querySelector('#csNotice')?.textContent || '').includes('post-validation'));
    const afterApply = calls.filter(call => call === 'POST /api/config-studio/apply').length;
    assert.equal(afterApply, beforeApply + 1, 'Config Studio must issue exactly one controlled apply');
    assert.equal(await page.locator('.cs-tab[data-tab="02_dns"] .dirty').count(), 0, 'dirty marker must clear after authoritative reload');
    assert.equal(errors.length, 0, errors.join('\n'));

    console.log('CONFIG_STUDIO_TABS', JSON.stringify(labels));
    console.log('CONFIG_STUDIO_APPLY_CALLS', afterApply);
  } finally {
    await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
})().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
