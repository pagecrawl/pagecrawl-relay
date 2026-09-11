// Optional browser regression: node --test settings.browser.test.cjs
// Requires Playwright and its Chromium browser; no live PageCrawl account is used.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { execFileSync, spawn } = require('node:child_process');
const { mkdtempSync, rmSync } = require('node:fs');
const { join } = require('node:path');
const { tmpdir } = require('node:os');
const { chromium } = require('playwright');

test('settings API works under CSP and preserves authorization across reloads', { timeout: 60000 }, async t => {
  const temp = mkdtempSync(join(tmpdir(), 'relay-browser-'));
  t.after(() => rmSync(temp, { recursive: true, force: true }));
  const binary = join(temp, 'relay');
  execFileSync('go', ['build', '-o', binary, '.'], { cwd: __dirname, stdio: 'pipe' });
  const relay = spawn(binary, ['-token', 'a'.repeat(64), '-gateway', 'ws://127.0.0.1:1/tunnel'], {
    env: { ...process.env, HOME: temp, XDG_CONFIG_HOME: temp, PAGECRAWL_RELAY_TOKEN: '' },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  t.after(async () => {
    if (relay.exitCode !== null) return;
    relay.kill('SIGTERM');
    await new Promise(resolve => relay.once('exit', resolve));
  });
  const url = await new Promise((resolve, reject) => {
    let output = '';
    const timer = setTimeout(() => reject(new Error('Settings server did not start')), 10000);
    relay.once('error', err => { clearTimeout(timer); reject(err); });
    relay.once('exit', code => { clearTimeout(timer); reject(new Error(`Relay exited ${code}`)); });
    relay.stdout.on('data', data => {
      output += data;
      const match = output.match(/Settings: (http:\/\/[^\s]+)/);
      if (match) { clearTimeout(timer); resolve(match[1]); }
    });
  });
  const browser = await chromium.launch({ headless: true });
  t.after(() => browser.close());
  const page = await browser.newPage();
  await page.addInitScript(() => {
    window.cspViolations = [];
    document.addEventListener('securitypolicyviolation', event => window.cspViolations.push(event.violatedDirective));
  });
  const response = await page.goto(url);
  assert.match(response.headers()['content-security-policy'], /connect-src 'self'/);
  await page.locator('#status').waitFor({ state: 'visible' });
  assert.equal(new URL(page.url()).search, '');
  await page.reload();
  await page.locator('#status').waitFor({ state: 'visible' });
  await page.locator('#pause').click();
  await page.waitForFunction(() => document.getElementById('pause').textContent === 'Resume');

  page.on('dialog', dialog => dialog.accept());
  await page.locator('#forget').click();
  await page.locator('#setup').waitFor({ state: 'visible' });
  await page.locator('#token').fill('b'.repeat(64));
  await page.locator('#save').click();
  await page.locator('#status').waitFor({ state: 'visible' });
  assert.equal(await page.locator('#token').inputValue(), '');

  const hostile = '<img src=x onerror="window.untrustedMarkupExecuted=true">';
  await page.route('**/api/check', route => route.fulfill({ json: {
    checks: [{ name: hostile, detail: hostile, fix: hostile, ok: false }],
  } }));
  await page.locator('#runCheck').click();
  await page.waitForFunction(() => document.getElementById('checkList').textContent.includes('<img'));
  assert.equal(await page.locator('#checkList img').count(), 0);
  assert.equal(await page.evaluate(() => window.untrustedMarkupExecuted), undefined);
  assert.deepEqual(await page.evaluate(() => window.cspViolations), []);

  await page.unroute('**/api/check');
  await page.route('**/api/check', route => route.fulfill({ status: 403, body: 'forbidden' }));
  await page.locator('#runCheck').click();
  await page.waitForFunction(() => document.getElementById('checkList').textContent.includes('no longer authorised'));
  assert.equal(await page.locator('#runCheck').isDisabled(), false);
});
