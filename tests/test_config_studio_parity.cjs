// Browser regression for Config Studio authenticated full parity after the separately-tested Routing v2 mount.
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
  '03_inbounds': {inbounds:[{tag:'test-in',settings:{auth:'TEST-INBOUND-AUTH'}}]},
  '04_outbounds': {outbounds:[{tag:'test-out',settings:{vnext:[{address:'192.0.2.10',users:[{id:'TEST-UUID-00000000'}]}]}}]},
  '05_routing': {routing:{domainStrategy:'AsIs',rules:[]}},
  '06_policy': {policy:{}}
};

function canonicalRoutingV2Source() {
  return fs.readFileSync(path.join(web, 'routing-v2.js'), 'utf8')
    .replaceAll('`04_outbounds.json`', '04_outbounds.json')
    .replace('[data-page-view="network"].fn-routing-v2>.card.fn-routing-v2-legacy', ':is([data-page-view="routing"],[data-page-view="network"]).fn-routing-v2>.card.fn-routing-v2-legacy')
    .replace("const page = qs('[data-page-view=\"network\"]');", "const page = qs('[data-page-view=\"routing\"],[data-page-view=\"network\"]');");
}

function json(res, body, code = 200) {
  res.writeHead(code, {'Content-Type':'application/json; charset=utf-8'});
  res.end(JSON.stringify(body));
}

function bodyJSON(req) {
  return new Promise(resolve => {
    let raw = '';
    req.on('data', c => { raw += c; });
    req.on('end', () => {
      try { resolve(JSON.parse(raw || '{}')); }
      catch (_) { resolve({}); }
    });
  });
}

function studioBody() {
  return {success:true,mutation:'NONE',xray:{online:true,version:'26.9.9'},tabs:[
    {name:'01_log',kind:'json',access:'editable',present:true,size:33,sha256:'a'.repeat(64),roots:['log'],content:live['01_log'],mutation:'NONE'},
    {name:'02_dns',kind:'json',access:'editable',present:true,size:40,sha256:'b'.repeat(64),roots:['dns'],content:live['02_dns'],mutation:'NONE'},
    {name:'03_inbounds',kind:'json',access:'editable',present:true,size:321,sha256:'c'.repeat(64),roots:['inbounds'],content:live['03_inbounds'],mutation:'NONE'},
    {name:'04_outbounds',kind:'json',access:'editable',present:true,size:654,sha256:'d'.repeat(64),roots:['outbounds'],content:live['04_outbounds'],mutation:'NONE'},
    {name:'05_routing',kind:'json',access:'routing-managed',present:true,size:55,sha256:'e'.repeat(64),roots:['routing'],content:live['05_routing'],mutation:'NONE'},
    {name:'06_policy',kind:'json',access:'routing-managed',present:true,size:20,sha256:'f'.repeat(64),roots:['policy'],content:live['06_policy'],mutation:'NONE'},
    {name:'ip_exclude',kind:'list',access:'read-only',present:true,size:18,sha256:'1'.repeat(64),text:'192.0.2.0/24\n',mutation:'NONE'},
    {name:'port_exclude',kind:'list',access:'read-only',present:true,size:8,sha256:'2'.repeat(64),text:'53\n123\n',mutation:'NONE'},
    {name:'port_proxying',kind:'list',access:'read-only',present:true,size:16,sha256:'3'.repeat(64),text:'80\n443\n596:599\n',mutation:'NONE'}
  ]};
}

const status = {
  version:'0.3.70',country:'Германия',city:'Франкфурт-на-Майне',country_code:'de',
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
      .replace('</body>', scripts.map(n => `<script src="/${n}"></script>`).join('') + '</body>');
    res.writeHead(200, {'Content-Type':'text/html; charset=utf-8'});
    res.end(html);
    return;
  }
  if (url.pathname === '/routing-v2-canonical.js') {
    res.writeHead(200, {'Content-Type':'application/javascript'});
    res.end(canonicalRoutingV2Source());
    return;
  }
  if (scripts.some(n => n !== 'routing-v2-canonical.js' && url.pathname === `/${n}`)) {
    res.writeHead(200, {'Content-Type':'application/javascript'});
    res.end(fs.readFileSync(path.join(web, url.pathname.slice(1)), 'utf8'));
    return;
  }
  if (url.pathname === '/api/auth/status') return json(res,{configured:true,authenticated:true});
  if (url.pathname === '/api/status') return json(res,status);
  if (url.pathname === '/api/network-profile/plan') return json(res,{success:true,supported:true,active:true,extra_profiles:[]});
  if (url.pathname === '/api/geodata/files') return json(res,{success:true,files:[],search_enabled:false});
  if (url.pathname === '/api/capabilities') return json(res,{success:true,split_dns_supported:true,memory_total_mib:1024,split_dns_min_mib:768});
  if (url.pathname === '/api/subscription') return json(res,{success:true,configured:true});
  if (url.pathname === '/api/routing/config') {
    return json(res,{success:true,mutation:'NONE',routing:live['05_routing'],policy:live['06_policy'],routing_present:true,policy_present:true,routing_sha256:'e'.repeat(64),policy_sha256:'f'.repeat(64)});
  }
  if (url.pathname === '/api/config-studio' && req.method === 'GET') return json(res,studioBody());
  if (url.pathname === '/api/config-studio/validate' && req.method === 'POST') {
    const c = await bodyJSON(req);
    assert.ok(['01_log.json','02_dns.json','03_inbounds.json','04_outbounds.json'].includes(c.file), 'unexpected single-file validation target');
    return json(res,{success:true,mutation:'NONE',xray_valid:true,rollback:'NOT_NEEDED',core_restart:false,sha256:'9'.repeat(64),result:'candidate is valid; live config unchanged'});
  }
  if (url.pathname === '/api/config-studio/apply' && req.method === 'POST') {
    const c = await bodyJSON(req);
    const name = String(c.file || '').replace(/\.json$/, '');
    assert.ok(['01_log','02_dns','03_inbounds','04_outbounds'].includes(name), 'unexpected single-file apply target');
    live[name] = c.content;
    return json(res,{success:true,mutation:'APPLIED',xray_valid:true,applied:true,rollback:'NOT_NEEDED',core_restart:false,snapshot:'/safe/snapshot',sha256:'8'.repeat(64),result:'config written and post-validated; Xray restart was not performed'});
  }
  if (url.pathname === '/api/routing/validate' && req.method === 'POST') {
    const c = await bodyJSON(req);
    return json(res,{success:true,mutation:'NONE',xray_valid:true,routing:c.routing,policy:c.policy});
  }
  if (url.pathname === '/api/routing/apply' && req.method === 'POST') {
    const c = await bodyJSON(req);
    live['05_routing'] = c.routing;
    live['06_policy'] = c.policy;
    return json(res,{success:true,mutation:'APPLIED',xray_valid:true,applied:true,rollback:'NOT_NEEDED',result:'routing applied'});
  }
  return json(res,{success:true});
});

(async () => {
  await new Promise(resolve => server.listen(0,'127.0.0.1',resolve));
  const browser = await chromium.launch({headless:true});
  try {
    const page = await browser.newPage({viewport:{width:1600,height:1000}});
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    const base = `http://127.0.0.1:${server.address().port}`;

    await page.goto(`${base}/#routing`);
    await page.waitForSelector('#routingV2Workspace',{state:'attached',timeout:10000});
    // Routing activation/race is covered by routing-v2-browser.yml. This gate starts
    // from an already-mounted Routing page and exercises only Config Studio parity.
    await page.evaluate(() => {
      const workspace = document.querySelector('#routingV2Workspace');
      const owner = workspace?.closest('[data-page-view]');
      document.querySelectorAll('[data-page-view]').forEach(node => node.classList.toggle('active', node === owner));
      if (owner) owner.hidden = false;
    });
    await page.waitForSelector('#routingV2Workspace',{state:'visible'});
    await page.locator('.rv2-mode[data-mode="config"]').click();
    await page.waitForSelector('#csTabsMain',{state:'visible',timeout:10000});
    await page.waitForSelector('#csTabsLists',{state:'visible',timeout:10000});
    await page.waitForFunction(() => document.querySelectorAll('#csTabsMain .cs-tab').length === 6);

    const mainLabels = await page.locator('#csTabsMain .cs-tab').allTextContents();
    const listLabels = await page.locator('#csTabsLists .cs-tab').allTextContents();
    assert.deepEqual(mainLabels, ['01_log','02_dns','03_inbounds','04_outbounds','05_routing','06_policy']);
    assert.deepEqual(listLabels, ['ip_exclude','port_exclude','port_proxying']);
    assert.doesNotMatch(await page.locator('#rv2ConfigPanel').textContent(), /PROTECTED/);
    assert.match(await page.locator('#csXray').textContent(), /Xray запущен/);
    assert.match(await page.locator('#csXray').textContent(), /26\.9\.9/);

    // Authenticated owner sees raw 04_outbounds in the editor and can validate/apply it.
    await page.locator('.cs-tab[data-tab="04_outbounds"]').click();
    const outInput = page.locator('#csInput');
    await outInput.waitFor({state:'visible'});
    assert.match(await outInput.inputValue(), /TEST-UUID-00000000/);
    assert.match(await outInput.inputValue(), /"outbounds"/);
    assert.equal(await page.locator('.cs-editor').evaluate(node => getComputedStyle(node).resize), 'vertical', 'Config Studio editor must be vertically resizable');
    await page.evaluate(() => { document.querySelector('.cs-editor').style.height = '620px'; });
    await page.waitForTimeout(80);
    await page.locator('.cs-tab[data-tab="03_inbounds"]').click();
    await page.waitForSelector('#csInput', {state:'visible'});
    const persistedEditorHeight = await page.locator('.cs-editor').evaluate(node => Math.round(node.getBoundingClientRect().height));
    assert.ok(persistedEditorHeight >= 610 && persistedEditorHeight <= 630, `editor height was not preserved across file switch: ${persistedEditorHeight}px`);
    await page.locator('.cs-tab[data-tab="04_outbounds"]').click();
    await page.waitForSelector('#csInput', {state:'visible'});
    assert.match(await outInput.inputValue(), /TEST-UUID-00000000/);

    await outInput.fill('{\n  "outbounds": [\n    {"tag": "test-out", "settings": {"id": "TEST-UUID-NEW"}}\n  ]\n');
    await page.waitForSelector('#csDiagnostic.show');
    const diagnostic = await page.locator('#csDiagnostic').textContent();
    assert.match(diagnostic,/Ошибка JSON/);
    assert.match(diagnostic,/Строка \d+/);
    assert.equal(await page.locator('#csApply').isDisabled(),true);
    assert.equal(await page.locator('.cs-tab[data-tab="04_outbounds"] .dirty').count(),1);

    await outInput.fill('{"outbounds":[{"tag":"test-out","settings":{"id":"TEST-UUID-NEW"}}]}');
    await page.locator('#csFormat').click();
    assert.match(await outInput.inputValue(),/\n  "outbounds": \[/);
    assert.equal(await page.locator('#csValidate').count(),0,'manual Xray validation button must not be present');
    assert.equal(await page.locator('#csApply').isDisabled(),false,'valid dirty draft must be saveable; backend validates during apply');
    const beforeValidate = calls.filter(x => x === 'POST /api/config-studio/validate').length;
    const beforeOut = calls.filter(x => x === 'POST /api/config-studio/apply').length;
    await page.locator('#csApply').click();
    await page.waitForFunction(() => (document.querySelector('#csNotice')?.textContent || '').includes('post-validation'));
    const afterOut = calls.filter(x => x === 'POST /api/config-studio/apply').length;
    assert.equal(afterOut,beforeOut+1,'04_outbounds must issue exactly one controlled apply');
    assert.equal(calls.filter(x => x === 'POST /api/config-studio/validate').length,beforeValidate,'Save must not require a separate validation POST');
    assert.equal(await page.locator('.cs-tab[data-tab="04_outbounds"] .dirty').count(),0);

    // 03_inbounds is also a normal authenticated editor.
    await page.locator('.cs-tab[data-tab="03_inbounds"]').click();
    assert.equal(await page.locator('#csInput').count(),1);
    assert.match(await page.locator('#csInput').inputValue(),/TEST-INBOUND-AUTH/);

    // List artifacts remain read-only and visibly separated from 01-06.
    await page.locator('.cs-tab[data-tab="port_proxying"]').click();
    assert.equal(await page.locator('#csInput').count(),0,'list artifacts remain read-only in #533');
    assert.match(await page.locator('#csBody').textContent(),/596:599/);
    assert.equal(await page.locator('#csValidate').count(),0);
    assert.equal(await page.locator('#csApply').isDisabled(),true);

    assert.equal(errors.length,0,errors.join('\n'));
    console.log('CONFIG_STUDIO_MAIN_TABS',JSON.stringify(mainLabels));
    console.log('CONFIG_STUDIO_LIST_TABS',JSON.stringify(listLabels));
    console.log('CONFIG_STUDIO_APPLY_CALLS',afterOut);
  } finally {
    await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
})().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
