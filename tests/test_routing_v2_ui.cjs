// Browser regression for the production Routing v2 canonical mount/apply lifecycle.
// Uses documentation-only fixtures; no router or live Xray is contacted.
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');

const root = path.resolve(__dirname, '..');
const web = path.join(root, 'freenet-ui', 'web');
const calls = [];
let routingLive = {routing:{domainStrategy:'AsIs',rules:[]}};
let policyLive = {policy:{}};

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

const status = {
  version:'0.3.68',country:'Германия',city:'Франкфурт-на-Майне',country_code:'de',
  profile_label:'DE Франкфурт-на-Майне, Германия, Extra',endpoint:'192.0.2.67:443',
  xray_online:true,xkeen_ui_online:true,dns_out_present:true,dns_mode:'xkeen',isp:'custom',isp_label:'Свой',
  recommended_dns_mode:'xkeen',setup_complete:true,subscription_configured:true,busy:false,updater_busy:false,last_action:{success:true}
};

// automation.js intentionally runs before the canonical Routing v2 source here:
// this reproduces the real production race that used to rename network -> routing
// before Routing v2's DOMContentLoaded mount listener ran.
const scripts = ['vpn-ux-fix.js','automation.js','routing-v2-canonical.js','routing-apply-ui.js'];
const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://localhost');
  calls.push(`${req.method} ${url.pathname}${url.search}`);

  if (url.pathname === '/') {
    const html = fs.readFileSync(path.join(web, 'index.html'), 'utf8')
      .replace('</body>', scripts.map(name => `<script src="/${name}"></script>`).join('') + '</body>');
    res.writeHead(200, {'Content-Type':'text/html; charset=utf-8'});
    res.end(html);
    return;
  }
  if (url.pathname === '/routing-v2-canonical.js') {
    res.writeHead(200, {'Content-Type':'application/javascript; charset=utf-8','Cache-Control':'no-store'});
    res.end(canonicalRoutingV2Source());
    return;
  }
  if (scripts.some(name => name !== 'routing-v2-canonical.js' && url.pathname === `/${name}`)) {
    res.writeHead(200, {'Content-Type':'application/javascript; charset=utf-8','Cache-Control':'no-store'});
    res.end(fs.readFileSync(path.join(web, url.pathname.slice(1)), 'utf8'));
    return;
  }
  if (url.pathname === '/api/auth/status') return json(res, {configured:true,authenticated:true});
  if (url.pathname === '/api/status') return json(res, status);
  if (url.pathname === '/api/network-profile/plan') return json(res, {success:true,supported:true,active:true,extra_profiles:[]});
  if (url.pathname === '/api/geodata/files') return json(res, {success:true,files:[],search_enabled:false});
  if (url.pathname === '/api/geodata/search') return json(res, {success:false,matches:[],error:'GeoData search temporarily disabled for memory safety'}, 503);
  if (url.pathname === '/api/capabilities') return json(res, {success:true,split_dns_supported:true,memory_total_mib:1024,split_dns_min_mib:768});
  if (url.pathname === '/api/routing/config') {
    return json(res, {
      success:true,mutation:'NONE',routing:routingLive,policy:policyLive,routing_present:true,policy_present:true,
      routing_sha256:'1111111111111111111111111111111111111111111111111111111111111111',
      policy_sha256:'2222222222222222222222222222222222222222222222222222222222222222'
    });
  }
  if (url.pathname === '/api/routing/validate' && req.method === 'POST') {
    const candidate = await bodyJSON(req);
    return json(res, {success:true,mutation:'NONE',xray_valid:true,routing:candidate.routing,policy:candidate.policy});
  }
  if (url.pathname === '/api/routing/apply' && req.method === 'POST') {
    const candidate = await bodyJSON(req);
    routingLive = candidate.routing;
    policyLive = candidate.policy;
    return json(res, {
      success:true,mutation:'APPLIED',xray_valid:true,applied:true,rollback:'NOT_NEEDED',
      before:{'05_routing.json':'1111','06_policy.json':'2222'},after:{'05_routing.json':'3333','06_policy.json':'2222'},
      result:'routing policy applied to managed sections'
    });
  }
  if (url.pathname === '/api/subscription') return json(res, {success:true,configured:true});
  return json(res, {success:true});
});

(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch({headless:true});
  try {
    const page = await browser.newPage({viewport:{width:1600,height:1000}});
    const errors = [];
    const consoleErrors = [];
    page.on('pageerror', error => { errors.push(error.message); console.error('ROUTING_PAGEERROR', error.message); });
    page.on('console', msg => { if (msg.type() === 'error') { consoleErrors.push(msg.text()); console.error('ROUTING_CONSOLE_ERROR', msg.text()); } });
    page.on('requestfailed', req => {
      const item = `requestfailed ${req.method()} ${req.url()} ${req.failure()?.errorText || ''}`;
      consoleErrors.push(item); console.error('ROUTING_REQUEST_FAILED', item);
    });
    page.on('dialog', dialog => dialog.accept());

    const base = `http://127.0.0.1:${server.address().port}`;
    await page.goto(`${base}/#routing`);
    try {
      await page.waitForSelector('#routingV2Workspace', {state:'visible', timeout:10000});
    } catch (error) {
      const debug = await page.evaluate(() => ({
        readyState: document.readyState,
        hash: location.hash,
        pageViews: [...document.querySelectorAll('[data-page-view]')].map(n => ({view:n.dataset.pageView,active:n.classList.contains('active'),routingV2:n.dataset.routingV2 || ''})),
        routingLoaded: !!window.__freenetRoutingApplyUILoaded,
        workspaceCount: document.querySelectorAll('#routingV2Workspace').length,
        oldPreviewCount: document.querySelectorAll('#policyBuilderPreview').length,
        routingScripts: [...document.scripts].map(s => s.src).filter(src => src.includes('routing-v2')),
        bodyText: (document.body?.innerText || '').slice(0,1200)
      }));
      console.error('ROUTING_MOUNT_DEBUG', JSON.stringify(debug));
      console.error('ROUTING_CALLS_DEBUG', JSON.stringify(calls));
      console.error('ROUTING_ERRORS_DEBUG', JSON.stringify(errors));
      console.error('ROUTING_CONSOLE_DEBUG', JSON.stringify(consoleErrors));
      throw error;
    }
    await page.waitForFunction(() => document.querySelector('[data-page-view="routing"]')?.classList.contains('active'));

    const mounted = await page.evaluate(() => ({
      route: document.querySelector('#routingV2Workspace')?.closest('[data-page-view]')?.dataset.pageView || '',
      oldPreview: document.querySelectorAll('#policyBuilderPreview').length,
      workspace: document.querySelectorAll('#routingV2Workspace').length,
      header: document.querySelector('[data-page-view="routing"] .page-head')?.textContent || '',
      applyCount: document.querySelectorAll('#rv2ApplyConfig').length
    }));
    assert.equal(mounted.route, 'routing', JSON.stringify(mounted));
    assert.equal(mounted.oldPreview, 0, `legacy preview survived canonical Routing v2 mount: ${JSON.stringify(mounted)}`);
    assert.equal(mounted.workspace, 1, `Routing v2 workspace missing/duplicated: ${JSON.stringify(mounted)}`);
    assert.match(mounted.header, /Config Studio/, `canonical Routing v2 header was overwritten: ${JSON.stringify(mounted)}`);
    assert.equal(mounted.applyCount, 1, `controlled Apply action missing: ${JSON.stringify(mounted)}`);

    await page.locator('.rv2-mode[data-mode="config"]').click();
    await page.waitForSelector('#rv2ConfigEditor', {state:'visible'});
    await page.waitForFunction(() => document.querySelector('#rv2ConfigState')?.textContent.includes('Live'));

    const candidate = {
      routing:{domainStrategy:'AsIs',rules:[{type:'field',domain:['domain:example.test'],outboundTag:'direct'}]}
    };
    await page.locator('#rv2ConfigEditor').fill(JSON.stringify(candidate, null, 2));
    await page.locator('#rv2ValidateConfig').click();
    await page.waitForFunction(() => !document.querySelector('#rv2ApplyConfig')?.disabled);

    const preview = await page.locator('#rv2ApplyPreview').textContent();
    assert.match(preview, /05_routing\.json: будет изменён/);
    assert.match(preview, /06_policy\.json: без изменений/);

    const postsBefore = calls.filter(call => call === 'POST /api/routing/apply').length;
    await page.locator('#rv2ApplyConfig').click();
    await page.waitForSelector('#routingV2Workspace', {state:'visible'});
    await page.waitForFunction(() => (document.querySelector('#rv2ApplyResult')?.textContent || '').includes('APPLIED'));
    const postsAfter = calls.filter(call => call === 'POST /api/routing/apply').length;
    assert.equal(postsAfter, postsBefore + 1, 'controlled apply must issue exactly one routing mutation');

    const result = await page.locator('#rv2ApplyResult').textContent();
    assert.match(result, /Результат: APPLIED/);
    assert.match(result, /Откат: NOT_NEEDED/);
    assert.equal(errors.length, 0, errors.join('\n'));
    assert.equal(consoleErrors.length, 0, consoleErrors.join('\n'));

    console.log('ROUTING_V2_RUNTIME', JSON.stringify(mounted));
    console.log('ROUTING_V2_APPLY_CALLS', postsAfter);
  } finally {
    await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
})().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
