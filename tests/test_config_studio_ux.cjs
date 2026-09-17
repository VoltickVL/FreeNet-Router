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
    assert(visibleText.includes('v26.9.9'));
    assert.equal(await page.locator('#csFormat').textContent(), 'Формат');
    assert.equal(await page.locator('#csValidate').textContent(), 'Проверить');
    assert.equal(await page.locator('#csReset').textContent(), 'Отменить');
    assert.equal(await page.locator('#csApply').textContent(), 'Сохранить');

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
