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
    res.end(JSON.stringify({success:true, online:serviceOnline, version:'26.9.9 (Xray, Penetrates Everything.) 52a412d (go1.27.1 linux/arm64)', events:[]}));
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
      res.writeHead(200, {'content-type':'application/json'});
      res.end(JSON.stringify({success:true, previous_version:'v26.9.9', current_version:applyBody.target_version, target_version:applyBody.target_version, rollback:'NOT_NEEDED', message:`Xray переключён: v26.9.9 → ${applyBody.target_version}.`, events:[]}));
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
        res.writeHead(200, {'content-type':'application/json'});
        res.end(JSON.stringify({success:true,online:true,version:'26.9.9',message:'Xray запущен и работает.',events:[]}));
        return;
      }
      if (payload.action === 'restart') {
        res.writeHead(200, {'content-type':'application/json'});
        res.end(JSON.stringify({success:true,online:true,version:'26.9.9',message:'Xray перезапущен и снова работает.',events:[]}));
        return;
      }
      res.writeHead(400, {'content-type':'application/json'});
      res.end(JSON.stringify({success:false,error:'unsupported action',events:[]}));
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
    await page.waitForFunction(() => document.querySelector('#csServiceVersion')?.textContent.includes('v26.9.9'));
    await page.waitForFunction(() => document.querySelector('#xrayTopbarVersion')?.textContent.includes('v26.9.9'));
    assert.equal((await page.locator('#csServiceVersion').textContent()).trim(), 'v26.9.9 ▾', 'Config Studio compatibility version chip must stay compact');
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

    assert.equal(catalogGets, 0, 'catalog must not load until explicit version click');
    assert.equal(applyPosts, 0, 'page load must not mutate Xray');
    await page.locator('#xrayTopbarChip').click();
    await page.waitForFunction(() => document.querySelector('#xrayCoreManager') && !document.querySelector('#xrayCoreManager').hidden && document.querySelector('#xrayCoreManager').innerText.includes('v26.10.1'));
    assert.equal(catalogGets, 1, 'one click should load catalog once');
    assert.equal(applyPosts, 0, 'catalog load must be read-only');
    const modalText = await page.locator('#xrayCoreManager').innerText();
    assert(modalText.includes('Установлена v26.9.9'));
    assert(modalText.includes('Доступна v26.10.1'));
    assert(modalText.includes('Предрелиз'));
    assert(!modalText.includes('Последний стабильный релиз с исправлениями XTLS.'), 'catalog rows must not expand into release descriptions');
    assert.equal(await page.locator('#xrayTopbarChip').getAttribute('aria-expanded'), 'true', 'Xray chip must expose open dialog state');
    assert.equal(await page.locator('.xcm-current-copy').count(), 0, 'normal Xray manager must not duplicate a technical current-version card');
    assert.equal(await page.getByRole('button', {name:'Обновить до v26.10.1'}).count(), 1, 'latest stable Xray must be immediately actionable');
    assert.equal(await page.locator('.xcm-backdrop').evaluate(el => getComputedStyle(el).display), 'none', 'Xray browse flow must not dim the whole page');
    assert.equal(await page.locator('.xcm-search').count(), 1, 'Xray dropdown must expose compact version search');
    const desktopGeometry = await page.evaluate(() => {
      const modal = document.querySelector('#xrayCoreManager .xcm-modal').getBoundingClientRect();
      const chip = document.querySelector('#xrayTopbarChip').getBoundingClientRect();
      return {width:modal.width,right:modal.right,top:modal.top,chipBottom:chip.bottom,bottom:modal.bottom,viewportWidth:innerWidth,viewportHeight:innerHeight,overflow:document.documentElement.scrollWidth>innerWidth,rootPointer:getComputedStyle(document.querySelector('#xrayCoreManager')).pointerEvents,modalPointer:getComputedStyle(document.querySelector('#xrayCoreManager .xcm-modal')).pointerEvents};
    });
    assert.ok(desktopGeometry.width <= 540.5 && desktopGeometry.right <= desktopGeometry.viewportWidth, 'Xray dropdown must match the VPN-style compact width and stay inside desktop viewport');
    assert.ok(desktopGeometry.top >= 0 && desktopGeometry.bottom <= desktopGeometry.viewportHeight, 'Xray dropdown must stay inside desktop viewport');
    assert.equal(desktopGeometry.rootPointer, 'none', 'Xray dropdown root must not block the page');
    assert.notEqual(desktopGeometry.modalPointer, 'none', 'Xray dropdown panel must remain interactive');
    assert.equal(desktopGeometry.overflow, false, 'Xray panel must not create horizontal overflow');
    const topbarGeometry = await page.locator('#xrayTopbarChip').evaluate(node => ({width:Math.round(node.getBoundingClientRect().width),height:Math.round(node.getBoundingClientRect().height)}));
    assert.ok(topbarGeometry.width >= 145, `Xray topbar control is too small: ${JSON.stringify(topbarGeometry)}`);
    assert.ok(topbarGeometry.height >= 58, `Xray topbar control is too short: ${JSON.stringify(topbarGeometry)}`);
    await page.locator('.xcm-search').fill('v26.8');
    assert.equal(await page.locator('.xcm-release:not([hidden])').count(), 1, 'Xray version search must filter catalog without mutation');
    await page.locator('.xcm-search').fill('');

    await page.locator('.xcm-release[data-version="v26.8.1"]').click();
    assert.equal(applyPosts, 0, 'selecting an older release must not apply it');
    assert.equal((await page.locator('#xrayTopbarVersion').textContent()).trim(), 'v26.9.9', 'selecting a target must not replace current topbar version');
    const downgradeButton = page.getByRole('button', {name:'Откатить до v26.8.1'});
    assert.equal(await downgradeButton.count(), 1, 'older release must be presented as rollback/downgrade');
    await downgradeButton.click();
    await page.waitForFunction(() => document.querySelector('#xrayCoreManager')?.innerText.includes('Xray переключён'));
    assert.equal(applyPosts, 1, 'one explicit version action must issue exactly one apply POST without a second confirmation screen');
    assert.deepEqual(applyBody, {target_version:'v26.8.1'});
    assert.equal((await page.locator('#xrayTopbarVersion').textContent()).trim(), 'v26.8.1', 'successful Xray apply must update the topbar version immediately');

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

    serviceOnline = false;
    const catalogBeforeOfflineRecovery = catalogGets;
    await page.reload();
    await page.waitForFunction(() => document.querySelector('#xrayTopbarVersion')?.textContent.includes('остановлен'));
    assert.equal((await page.locator('#xrayTopbarVersion').textContent()).trim(), 'остановлен', 'topbar must expose stopped Xray truthfully');
    await page.locator('#xrayTopbarChip').click();
    await page.waitForFunction(() => document.querySelector('#xrayCoreManager')?.innerText.includes('Xray остановлен'));
    assert.equal(catalogGets, catalogBeforeOfflineRecovery, 'offline recovery must not browse core catalog before runtime is healthy');
    const recoveryButton = page.locator('#xrayCoreManager').getByRole('button', {name:'Запустить Xray'});
    assert.equal(await recoveryButton.count(), 1, 'offline Xray manager must expose one direct recovery action');
    await recoveryButton.click();
    await page.waitForFunction(() => document.querySelector('#xrayCoreManager')?.innerText.includes('Xray запущен и работает'));
    assert.deepEqual(serviceActions, ['start'], 'topbar recovery must use explicit start, never stop/restart');
    assert.equal((await page.locator('#xrayTopbarVersion').textContent()).trim(), 'v26.9.9', 'successful recovery must restore topbar version');
    assert.equal((await page.locator('#csServiceStatus').textContent()).trim(), 'Работает', 'Config Studio status must reconcile after topbar recovery');
    assert.equal((await page.locator('#csRestartXray').textContent()).trim(), 'Перезапустить', 'recovered service returns to restart action');

    const responsive = await page.evaluate(() => new Promise(resolve => setTimeout(() => resolve('alive'), 20)));
    assert.equal(responsive, 'alive', 'Xray manager UI must not starve browser event loop');
    console.log('Xray Core Manager selector + offline recovery + clean Config Studio UX: OK');
  } finally {
    await browser.close(); server.close();
  }
})().catch(error => { console.error(error); server.close(); process.exit(1); });
