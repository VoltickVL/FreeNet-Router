'use strict';

const assert = require('assert/strict');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const uxScript = fs.readFileSync(path.join(__dirname, '..', 'freenet-ui', 'web', 'config-studio-ux.js'), 'utf8');
const managerScript = fs.readFileSync(path.join(__dirname, '..', 'freenet-ui', 'web', 'xray-core-manager.js'), 'utf8');
let catalogGets = 0;
let applyPosts = 0;
let applyBody = null;
let serviceOnline = true;
let serviceVersion = 'v26.9.9';
let serviceEvents = [
  {at:'2026-10-04T00:01:00Z',kind:'xray',result:'success',message:'Xray запущен через FreeNet.'}
];
const serviceActions = [];

const html = `<!doctype html><html><head><meta charset="utf-8"><title>Xray Core Manager fixture</title>
<style>
.cs-notice{display:block}.cs-notice.show{display:block}.cs-notice.bad{display:block}
</style></head><body>
<header class="topbar overview-approved"><div id="overviewApprovedTop" class="overview-approved-top fn-render-facts"><div id="dnsFact">DNS</div><div id="freenetFact">FreeNet</div></div><div class="top-actions"></div></header>
<button data-page="access" id="journalNav">Журнал</button>
<div class="rv2-modebar"><span id="rv2WorkspaceState" class="rv2-state">DRAFT · MUTATION: NONE</span></div>
<section id="configStudioWorkspace" class="cs-shell">
  <div class="cs-title"><h2>Config Studio</h2></div><p class="rv2-copy">Technical copy.</p><div id="csXray">technical xray</div>
  <div class="cs-tab-groups"><div id="csTabsMain" class="cs-tabs cs-tabs-main"><button class="cs-tab active" data-tab="01_log">01_log</button></div><div id="csTabsLists" class="cs-tabs cs-tabs-lists"><button class="cs-tab" data-tab="ip_exclude">ip_exclude</button></div></div>
  <div class="cs-toolbar"><button id="csFormat" class="cs-btn">Форматировать</button><button id="csValidate" class="cs-btn">Проверить Xray</button><button id="csReset" class="cs-btn">Сбросить draft</button><button id="csApply" class="cs-btn primary">Применить проверенный файл</button></div>
  <div class="cs-meta"><span>Файл: 01_log.json</span><span>hash</span></div>
  <span id="csState">READ ONLY</span><div class="cs-safe-note">ROLLBACK STOP</div>
  <div id="csNotice" class="cs-notice show ok">Candidate прошёл xray run -test -confdir. Live config не изменён. MUTATION: NONE</div>
  <div id="csApplyNote">Live snapshot: 05_routing deadbeef · 06_policy cafebabe<br>Чтобы применить изменения, сначала выполните Проверить Xray.</div>
</section>
<script src="/config-studio-ux.js"></script><script src="/xray-core-manager.js"></script>
</body></html>`;

const catalog = {
  success: true,
  current_version: 'v26.9.9',
  latest_version: 'v26.10.1',
  architecture: 'arm64',
  asset_name: 'Xray-linux-arm64-v8a.zip',
  releases: [
    {version:'v26.10.1', published_at:'2026-09-15T00:00:00Z', prerelease:false, current:false, latest:true, description:'Последний стабильный релиз с исправлениями XTLS.', asset:{available:true}},
    {version:'v26.9.9', published_at:'2026-09-09T00:00:00Z', prerelease:false, current:true, latest:false, description:'Установленная версия.', asset:{available:true}},
    {version:'v26.8.1', published_at:'2026-08-01T00:00:00Z', prerelease:false, current:false, latest:false, description:'Предыдущая стабильная версия для отката.', asset:{available:true}},
    {version:'v26.11.0-beta.1', published_at:'2026-09-16T00:00:00Z', prerelease:true, current:false, latest:false, description:'Предварительная версия.', asset:{available:true}}
  ]
};

const server = http.createServer((req, res) => {
  if (req.url === '/') { res.writeHead(200, {'content-type':'text/html; charset=utf-8'}); res.end(html); return; }
  if (req.url === '/config-studio-ux.js') { res.writeHead(200, {'content-type':'application/javascript'}); res.end(uxScript); return; }
  if (req.url === '/xray-core-manager.js') { res.writeHead(200, {'content-type':'application/javascript'}); res.end(managerScript); return; }
  if (req.url === '/api/xray/service' && req.method === 'GET') {
    res.writeHead(200, {'content-type':'application/json'});
    res.end(JSON.stringify({success:true, online:serviceOnline, version:`${serviceVersion} (Xray, Penetrates Everything.) 52a412d (go1.27.1 linux/arm64)`, events:serviceEvents}));
    return;
  }
  if (req.url === '/api/xray/core/catalog' && req.method === 'GET') {
    catalogGets += 1; res.writeHead(200, {'content-type':'application/json'}); res.end(JSON.stringify(catalog)); return;
  }
  if (req.url === '/api/xray/core/apply' && req.method === 'POST') {
    applyPosts += 1; let raw = '';
    req.on('data', chunk => { raw += chunk; });
    req.on('end', () => {
      applyBody = JSON.parse(raw);
      const previous = serviceVersion;
      serviceVersion = applyBody.target_version;
      serviceEvents = [{at:'2026-10-04T00:04:00Z',kind:'xray',result:'success',message:`Xray переключён: ${previous} → ${serviceVersion}.`}, ...serviceEvents];
      res.writeHead(200, {'content-type':'application/json'});
      res.end(JSON.stringify({success:true, previous_version:previous, current_version:serviceVersion, target_version:serviceVersion, rollback:'NOT_NEEDED', message:`Xray переключён: ${previous} → ${serviceVersion}.`, events:serviceEvents}));
    });
    return;
  }
  if (req.url === '/api/xray/service' && req.method === 'POST') {
    let raw = '';
    req.on('data', chunk => { raw += chunk; });
    req.on('end', () => {
      const payload = JSON.parse(raw);
      serviceActions.push(payload.action);
      if (payload.action === 'start') {
        serviceOnline = true;
        serviceEvents = [{at:'2026-10-04T00:03:00Z',kind:'xray',result:'success',message:'Xray запущен через FreeNet.'}, ...serviceEvents];
        res.writeHead(200, {'content-type':'application/json'});
        res.end(JSON.stringify({success:true,online:true,version:serviceVersion,message:'Xray запущен и работает.',events:serviceEvents}));
        return;
      }
      if (payload.action === 'stop') {
        serviceOnline = false;
        serviceEvents = [{at:'2026-10-04T00:02:00Z',kind:'xray',result:'success',message:'Xray остановлен через FreeNet.'}, ...serviceEvents];
        res.writeHead(200, {'content-type':'application/json'});
        res.end(JSON.stringify({success:true,online:false,version:serviceVersion,message:'Xray остановлен.',events:serviceEvents}));
        return;
      }
      if (payload.action === 'restart') {
        serviceOnline = true;
        serviceEvents = [{at:'2026-10-04T00:01:30Z',kind:'xray',result:'success',message:'Xray перезапущен через FreeNet.'}, ...serviceEvents];
        res.writeHead(200, {'content-type':'application/json'});
        res.end(JSON.stringify({success:true,online:true,version:serviceVersion,message:'Xray перезапущен и снова работает.',events:serviceEvents}));
        return;
      }
      res.writeHead(400, {'content-type':'application/json'});
      res.end(JSON.stringify({success:false,error:'unsupported action',events:serviceEvents}));
    });
    return;
  }
  res.writeHead(404); res.end('not found');
});

(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const port = server.address().port;
  const browser = await chromium.launch({headless:true});
  const page = await browser.newPage();
  try {
    await page.goto(`http://127.0.0.1:${port}/`);
    await page.waitForFunction(() => document.querySelector('#xrayTopbarVersion')?.textContent.includes('v26.9.9'));
    assert.equal(await page.locator('#csServiceVersion').count(), 0, 'version has one canonical owner, not a Config Studio duplicate');
    assert.equal((await page.locator('#xrayTopbarVersion').textContent()).trim(), 'v26.9.9', 'topbar Xray version must be compact');
    assert.equal(await page.locator('#overviewApprovedTop > :first-child').getAttribute('id'), 'xrayTopbarChip', 'Xray must appear before DNS / FreeNet facts in the topbar');
    assert.equal(await page.locator('#csValidate').count(), 0, 'manual validation control must be removed');
    assert.equal(await page.locator('#csMeta').count(), 0, 'file/hash metadata must be removed');
    assert.equal(await page.locator('#csApplyNote').count(), 0, 'Live snapshot technical panel must be removed');
    const cleanText = await page.locator('body').innerText();
    assert(!cleanText.includes('Live snapshot'));
    assert.equal(await page.locator('#csNotice').evaluate(el => getComputedStyle(el).display), 'none', 'technical successful validation panel must be hidden');
    await page.locator('#csNotice').evaluate(el => { el.className = 'cs-notice show bad'; el.textContent = 'Ошибка JSON: comma expected, строка 20'; });
    assert.notEqual(await page.locator('#csNotice').evaluate(el => getComputedStyle(el).display), 'none', 'real error diagnostics must remain visible');

    assert.equal(catalogGets, 0, 'catalog must not load until explicit Versions action');
    assert.equal(applyPosts, 0, 'page load must not mutate Xray');
    assert.deepEqual(serviceActions, [], 'page load must not control Xray');
    assert.equal(await page.locator('#csRestartXray').count(), 0, 'Config Studio must not duplicate Xray control');

    await page.evaluate(() => {
      window.__xrayJournalClicks = 0;
      document.querySelector('#journalNav')?.addEventListener('click', () => { window.__xrayJournalClicks += 1; });
    });

    // The topbar invokes the canonical Xray surface; Config Studio has none.
    await page.locator('#xrayTopbarChip').click();
    await page.waitForFunction(() => {
      const root = document.querySelector('#xrayCoreManager');
      const text = root?.innerText || '';
      return root && !root.hidden && text.includes('Работает') && text.includes('Перезапустить') && text.includes('Остановить') && text.includes('Обновить');
    });
    assert.equal(catalogGets, 0, 'opening Xray control must not fetch the version catalog');
    assert.deepEqual(serviceActions, [], 'opening Xray control must stay read-only');
    assert.match(await page.locator('#xrayCoreManager').innerText(),/последние события/i);
    assert.match(await page.locator('#xrayCoreManager').innerText(),/Xray запущен через FreeNet/);
    assert.equal(await page.locator('#xrayTopbarChip').getAttribute('aria-expanded'), 'true', 'Xray chip must expose open dialog state');
    assert.equal(await page.locator('.xcm-backdrop').evaluate(el => getComputedStyle(el).display), 'none', 'Xray control must not dim the whole page');

    const desktopGeometry = await page.evaluate(() => {
      const modal = document.querySelector('#xrayCoreManager .xcm-modal').getBoundingClientRect();
      const chip = document.querySelector('#xrayTopbarChip').getBoundingClientRect();
      return {width:modal.width,right:modal.right,top:modal.top,chipBottom:chip.bottom,bottom:modal.bottom,viewportWidth:innerWidth,viewportHeight:innerHeight,overflow:document.documentElement.scrollWidth>innerWidth,rootPointer:getComputedStyle(document.querySelector('#xrayCoreManager')).pointerEvents,modalPointer:getComputedStyle(document.querySelector('#xrayCoreManager .xcm-modal')).pointerEvents};
    });
    assert.ok(desktopGeometry.width <= 540.5 && desktopGeometry.right <= desktopGeometry.viewportWidth, 'Xray control must stay inside desktop viewport');
    assert.ok(desktopGeometry.top >= 0 && desktopGeometry.bottom <= desktopGeometry.viewportHeight, 'Xray control must stay inside desktop viewport vertically');
    assert.equal(desktopGeometry.rootPointer, 'none', 'Xray control root must not block the page');
    assert.notEqual(desktopGeometry.modalPointer, 'none', 'Xray control panel must remain interactive');
    assert.equal(desktopGeometry.overflow, false, 'Xray control must not create horizontal overflow');
    const topbarGeometry = await page.locator('#xrayTopbarChip').evaluate(node => ({width:Math.round(node.getBoundingClientRect().width),height:Math.round(node.getBoundingClientRect().height)}));
    assert.ok(topbarGeometry.width >= 145, `Xray topbar control is too small: ${JSON.stringify(topbarGeometry)}`);
    assert.ok(topbarGeometry.height >= 58, `Xray topbar control is too short: ${JSON.stringify(topbarGeometry)}`);

    // Running -> Restart -> running, through the shared backend owner.
    const manager = page.locator('#xrayCoreManager');
    await manager.getByRole('button', {name:'Перезапустить'}).click();
    await page.waitForFunction(() => (document.querySelector('#xrayCoreManager')?.innerText || '').includes('Xray перезапущен и снова работает'));
    assert.deepEqual(serviceActions, ['restart']);

    // Running -> Stop must truthfully reconcile every surface and hide version mutation while stopped.
    await manager.getByRole('button', {name:'Остановить'}).click();
    await page.waitForFunction(() => document.querySelector('#xrayTopbarVersion')?.textContent.trim() === 'остановлен');
    await page.waitForFunction(() => (document.querySelector('#xrayCoreManager')?.innerText || '').includes('Остановлен'));
    assert.deepEqual(serviceActions, ['restart','stop']);
    assert.equal(await manager.getByRole('button', {name:'Обновить'}).count(), 0, 'stopped Xray must not expose a version mutation that could start it implicitly');
    assert.equal(catalogGets, 0, 'Stop must not fetch version catalog');

    // Stopped -> Start must return to running without a separate control path.
    await manager.getByRole('button', {name:'Запустить Xray'}).click();
    await page.waitForFunction(() => (document.querySelector('#xrayCoreManager')?.innerText || '').includes('Xray запущен и работает'));
    await page.waitForFunction(() => document.querySelector('#xrayTopbarVersion')?.textContent.trim() === 'v26.9.9');
    assert.deepEqual(serviceActions, ['restart','stop','start']);

    // Journal is part of the same surface and navigates to the existing full Journal.
    await manager.getByRole('button', {name:'Журнал'}).click();
    await page.waitForFunction(() => document.querySelector('#xrayCoreManager')?.hidden === true);
    assert.equal(await page.evaluate(() => window.__xrayJournalClicks), 1, 'Xray Journal action must use the existing full Journal');

    // Version management remains explicit and lazy-loaded from the same surface.
    await page.locator('#xrayTopbarChip').click();
    await page.waitForFunction(() => document.querySelector('#xrayCoreManager') && !document.querySelector('#xrayCoreManager').hidden);
    await manager.getByRole('button', {name:'Обновить'}).click();
    await page.waitForFunction(() => (document.querySelector('#xrayCoreManager')?.innerText || '').includes('v26.10.1'));
    assert.equal(catalogGets, 1, 'explicit Versions action should load catalog once');
    assert.equal(applyPosts, 0, 'catalog load must be read-only');
    const modalText = await manager.innerText();
    assert(modalText.includes('Установлена v26.9.9'));
    assert(modalText.includes('Доступна v26.10.1'));
    assert(modalText.includes('Предрелиз'));
    assert(!modalText.includes('Последний стабильный релиз с исправлениями XTLS.'), 'catalog rows must stay compact');
    assert.equal(await page.getByRole('button', {name:'Обновить до v26.10.1'}).count(), 1, 'latest stable Xray must remain actionable');
    assert.equal(await page.locator('.xcm-search').count(), 1, 'Versions view must expose compact search');
    await page.locator('.xcm-search').fill('v26.8');
    assert.equal(await page.locator('.xcm-release:not([hidden])').count(), 1, 'Xray version search must filter catalog without mutation');
    await page.locator('.xcm-search').fill('');

    await page.locator('.xcm-release[data-version="v26.8.1"]').click();
    assert.equal(applyPosts, 0, 'selecting an older release must not apply it');
    assert.equal((await page.locator('#xrayTopbarVersion').textContent()).trim(), 'v26.9.9', 'selecting a target must not replace current topbar version');
    const downgradeButton = manager.getByRole('button', {name:'Откатить до v26.8.1'});
    assert.equal(await downgradeButton.count(), 1, 'older release must be presented as rollback/downgrade');
    await downgradeButton.click();
    await page.waitForFunction(() => (document.querySelector('#xrayCoreManager')?.innerText || '').includes('Xray переключён'));
    assert.equal(applyPosts, 1, 'one explicit version action must issue exactly one apply POST');
    assert.deepEqual(applyBody, {target_version:'v26.8.1'});
    assert.equal((await page.locator('#xrayTopbarVersion').textContent()).trim(), 'v26.8.1', 'successful Xray apply must update topbar version immediately');
    await page.waitForFunction(() => (document.querySelector('#xrayCoreManager')?.innerText || '').includes('v26.8.1') && (document.querySelector('#xrayCoreManager')?.innerText || '').includes('Работает'));
    assert.match(await manager.innerText(),/Xray переключён: v26\.9\.9 → v26\.8\.1/);

    await page.locator('#xcmClose').click();
    await page.waitForFunction(() => document.querySelector('#xrayCoreManager')?.hidden === true);
    assert.equal(await page.locator('#xrayTopbarChip').getAttribute('aria-expanded'), 'false', 'closing Xray panel must reset chip state');
    await page.locator('#xrayTopbarChip').click();
    await page.waitForFunction(() => document.querySelector('#xrayCoreManager')?.hidden === false);
    await page.keyboard.press('Escape');
    await page.waitForFunction(() => document.querySelector('#xrayCoreManager')?.hidden === true);
    assert.equal(await page.locator('#xrayTopbarChip').evaluate(el => document.activeElement === el), true, 'Escape must return focus to Xray chip');

    await page.setViewportSize({width:390,height:844});
    await page.locator('#xrayTopbarChip').click();
    await page.waitForFunction(() => document.querySelector('#xrayCoreManager')?.hidden === false);
    const mobileGeometry = await page.evaluate(() => ({overflow:document.documentElement.scrollWidth>innerWidth,modal:document.querySelector('#xrayCoreManager .xcm-modal').getBoundingClientRect().width,viewport:innerWidth}));
    assert.equal(mobileGeometry.overflow, false, 'mobile Xray panel must not create horizontal overflow');
    assert.ok(mobileGeometry.modal <= mobileGeometry.viewport - 20, 'mobile Xray panel must fit viewport');
    await page.keyboard.press('Escape');

    const responsive = await page.evaluate(() => new Promise(resolve => setTimeout(() => resolve('alive'), 20)));
    assert.equal(responsive, 'alive', 'Xray manager UI must not starve browser event loop');
    console.log('Unified Xray Control Surface + service lifecycle + versions + Journal: OK');
  } finally {
    await browser.close(); server.close();
  }
})().catch(error => { console.error(error); server.close(); process.exit(1); });
