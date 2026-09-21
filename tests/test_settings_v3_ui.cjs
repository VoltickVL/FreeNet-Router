// Compatibility wrapper for the Settings v3 browser contract.
// The production file is now a thin loader that patches the preserved core
// implementation into a single, highlighted top Save action.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const Module = require('node:module');

const corePath = path.join(__dirname, 'test_settings_v3_ui_core.cjs');
let source = fs.readFileSync(corePath, 'utf8');

function replaceOnce(from, to) {
  assert.ok(source.includes(from), `Settings v3 test wrapper could not find expected source fragment: ${from.slice(0, 120)}`);
  source = source.replace(from, to);
}

replaceOnce(
  "'runtime-acceptance.js', 'automation.js', 'automation-async.js', 'settings-v3.js'",
  "'runtime-acceptance.js', 'automation.js', 'automation-async.js', 'settings-v3-core.js', 'settings-v3.js'"
);

source = source.replaceAll('#fn3MaintenanceSave', '#fn3Save');

replaceOnce(
  "    await page.waitForSelector('#fn3AutoEnabled', {state:'visible'});\n",
  "    await page.waitForSelector('#fn3AutoEnabled', {state:'visible'});\n    await page.waitForFunction(() => !document.querySelector('#fn3MaintenanceSave'));\n    assert.equal(await page.locator('#fn3MaintenanceSave').count(), 0, 'Settings must keep a single top Save button');\n"
);

const wrapped = new Module(corePath, module.parent || module);
wrapped.filename = corePath;
wrapped.paths = Module._nodeModulePaths(path.dirname(corePath));
wrapped._compile(source, corePath);
