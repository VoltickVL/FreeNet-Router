'use strict';

const assert = require('assert/strict');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const uxScript = fs.readFileSync(path.join(__dirname, '..', 'freenet-ui', 'web', 'config-studio-ux.js'), 'utf8');
let restartPosts = 0;
let events = [{at:'2026-09-17T10:01:00Z', kind:'xray', result:'success', message:'Предыдущая операция Xray завершена.'}];

const html = `<!doctype html><html><head><meta charset="utf-8"><title>Config Studio UX fixture</title></head><body>
<button data-page="access" id="journalNav">Журнал</button>
<div class="rv2-modebar"><span id="rv2WorkspaceState" class="rv2-state">DRAFT · MUTATION: NONE</span></div>
<section id="configStudioWorkspace" class="cs-shell">
  <div class="cs-title"><h2>Config Studio</h2></div>
  <p class="rv2-copy">Technical copy.</p>
  <div id="csXray">Xray technical chip</div>
  <div class="cs-tab-groups">
    <div id="csTabsMain" class="cs-tabs cs-tabs-main"><button class="cs-tab active" data-tab="01_log">01_log</button><button class="cs-tab" data-tab="02_dns">02_dns</button><button class="cs-tab" data-tab="03_inbounds">03_inbounds</button><button class="cs-tab" data-tab="04_outbounds">04_outbounds</button><button class="cs-tab" data-tab="05_routing">05_routing</button><button class="cs-tab" data-tab="06_policy">06_policy</button></div>
    <div id="csTabsLists" class="cs-tabs cs-tabs-lists"><button class="cs-tab" data-tab="ip_exclude">ip_exclude</button><button class="cs-tab" data-tab="port_exclude">port_exclude</button><button class="cs-tab" data-tab="port_proxying">port_proxying</button></div>
  </div>
  <div class="cs-toolbar"><div class="cs-actions"><button id="csFormat" class="cs-btn">Форматировать</button><button id="csValidate" class="cs-btn">Проверить Xray</button><button id="csReset" class="cs-btn">Сбросить draft</button></div><button id="csApply" class="cs-btn primary">Применить проверенный файл</button></div>
  <div id="csMeta" class="cs-meta"><span>Файл: 06_policy.json</span><span>live · deadbeef</span></div>
  <div id="csApplyNote">Live snapshot: 05_routing deadbeef · 06_policy cafebabe</div>
  <div id="rv2ApplyPreview">Live snapshot: legacy routing preview</div><div id="rv2ApplyResult"></div><button id="rv2ApplyConfig">Применить проверенный candidate</button>
  <span id="csState" class="cs-state readonly">READ ONLY</span>
  <div class="cs-safe-note">ROLLBACK / STOP / MUTATION technical prose</div>
  <div id="csNotice" class="cs-notice"></div>
</section>
<script src="/config-studio-ux.js"></script>
</body></html>`;

const server = http.createServer((req, res) => {
  if (req.url === '/') {
    res.writeHead(200, {'content-type':'text/html; charset=utf-8'});
    res.end(html);
    return;
  }
  if (req.url === '/config-studio-ux.js') {
    res.writeHead(200, {'content-type':'application/javascript; charset=utf-8'});
    res.end(uxScript);
    return;
  }
  if (req.url === '/api/xray/service' && req.method === 'GET') {
    res.writeHead(200, {'content-type':'application/json'});
    res.end(JSON.stringify({success:true, online:true, version:'26.9.9', events}));
    return;
  }
  if (req.url === '/api/xray/service' && req.method === 'POST') {
    restartPosts += 1;
    let body = '';
    req.on('data', chunk => { body += chunk; });
    req.on('end', () => {
      assert.deepEqual(JSON.parse(body), {action:'restart'});
      events = [{at:'2026-09-17T10:02:00Z', kind:'xray', result:'success', message:'Xray перезапущен через FreeNet.'}, ...events];
      res.writeHead(200, {'content-type':'application/json'});
      res.end(JSON.stringify({success:true, online:true, version:'26.9.9', message:'Xray перезапущен и снова работает.', events}));
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
    await page.waitForSelector('#csServiceVersion');
    await page.waitForFunction(() => document.querySelector('#csServiceVersion')?.textContent.includes('26.9.9'));

    const browserAlive = await Promise.race([
      page.evaluate(() => new Promise(resolve => setTimeout(() => resolve('alive'), 60))),
      new Promise(resolve => setTimeout(() => resolve('starved'), 1200))
    ]);
    assert.equal(browserAlive, 'alive', 'Config Studio observer must not starve the browser event loop');

    const visibleText = await page.locator('body').innerText();
    assert(!visibleText.includes('READ ONLY'), 'technical READ ONLY status must not be visible');
    assert(!visibleText.includes('DRAFT'), 'technical DRAFT status must not be visible');
    assert(!visibleText.includes('MUTATION'), 'technical MUTATION status must not be visible');
    assert(visibleText.includes('Конфигурация Xray'));
    const groupLabels = await page.evaluate(() => ({
      main: getComputedStyle(document.querySelector('#csTabsMain'), '::before').content,
      lists: getComputedStyle(document.querySelector('#csTabsLists'), '::before').content
    }));
    assert(groupLabels.main.includes('Конфиги Xray'));
    assert(groupLabels.lists.includes('Списки XKeen'));
    const visual = await page.evaluate(() => {
      const main = document.querySelector('#csTabsMain');
      const lists = document.querySelector('#csTabsLists');
      const mainLabel = getComputedStyle(main, '::before');
      const listLabel = getComputedStyle(lists, '::before');
      const restart = getComputedStyle(document.querySelector('#csRestartXray'));
      const journal = getComputedStyle(document.querySelector('#csToggleJournal'));
      const format = getComputedStyle(document.querySelector('#csFormat'));
      const reset = getComputedStyle(document.querySelector('#csReset'));
      const apply = getComputedStyle(document.querySelector('#csApply'));
      return {
        mainBorder: getComputedStyle(main).borderTopStyle,
        listsBorder: getComputedStyle(lists).borderTopStyle,
        mainLabelColor: mainLabel.color,
        listLabelColor: listLabel.color,
        mainLabelBackground: mainLabel.backgroundImage,
        listLabelBackground: listLabel.backgroundImage,
        restartBackground: restart.backgroundImage,
        journalBackground: journal.backgroundImage,
        formatBackground: format.backgroundImage,
        formatColor: format.color,
        resetBackground: reset.backgroundImage,
        resetColor: reset.color,
        applyBackground: apply.backgroundImage,
        toolbarBorder: getComputedStyle(document.querySelector('.cs-toolbar')).borderTopStyle
      };
    });
    assert.equal(visual.mainBorder, 'none', 'Xray tabs must not sit inside a nested frame');
    assert.equal(visual.listsBorder, 'none', 'XKeen tabs must not sit inside a nested frame');
    assert.notEqual(visual.mainLabelColor, visual.listLabelColor, 'Xray and XKeen group labels need distinct accents');
    assert.match(visual.mainLabelBackground, /gradient/i);
    assert.match(visual.listLabelBackground, /gradient/i);
    assert.equal(visual.restartBackground, 'none', 'Xray operational controls should use the neutral treatment');
    assert.equal(visual.journalBackground, 'none', 'Journal control should use the neutral treatment');
    assert.equal(visual.formatBackground, 'none', 'Format must be a tertiary action, not another blue primary');
    assert.equal(visual.resetBackground, 'none', 'Reset must use its own neutral/warn treatment');
    assert.notEqual(visual.formatColor, visual.resetColor, 'Format and reset need distinct visual meaning');
    assert.match(visual.applyBackground, /gradient/i, 'Save must remain the only primary blue editor action');
    assert.equal(visual.toolbarBorder, 'solid', 'Editor footer actions need a separator below the editor body');
    assert(visibleText.includes('v26.9.9'));
    assert.equal(await page.locator('#csFormat').textContent(), 'Форматировать');
    assert.equal(await page.locator('#csValidate').count(), 0, 'manual validation control must be removed');
    assert.equal(await page.locator('#csReset').textContent(), 'Отменить');
    assert.equal(await page.locator('#csApply').textContent(), 'Сохранить');
    assert.equal(await page.locator('#csMeta').count(), 0, 'file/hash metadata must be removed from normal UI');
    assert.equal(await page.locator('#csApplyNote').count(), 0, 'Live snapshot note must be removed');
    assert.equal(await page.locator('#rv2ApplyPreview').count(), 0, 'legacy routing preview must be removed');
    assert.equal(await page.locator('#rv2ApplyConfig').count(), 0, 'legacy manual-validation apply control must be removed');
    const cleanText = await page.locator('body').innerText();
    assert(!cleanText.includes('Live snapshot'));
    assert(!cleanText.includes('Файл: 06_policy.json'));

    await page.evaluate(() => {
      document.querySelectorAll('.cs-tab').forEach(node => node.classList.remove('active'));
      const tab = document.querySelector('[data-tab="ip_exclude"]');
      tab.classList.add('active');
      tab.click();
    });
    await page.waitForFunction(() => document.querySelector('.cs-shell')?.classList.contains('cs-list-view'));

    await page.locator('#csToggleJournal').click();
    await page.waitForFunction(() => document.querySelector('#csServiceJournal')?.classList.contains('show'));
    assert((await page.locator('#csServiceJournal').innerText()).includes('Предыдущая операция Xray завершена.'));

    await page.locator('#csRestartXray').click();
    await page.waitForFunction(() => document.querySelector('#csNotice')?.textContent.includes('снова работает'));
    assert.equal(restartPosts, 1, 'one explicit click must issue exactly one restart POST');
    assert((await page.locator('#csServiceStatus').innerText()).includes('Работает'));
    assert((await page.locator('#csServiceJournal').innerText()).includes('Xray перезапущен через FreeNet.'));

    const afterMutationAlive = await Promise.race([
      page.evaluate(() => new Promise(resolve => {
        const probe = document.createElement('div');
        probe.textContent = 'observer-probe';
        document.body.appendChild(probe);
        setTimeout(() => resolve('alive'), 60);
      })),
      new Promise(resolve => setTimeout(() => resolve('starved'), 1200))
    ]);
    assert.equal(afterMutationAlive, 'alive', 'unrelated DOM mutation must not trigger a runaway observer loop');
    assert.equal(restartPosts, 1, 'observer/rerender must not repeat restart automatically');
    console.log('Config Studio simplified UX + responsive observer + Xray restart journal: OK');
  } finally {
    await browser.close();
    server.close();
  }
})().catch(error => {
  console.error(error);
  server.close();
  process.exit(1);
});
