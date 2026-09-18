const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const path = require('node:path');

const root = path.resolve(__dirname, '..');
const script = path.join(root, 'freenet-ui/web/vpn-selector-reconcile.js');
const expected = '192.0.2.40:443';

(async () => {
  const browser = await chromium.launch({headless:true});
  try {
    const page = await browser.newPage();
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    await page.setContent(`
      <div id="quickActionsSection">
        <div id="exactConnectRow" class="action-row"><button id="exactConnectBtn">Подключиться</button></div>
        <div id="routineRow" class="action-row" hidden><button>Обновить профиль</button></div>
      </div>
      <input id="profileSearch" value="Литва">
      <button id="profilesTrigger" aria-expanded="true"><span id="profilesTriggerText">Литва</span></button>
      <div id="profilesMenu">server list</div>
      <div id="selectedProfileCard"></div>
      <div id="providerNotice"></div>
      <div id="notice"></div>
    `);
    await page.evaluate(() => {
      window.selectedProviderID = 'fixture-lt';
      window.selectedProviderName = 'Литва · Вильнюс';
      window.providerPlanReady = false;
      window.providerApplied = false;
      window.__renderOptions = 0;
      window.__closeMenu = 0;
      window.__baseUpdates = 0;
      window.__pickerOpen = true;
      window.__pickerClosed = 0;
      window.FreeNetVPNPicker = {
        isOpen: () => window.__pickerOpen,
        close: () => { window.__pickerOpen = false; window.__pickerClosed += 1; }
      };
      window.updateStatusViews = function() { window.__baseUpdates += 1; };
      window.renderProfileOptions = function() { window.__renderOptions += 1; };
      window.renderSelectedProfile = function(profile) {
        const card = document.querySelector('#selectedProfileCard');
        if (!profile) {
          card.textContent = '';
          card.hidden = true;
        }
      };
      window.closeProfileMenu = function() { window.__closeMenu += 1; };
      window.hideBox = function(id) { document.getElementById(id).hidden = true; };
    });
    await page.addScriptTag({path:script});

    await page.evaluate(() => updateStatusViews({endpoint:'192.0.2.99:443',xray_online:true,busy:false,updater_busy:false}));
    assert.equal(await page.locator('#profileSearch').inputValue(), 'Литва', 'periodic healthy status must not clear an open popover search');
    assert.equal(await page.locator('#profilesMenu').isVisible(), true, 'periodic healthy status must not collapse the open popover server list');

    async function setPending(title) {
      await page.evaluate(({title, expected}) => {
        const card = document.querySelector('#selectedProfileCard');
        card.hidden = false;
        card.innerHTML = '';
        const strong = document.createElement('strong');
        strong.textContent = title;
        const ep = document.createElement('span');
        ep.className = 'selected-endpoint';
        ep.textContent = expected;
        const note = document.createElement('span');
        note.className = 'selected-note';
        note.textContent = 'Повторное подключение автоматически не запускается.';
        card.append(strong, ep, note);
        document.querySelector('#exactConnectRow').hidden = false;
        document.querySelector('#routineRow').hidden = true;
        document.querySelector('#providerNotice').hidden = false;
        document.querySelector('#notice').hidden = false;
        document.querySelector('#notice').textContent = title === 'Связь прервалась'
          ? 'Связь прервалась во время переключения. Проверяем фактическое состояние перед любым повтором.'
          : 'Фактическое состояние VPN после подключения не подтверждено. Не повторяйте операцию вслепую.';
        window.selectedProviderID = 'fixture-lt';
        window.selectedProviderName = 'Литва · Вильнюс';
        window.providerApplied = false;
      }, {title, expected});
    }

    await setPending('Требуется проверка состояния');
    await page.evaluate(() => updateStatusViews({endpoint:'192.0.2.99:443',xray_online:true,busy:false,updater_busy:false}));
    assert.equal(await page.locator('#selectedProfileCard').isVisible(), true, 'other endpoint must keep warning');
    assert.equal(await page.locator('#exactConnectRow').isVisible(), true, 'other endpoint must keep exact controls');

    await page.evaluate(({expected}) => updateStatusViews({endpoint:expected,xray_online:false,busy:false,updater_busy:false}), {expected});
    assert.equal(await page.locator('#selectedProfileCard').isVisible(), true, 'offline exact endpoint must keep warning');

    await page.evaluate(({expected}) => updateStatusViews({endpoint:expected,xray_online:true,busy:true,updater_busy:false}), {expected});
    assert.equal(await page.locator('#selectedProfileCard').isVisible(), true, 'busy exact endpoint must keep warning');

    await page.evaluate(({expected}) => updateStatusViews({endpoint:expected,xray_online:true,busy:false,updater_busy:false}), {expected});
    assert.equal(await page.locator('#selectedProfileCard').isVisible(), false, 'healthy exact endpoint must dismiss stale warning');
    assert.equal(await page.locator('#exactConnectRow').isVisible(), false, 'healthy exact endpoint must leave exact mode');
    assert.equal(await page.locator('#routineRow').isVisible(), true, 'routine actions must be restored');
    assert.equal(await page.evaluate(() => selectedProviderID), '', 'stale selected profile id must be cleared');
    assert.equal(await page.evaluate(() => providerApplied), true, 'confirmed status marks manual switch accepted');
    assert.equal(await page.evaluate(() => window.__closeMenu), 1, 'selector menu must close');
    assert.equal(await page.evaluate(() => window.__pickerClosed), 1, 'confirmed exact switch must close the stable selector popover');
    assert.equal(await page.locator('#notice').isVisible(), false, 'stale warning notice must close');

    await setPending('Связь прервалась');
    await page.evaluate(({expected}) => updateStatusViews({endpoint:expected,xray_online:true,busy:false,updater_busy:false}), {expected});
    assert.equal(await page.locator('#selectedProfileCard').isVisible(), false, 'late status after interrupted HTTP response must reconcile');

    assert.ok(await page.evaluate(() => window.__baseUpdates >= 5), 'original status renderer must still run');
    assert.equal(errors.length, 0, errors.join('\n'));
    console.log('PASS: delayed manual VPN status reconciles only on the exact healthy endpoint and never masks unknown state.');
  } finally {
    await browser.close();
  }
})().catch(err => { console.error(err); process.exitCode = 1; });
