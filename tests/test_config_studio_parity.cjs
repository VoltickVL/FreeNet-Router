// Browser regression for Config Studio authenticated full parity after the separately-tested Routing v2 mount.
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');

const root = path.resolve(__dirname, '..');
const web = path.join(root, 'freenet-ui', 'web');
const calls = [];
let standardGeoHadExplicitFile = false;
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
  xray_online:true,xkeen_ui_online:true,dns_out_present:true,dns_mode:'xkeen',
  setup_complete:true,subscription_configured:true,busy:false,updater_busy:false,last_action:{success:true}
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
  if (url.pathname === '/api/geodata/files') return json(res,{success:true,files:[{name:'geosite.dat',kind:'geosite',size:4096},{name:'geosite_v2fly.dat',kind:'geosite',size:4096},{name:'geoip.dat',kind:'geoip',size:4096}],search_enabled:true});
  if (url.pathname === '/api/geodata/suggest') {
    const kind = url.searchParams.get('kind') || '';
    const requestedFile = url.searchParams.get('file') || '';
    const q = (url.searchParams.get('q') || '').toLowerCase();
    const category = kind === 'geoip' ? 'private' : 'youtube';
    if (kind === 'geosite' && !q.startsWith('ext:') && requestedFile) standardGeoHadExplicitFile = true;
    const file = requestedFile || (kind === 'geoip' ? 'geoip.dat' : 'geosite_v2fly.dat');
    const selector = requestedFile
      ? 'ext:' + file + ':' + category
      : (kind === 'geoip' ? 'geoip:' + category : 'ext:' + file + ':' + category);
    return json(res,{success:true,kind,query:q,mode:'prefix',mutation:'NONE',suggestions:[
      {file,kind,category,selector,ext_selector:'ext:'+file+':'+category,match:'category'}
    ],warnings:[]});
  }
  if (url.pathname === '/api/capabilities') return json(res,{success:true,split_dns_supported:true,memory_total_mib:1024,split_dns_min_mib:768});
  if (url.pathname === '/api/subscription') return json(res,{success:true,configured:true});
  if (url.pathname === '/api/policy/compile' && req.method === 'POST') {
    const request = await bodyJSON(req);
    const rules = Array.isArray(request.rules) ? request.rules : [];
    const compiledRules = rules.map((rule,index) => {
      const action = String(rule.action || 'DIRECT').toUpperCase();
      const kind = String(rule.selector?.kind || '');
      const payload_outbound = action === 'DIRECT' ? 'direct' : action === 'VPN' ? 'vless-reality' : 'block';
      const dns_leg = (kind === 'domain' || kind === 'geosite') ? (action === 'DIRECT' ? 'dns-direct' : action === 'VPN' ? 'dns-vless' : 'block') : '';
      return {order:index,selector:rule.selector,action,payload_outbound,...(dns_leg ? {dns_leg} : {})};
    });
    return json(res,{success:true,mutation:'NONE',compiled:{rules:compiledRules,payload:compiledRules,dns:compiledRules.filter(rule=>rule.dns_leg)}});
  }
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
    return json(res,{success:true,mutation:'APPLIED',xray_valid:true,applied:true,rollback:'NOT_NEEDED',core_restart:true,snapshot:'/safe/snapshot',sha256:'8'.repeat(64),result:'config written, post-validated and activated by firewall-preserving Xray core restart'});
  }
  if (url.pathname === '/api/routing/validate' && req.method === 'POST') {
    const c = await bodyJSON(req);
    return json(res,{success:true,mutation:'NONE',xray_valid:true,routing:c.routing,policy:c.policy});
  }
  if (url.pathname === '/api/routing/apply' && req.method === 'POST') {
    const c = await bodyJSON(req);
    live['05_routing'] = c.routing;
    live['06_policy'] = c.policy;
    return json(res,{success:true,mutation:'APPLIED',xray_valid:true,applied:true,rollback:'NOT_NEEDED',core_restart:true,result:'routing policy applied; active Xray Core restarted with firewall-preserving core-only path'});
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
    const editorFooter = await page.evaluate(() => {
      const body = document.querySelector('#csBody');
      const toolbar = document.querySelector('.cs-toolbar');
      const bodyRect = body.getBoundingClientRect();
      const toolbarRect = toolbar.getBoundingClientRect();
      return {
        follows: !!(body.compareDocumentPosition(toolbar) & Node.DOCUMENT_POSITION_FOLLOWING),
        bodyBottom: Math.round(bodyRect.bottom),
        toolbarTop: Math.round(toolbarRect.top),
        footerClass: toolbar.classList.contains('cs-editor-footer')
      };
    });
    assert.equal(editorFooter.follows, true, `Config Studio actions must follow the editor body: ${JSON.stringify(editorFooter)}`);
    assert.equal(editorFooter.footerClass, true, 'Config Studio toolbar must use the editor-footer treatment');
    assert.ok(editorFooter.toolbarTop >= editorFooter.bodyBottom - 1, `Config Studio actions must render below the editor: ${JSON.stringify(editorFooter)}`);
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
    assert.match(await page.locator('#csNotice').textContent(),/Xray Core.*core-only path/);
    const afterOut = calls.filter(x => x === 'POST /api/config-studio/apply').length;
    assert.equal(afterOut,beforeOut+1,'04_outbounds must issue exactly one controlled apply');
    assert.equal(calls.filter(x => x === 'POST /api/config-studio/validate').length,beforeValidate,'Save must not require a separate validation POST');
    assert.equal(await page.locator('.cs-tab[data-tab="04_outbounds"] .dirty').count(),0);

    // 03_inbounds is also a normal authenticated editor.
    await page.locator('.cs-tab[data-tab="03_inbounds"]').click();
    assert.equal(await page.locator('#csInput').count(),1);
    assert.match(await page.locator('#csInput').inputValue(),/TEST-INBOUND-AUTH/);

    // Rules -> Config Studio: apply once, then open Configuration and see authoritative live state without F5.
    await page.locator('.rv2-mode[data-mode="rules"]').click();
    await page.waitForSelector('#rv2RulesPanel',{state:'visible'});
    await page.locator('.rv4-board-add[data-add-action="DIRECT"]').click();
    await page.locator('#rv2Kind').selectOption('domain');
    await page.locator('#rv2Value').fill('sync.test');
    await page.locator('#rv2AddRule').click();
    await page.waitForFunction(() => document.querySelector('#rv2ApplyRules') && !document.querySelector('#rv2ApplyRules').disabled);
    assert.equal(await page.locator('#rv2ApplyRules').textContent(),'Применить');
    await page.locator('#rv2ApplyRules').click();
    await page.waitForFunction(() => (document.querySelector('#rv2RulesApplyResult')?.textContent || '').includes('Изменения применены'),null,{timeout:10000});
    assert.equal(live['05_routing'].routing.rules.some(rule => Array.isArray(rule.domain) && rule.domain.includes('domain:sync.test')),true);

    await page.locator('.rv2-mode[data-mode="config"]').click();
    await page.waitForSelector('#csTabsMain',{state:'visible'});
    await page.locator('.cs-tab[data-tab="05_routing"]').click();
    await page.waitForFunction(() => (document.querySelector('#csInput')?.value || '').includes('domain:sync.test'));
    assert.equal(await page.locator('#csApply').textContent(),'Применить');

    // Routing editor GeoData autocomplete replaces only the active JSON string token and never applies by itself.
    const routingInput = page.locator('#csInput');
    const autoApplyBefore = calls.filter(x => x === 'POST /api/routing/apply').length;
    const geoDraft = JSON.stringify({routing:{domainStrategy:'AsIs',rules:[
      {type:'field',domain:['geosite:you'],outboundTag:'direct'}
    ]}},null,2);
    await routingInput.fill(geoDraft);
    await page.evaluate(() => {
      const input=document.querySelector('#csInput');
      const needle='geosite:you';
      const pos=input.value.indexOf(needle)+needle.length;
      input.setSelectionRange(pos,pos);
      input.dispatchEvent(new Event('input',{bubbles:true}));
    });
    await page.waitForSelector('#csGeoAutocomplete .cs-geo-item');
    assert.match(await page.locator('#csGeoAutocomplete').innerText(),/ext:geosite_v2fly\.dat:youtube/);
    await routingInput.press('Tab');
    assert.match(await routingInput.inputValue(),/ext:geosite_v2fly\.dat:youtube/);
    assert.doesNotMatch(await routingInput.inputValue(),/geosite:you"/);
    assert.equal(standardGeoHadExplicitFile,false,'standard geosite autocomplete must scan compatible installed DATs instead of forcing geosite.dat');
    assert.equal(calls.filter(x => x === 'POST /api/routing/apply').length,autoApplyBefore,'editor autocomplete must remain read-only');

    // Normal typing in a new, not-yet-closed JSON string must still trigger suggestions.
    await routingInput.fill('{"routing":{"rules":[{"domain":["geosite:you');
    await page.evaluate(() => {
      const input=document.querySelector('#csInput');
      input.setSelectionRange(input.value.length,input.value.length);
      input.dispatchEvent(new Event('input',{bubbles:true}));
    });
    await page.waitForSelector('#csGeoAutocomplete .cs-geo-item');
    assert.match(await page.locator('#csGeoAutocomplete').innerText(),/ext:geosite_v2fly\.dat:youtube/);
    await routingInput.press('Tab');
    assert.match(await routingInput.inputValue(),/ext:geosite_v2fly\.dat:youtube$/);

    const extDraft = JSON.stringify({routing:{domainStrategy:'AsIs',rules:[
      {type:'field',ip:['ext:geoip.dat:pri'],outboundTag:'direct'}
    ]}},null,2);
    await routingInput.fill(extDraft);
    await page.evaluate(() => {
      const input=document.querySelector('#csInput');
      const needle='ext:geoip.dat:pri';
      const pos=input.value.indexOf(needle)+needle.length;
      input.setSelectionRange(pos,pos);
      input.dispatchEvent(new Event('input',{bubbles:true}));
    });
    await page.waitForSelector('#csGeoAutocomplete .cs-geo-item');
    assert.match(await page.locator('#csGeoAutocomplete').innerText(),/ext:geoip\.dat:private/);
    await routingInput.press('Enter');
    assert.match(await routingInput.inputValue(),/ext:geoip\.dat:private/);
    assert.equal(calls.filter(x => x === 'POST /api/routing/apply').length,autoApplyBefore);

    // Config Studio -> Rules: routing apply pushes the same authoritative live state back into the Rules board.
    await routingInput.fill(JSON.stringify({routing:{domainStrategy:'AsIs',rules:[
      {type:'field',domain:['domain:studio.test'],outboundTag:'direct'}
    ]}},null,2));
    assert.equal(await page.locator('#csApply').isDisabled(),false);
    const routingApplyBefore = calls.filter(x => x === 'POST /api/routing/apply').length;
    await page.locator('#csApply').click();
    await page.waitForFunction(() => (document.querySelector('#csNotice')?.textContent || '').includes('post-validation'),null,{timeout:10000});
    assert.equal(calls.filter(x => x === 'POST /api/routing/apply').length,routingApplyBefore+1);

    await page.locator('.rv2-mode[data-mode="rules"]').click();
    await page.waitForSelector('#rv2RulesPanel',{state:'visible'});
    await page.waitForFunction(() => (document.querySelector('#rv2DirectContent')?.textContent || '').includes('studio.test'));
    assert.match(await page.locator('#rv2DirectContent').innerText(),/studio\.test/);
    assert.doesNotMatch(await page.locator('#rv2DirectContent').innerText(),/sync\.test/);

    // An external live change must never silently overwrite a local Config Studio draft.
    await page.locator('.rv2-mode[data-mode="config"]').click();
    await page.waitForSelector('#csTabsMain',{state:'visible'});
    await page.locator('.cs-tab[data-tab="05_routing"]').click();
    await page.locator('#csInput').fill(JSON.stringify({routing:{domainStrategy:'AsIs',rules:[
      {type:'field',domain:['domain:draft.test'],outboundTag:'direct'}
    ]}},null,2));
    await page.evaluate(() => document.dispatchEvent(new CustomEvent('freenet:xray-config-applied', {
      detail:{source:'rules',files:['05_routing','06_policy']}
    })));
    await page.waitForFunction(() => (document.querySelector('#csNotice')?.textContent || '').includes('Локальный черновик сохранён'));
    assert.match(await page.locator('#csInput').inputValue(),/draft\.test/);
    assert.equal(await page.locator('#csApply').isDisabled(),true,'stale Config Studio draft must be blocked, not overwritten or applied');
    await page.locator('#csReset').click();
    await page.waitForFunction(() => (document.querySelector('#csInput')?.value || '').includes('studio.test'));
    assert.doesNotMatch(await page.locator('#csInput').inputValue(),/draft\.test/);

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
