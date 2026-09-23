'use strict';
/* Task 56 — mobile dual-browser QA (Playwright device emulation)
 * Flows B (search -> detail -> quick consume -> record) and C (recharge -> balance -> ledger -> balance consume)
 * on iPhone (Safari-ish UA) + Android Chrome, plus 375px layout checks and malformed-input probes.
 * Artifacts (screenshots + traces + JSON report) -> .omo/evidence/task-56-go-hair-salon-mvp/
 *
 * Usage (manual QA harness, NOT part of `npm run build`):
 *   1. start the Go server with SERVER_PORT=18090 and a temp config,
 *   2. write a seed.json (admin creds + seeded service/customer ids) into QA_TMP_DIR,
 *   3. `node web/e2e/task56-mobile-qa.js`.
 * Env overrides: PLAYWRIGHT_MODULE (default "playwright"), QA_TMP_DIR, QA_EVIDENCE_DIR.
 */
/* eslint-disable */
const fs = require('fs');
const path = require('path');
const os = require('os');

const PW_PATH = process.env.PLAYWRIGHT_MODULE || 'playwright';
const { chromium, devices } = require(PW_PATH);

const DIR = process.env.QA_TMP_DIR || path.join(os.tmpdir(), 'ghs-qa');
const EVID = process.env.QA_EVIDENCE_DIR || path.resolve(__dirname, '../../.omo/evidence/task-56-go-hair-salon-mvp');
const seed = JSON.parse(fs.readFileSync(path.join(DIR, 'seed.json'), 'utf8').replace(/^\uFEFF/, ''));
const BASE = seed.base_url;

fs.mkdirSync(EVID, { recursive: true });

const results = [];
const artifacts = [];
let token = null;
let consoleErrors = {};
const expected422ByDevice = {};
let currentDeviceConsole = null;

function rec(device, check, pass, detail, ms) {
  results.push({ device, check, pass, detail, ms: ms == null ? null : ms });
  const dur = ms == null ? '' : ` (${ms}ms)`;
  console.log(`${pass ? 'PASS' : 'FAIL'} | ${device} | ${check} | ${detail}${dur}`);
}
function assert(cond, msg) { if (!cond) throw new Error('ASSERT: ' + msg); }

async function step(device, name, fn) {
  const t0 = Date.now();
  try {
    const d = await fn();
    rec(device, name, true, d == null ? '' : String(d), Date.now() - t0);
  } catch (e) {
    rec(device, name, false, String((e && e.message) || e).slice(0, 400), Date.now() - t0);
    throw e;
  }
}

async function shot(page, name) {
  const rel = name + '.png';
  await page.screenshot({ path: path.join(EVID, rel) });
  artifacts.push(rel);
  console.log('  shot -> ' + rel);
  return rel;
}

function cents(text) {
  const m = String(text).replace(/,/g, '').match(/(-?\d+\.\d{2})/);
  return m ? Math.round(parseFloat(m[1]) * 100) : null;
}

async function api(pathname, opts = {}) {
  const headers = { 'Content-Type': 'application/json', ...(opts.headers || {}) };
  if (token) headers.Authorization = 'Bearer ' + token;
  const res = await fetch(BASE + '/api/v1' + pathname, { ...opts, headers });
  let body = null;
  try { body = await res.json(); } catch (_) { /* ignore */ }
  return { status: res.status, body };
}

async function apiLogin() {
  const r = await api('/auth/login', { method: 'POST', body: JSON.stringify({ username: seed.admin_user, password: seed.admin_pass }) });
  assert(r.status === 200 && r.body && r.body.code === 0, `login API status=${r.status}`);
  token = r.body.data.token;
}

/* ---------- UI helpers ---------- */

async function login(page) {
  await page.goto(BASE + '/login', { waitUntil: 'domcontentloaded' });
  await page.fill('input[placeholder="用户名"]', seed.admin_user);
  await page.fill('input[placeholder="密码"]', seed.admin_pass);
  await page.click('button:has-text("登录")');
  await page.waitForSelector('.mobile-tabbar', { timeout: 20000 });
}

async function gotoCustomerTab(page) {
  await page.locator('.mobile-tabbar__item:has-text("客户")').click();
  await page.waitForURL(/\/customers(\?|$)/, { timeout: 15000 });
}

async function searchCustomer(page, phone) {
  const input = page.locator('.customer-toolbar__search input').first();
  await input.waitFor({ state: 'visible', timeout: 15000 });
  await input.fill(phone);
  await page.locator('.customer-toolbar button:has-text("搜索")').first().click();
  const card = page.locator('.customer-card', { hasText: phone }).first();
  await card.waitFor({ state: 'visible', timeout: 15000 });
  return card;
}

async function openDetail(page, card) {
  await card.click();
  await page.waitForURL(/\/customers\/\d+$/, { timeout: 15000 });
  await page.waitForSelector('.profile__hero', { timeout: 15000 });
}

async function heroBalanceCents(page) {
  const txt = await page.locator('.profile__hero-value').first().innerText();
  return cents(txt);
}

async function clickQuickConsume(page) {
  await page.locator('button:has-text("快速消费")').first().click();
  await page.waitForURL(/\/orders\/new/, { timeout: 15000 });
  await page.locator('.items-editor__select').first().waitFor({ state: 'visible', timeout: 15000 });
}

async function customerCardText(page) {
  return page.locator('.customer-card__select').first().innerText();
}

async function selectService(page) {
  await page.locator('.items-editor__select').first().click();
  const opt = page.locator('.el-select-dropdown__item:visible', { hasText: seed.service_name }).first();
  await opt.waitFor({ state: 'visible', timeout: 15000 });
  await opt.click();
  await page.keyboard.press('Escape');
  await page.locator('.items-editor__row').first().waitFor({ state: 'visible', timeout: 10000 });
}

async function choosePayment(page, label) {
  const radio = page.locator('label.el-radio', { hasText: label }).first();
  await radio.waitFor({ state: 'visible', timeout: 10000 });
  await radio.click();
}

async function checkedPayment(page) {
  return page.locator('label.el-radio.is-checked').first().innerText();
}

async function submitConsume(page) {
  await page.locator('button.confirm-card__submit').first().click();
  await page.waitForSelector('.el-result', { timeout: 20000 });
}

async function submitRecharge(page) {
  await page.locator('button.recharge-confirm__submit').first().click();
  await page.waitForSelector('.el-result', { timeout: 20000 });
}

async function backToCustomer(page) {
  await page.locator('button:has-text("查看客户")').first().click();
  await page.waitForURL(/\/customers\/\d+$/, { timeout: 15000 });
  await page.waitForSelector('.profile__hero', { timeout: 15000 });
}

async function openTab(page, label) {
  await page.locator('.el-tabs__item', { hasText: label }).first().click();
}

async function resultTitle(page) {
  return page.locator('.el-result__title').first().innerText();
}

/* ---------- device flows ---------- */

async function runDeviceFlow(deviceName, contextOpts, cfg) {
  rec(deviceName, 'device context', true, `viewport=${JSON.stringify(contextOpts.viewport)} ua=${String(contextOpts.userAgent).slice(0, 60)}`, null);
  const browser = await chromium.launch();
  const ctx = await browser.newContext(contextOpts);
  const errors = [];
  consoleErrors[deviceName] = errors;
  const expected422 = [];
  expected422ByDevice[deviceName] = expected422;
  await ctx.tracing.start({ screenshots: true, snapshots: true, sources: false });
  const page = await ctx.newPage();
  page.on('pageerror', (e) => errors.push('pageerror: ' + String(e)));
  page.on('console', (m) => {
    if (m.type() === 'error') {
      const t = m.text();
      if (/Failed to load resource.*status of 422/.test(t)) { expected422.push(t); return; }
      errors.push('console.error: ' + t);
    }
  });

  let aborted = false;
  try {
    await step(deviceName, 'login (Flow B start)', async () => { await login(page); return 'mobile shell rendered'; });

    /* ---- Flow B: search -> detail -> quick consume -> record ---- */
    await step(deviceName, 'B1 search customer by phone', async () => {
      await gotoCustomerTab(page);
      const card = await searchCustomer(page, cfg.phone);
      await shot(page, `flowB-1-search-${deviceName}`);
      return card ? `card found for ${cfg.phone}` : '';
    });
    await step(deviceName, 'B2 open detail', async () => {
      const card = page.locator('.customer-card', { hasText: cfg.phone }).first();
      await openDetail(page, card);
      const bal = await heroBalanceCents(page);
      assert(bal === 0, `expected starting balance 0, got ${bal}`);
      await shot(page, `flowB-2-detail-${deviceName}`);
      return `detail opened, hero balance=${bal} cents`;
    });
    await step(deviceName, 'B3 quick consume (cash)', async () => {
      await clickQuickConsume(page);
      const cardText = await customerCardText(page);
      assert(cardText.includes(cfg.name), `customer not preselected in consume form: "${cardText}"`);
      await selectService(page);
      const pay = await checkedPayment(page);
      assert(pay.includes('现金'), `default payment not cash: ${pay}`);
      await shot(page, `flowB-3-consume-form-${deviceName}`);
      await submitConsume(page);
      const title = await resultTitle(page);
      assert(title.includes('消费已完成'), `unexpected result title: ${title}`);
      const rb = cents(await page.locator('.result__balance-value').first().innerText());
      assert(rb === 0, `cash consume must not change balance, got ${rb}`);
      await shot(page, `flowB-4-result-${deviceName}`);
      return `order completed, balance after cash consume=${rb} cents`;
    });
    await step(deviceName, 'B4 order appears in customer record', async () => {
      await backToCustomer(page);
      await openTab(page, '消费记录');
      const card = page.locator('.order-card').first();
      await card.waitFor({ state: 'visible', timeout: 15000 });
      const txt = await card.innerText();
      assert(txt.includes('38.00'), `order card does not show 38.00: ${txt.replace(/\n/g, ' ')}`);
      await shot(page, `flowB-5-record-${deviceName}`);
      return `record shows order, amount 38.00 in "${txt.split('\n').slice(0, 3).join(' / ')}"`;
    });

    /* ---- Flow C: recharge -> balance -> ledger -> balance consume ---- */
    await step(deviceName, 'C1 recharge increases balance', async () => {
      await page.locator('button:has-text("充值")').first().click();
      await page.waitForURL(/\/recharges\/new/, { timeout: 15000 });
      const cardText = await customerCardText(page);
      assert(cardText.includes(cfg.name), `customer not preselected in recharge form: "${cardText}"`);
      await page.getByPlaceholder('客户实际支付金额，如 100').fill(String(cfg.recharge));
      if (cfg.gift > 0) await page.getByPlaceholder('不赠送留空，如 20').fill(String(cfg.gift));
      await shot(page, `flowC-1-recharge-form-${deviceName}`);
      await submitRecharge(page);
      const title = await resultTitle(page);
      assert(title.includes('充值成功'), `unexpected recharge title: ${title}`);
      const rb = cents(await page.locator('.recharge-result__balance-value').first().innerText());
      assert(rb === cfg.expectedAfterRecharge, `expected balance ${cfg.expectedAfterRecharge}, got ${rb}`);
      await shot(page, `flowC-2-recharge-result-${deviceName}`);
      return `recharged paid=${cfg.recharge} gift=${cfg.gift}, new balance=${rb} cents`;
    });
    await step(deviceName, 'C2 balance refreshed on detail', async () => {
      await backToCustomer(page);
      const bal = await heroBalanceCents(page);
      assert(bal === cfg.expectedAfterRecharge, `stale balance after recharge: expected ${cfg.expectedAfterRecharge}, got ${bal}`);
      return `fresh detail hero balance=${bal} cents (no stale state)`;
    });
    await step(deviceName, 'C3 balance ledger shows recharge + gift', async () => {
      await openTab(page, '余额流水');
      const card = page.locator('.tx-card').first();
      await card.waitFor({ state: 'visible', timeout: 15000 });
      const txt = await page.locator('.tx-card').allInnerTexts();
      const joined = txt.join(' | ');
      assert(joined.includes('充值') || joined.includes('recharge'), `no recharge ledger row: ${joined.slice(0, 200)}`);
      assert(joined.includes(cfg.recharge + '.00'), `principal ${cfg.recharge}.00 missing: ${joined.slice(0, 300)}`);
      if (cfg.gift > 0) assert(joined.includes(cfg.gift + '.00'), `gift ${cfg.gift}.00 missing: ${joined.slice(0, 300)}`);
      assert(joined.includes((cfg.expectedAfterRecharge / 100).toFixed(2)), `balance after recharge missing: ${joined.slice(0, 300)}`);
      await shot(page, `flowC-3-ledger-${deviceName}`);
      return `ledger rows: ${txt.length}`;
    });
    await step(deviceName, 'C4 consume using balance decreases balance', async () => {
      await clickQuickConsume(page);
      const cardText = await customerCardText(page);
      assert(cardText.includes(cfg.name), `customer not preselected: ${cardText}`);
      await selectService(page);
      await choosePayment(page, '余额');
      await shot(page, `flowC-4-balance-pay-form-${deviceName}`);
      await submitConsume(page);
      const title = await resultTitle(page);
      assert(title.includes('消费已完成'), `unexpected result title: ${title}`);
      const rb = cents(await page.locator('.result__balance-value').first().innerText());
      assert(rb === cfg.expectedFinalBalance, `expected final balance ${cfg.expectedFinalBalance}, got ${rb}`);
      await shot(page, `flowC-5-balance-result-${deviceName}`);
      return `balance-paid consume OK, final balance=${rb} cents`;
    });
    await step(deviceName, 'C5 ledger + detail reflect decrease', async () => {
      await backToCustomer(page);
      const bal = await heroBalanceCents(page);
      assert(bal === cfg.expectedFinalBalance, `stale balance after consume: expected ${cfg.expectedFinalBalance}, got ${bal}`);
      await openTab(page, '余额流水');
      await page.locator('.tx-card').first().waitFor({ state: 'visible', timeout: 15000 });
      const joined = (await page.locator('.tx-card').allInnerTexts()).join(' | ');
      assert(joined.includes('38.00'), `consume ledger row 38.00 missing: ${joined.slice(0, 300)}`);
      assert(joined.includes((cfg.expectedFinalBalance / 100).toFixed(2)), `final balance missing: ${joined.slice(0, 300)}`);
      await shot(page, `flowC-6-ledger-after-${deviceName}`);
      return `hero + ledger show final balance ${bal} cents`;
    });

    /* ---- API cross-check (server is source of truth) ---- */
    await step(deviceName, 'API cross-check balance/points', async () => {
      const cust = await api(`/customers/${cfg.id}`);
      assert(cust.status === 200, `GET customer status ${cust.status}`);
      const d = cust.body.data;
      assert(d.balance_cents === cfg.expectedFinalBalance, `server balance ${d.balance_cents} != expected ${cfg.expectedFinalBalance}`);
      assert(d.points === cfg.expectedPoints, `server points ${d.points} != expected ${cfg.expectedPoints}`);
      const tx = await api(`/customers/${cfg.id}/balance-transactions?page=1&page_size=50`);
      const types = (tx.body.data.items || []).map((i) => i.type);
      assert(types.includes('recharge') && types.includes('gift') && types.includes('consume'), `ledger types ${JSON.stringify(types)}`);
      return `server balance=${d.balance_cents} points=${d.points} txTypes=${JSON.stringify(types)}`;
    });
  } catch (e) {
    aborted = true;
    rec(deviceName, 'device flow', false, 'aborted: ' + String((e && e.message) || e));
    try { await shot(page, `failure-${deviceName}`); } catch (_) { /* ignore */ }
  } finally {
    try { await ctx.tracing.stop({ path: path.join(EVID, `trace-${deviceName}.zip`) }); artifacts.push(`trace-${deviceName}.zip`); } catch (_) { /* ignore */ }
    await browser.close();
  }
  return { errors, aborted };
}

/* ---------- 375px layout checks + malformed input probes ---------- */

async function runLayoutChecks(cfg375) {
  const deviceName = 'iphone375';
  const browser = await chromium.launch();
  const ctx = await browser.newContext(cfg375);
  consoleErrors[deviceName] = [];
  const expected422 = [];
  expected422ByDevice[deviceName] = expected422;
  await ctx.tracing.start({ screenshots: true, snapshots: true, sources: false });
  const page = await ctx.newPage();
  const errors = consoleErrors[deviceName];
  page.on('pageerror', (e) => errors.push('pageerror: ' + String(e)));
  page.on('console', (m) => {
    if (m.type() === 'error') {
      const t = m.text();
      if (/Failed to load resource.*status of 422/.test(t)) { expected422.push(t); return; }
      errors.push('console.error: ' + t);
    }
  });

  const scrollInfo = async () => page.evaluate(() => ({
    innerWidth: window.innerWidth,
    docScrollWidth: document.documentElement.scrollWidth,
    bodyScrollWidth: document.body.scrollWidth,
  }));

  let aborted = false;
  try {
    await step(deviceName, 'login @375px', async () => { await login(page); return `innerWidth=${(await scrollInfo()).innerWidth}`; });

    await step(deviceName, 'dashboard: no page-level horizontal scroll', async () => {
      await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });
      await page.waitForSelector('.quick-entries', { timeout: 15000 });
      const s = await scrollInfo();
      assert(s.docScrollWidth <= s.innerWidth + 1, `doc scrollWidth ${s.docScrollWidth} > innerWidth ${s.innerWidth}`);
      await shot(page, 'layout-375-dashboard');
      return `docScrollWidth=${s.docScrollWidth} innerWidth=${s.innerWidth}`;
    });

    await step(deviceName, 'dashboard recent grid no longer clips', async () => {
      const info = await page.locator('.dashboard__recent').first().evaluate((el) => {
        const cs = getComputedStyle(el);
        return { cols: cs.gridTemplateColumns, scrollWidth: el.scrollWidth, clientWidth: el.clientWidth };
      });
      assert(info.scrollWidth <= info.clientWidth + 1, `recent grid clips: scrollWidth ${info.scrollWidth} > clientWidth ${info.clientWidth}`);
      const trackCount = info.cols.trim().split(/\s+/).length;
      assert(trackCount === 1, `expected 1 grid track at 375px, got ${trackCount} (${info.cols})`);
      return `gridTemplateColumns="${info.cols}" (1 track), scrollWidth=${info.scrollWidth} <= clientWidth=${info.clientWidth}`;
    });

    await step(deviceName, 'bottom tab bar tappable (>=44px, 4 items)', async () => {
      const items = page.locator('.mobile-tabbar__item');
      const n = await items.count();
      assert(n === 4, `expected 4 tabs, got ${n}`);
      const labels = [];
      for (let i = 0; i < n; i++) {
        const box = await items.nth(i).boundingBox();
        const label = (await items.nth(i).innerText()).trim();
        labels.push(label);
        assert(box && box.height >= 44, `tab "${label}" height ${box && box.height} < 44`);
      }
      assert(labels.join(',') === 'Dashboard,客户,消费,充值', `unexpected tab labels: ${labels.join(',')}`);
      await items.nth(1).click();
      await page.waitForURL(/\/customers/, { timeout: 15000 });
      const s = await scrollInfo();
      assert(s.docScrollWidth <= s.innerWidth + 1, `customers doc scrollWidth ${s.docScrollWidth} > ${s.innerWidth}`);
      await shot(page, 'layout-375-customers');
      return `labels=[${labels.join(',')}] heights>=44, tap 客户 -> /customers, no h-scroll`;
    });

    await step(deviceName, 'orders tab @375px: no horizontal scroll', async () => {
      await page.locator('.mobile-tabbar__item:has-text("消费")').click();
      await page.waitForURL(/\/orders/, { timeout: 15000 });
      const s = await scrollInfo();
      assert(s.docScrollWidth <= s.innerWidth + 1, `orders doc scrollWidth ${s.docScrollWidth} > ${s.innerWidth}`);
      await shot(page, 'layout-375-orders');
      return `docScrollWidth=${s.docScrollWidth} innerWidth=${s.innerWidth}`;
    });

    await step(deviceName, 'malformed: search with no results', async () => {
      await gotoCustomerTab(page);
      const input = page.locator('.customer-toolbar__search input').first();
      await input.fill('19999999999');
      await page.locator('.customer-toolbar button:has-text("搜索")').first().click();
      await page.locator('text=未找到客户').first().waitFor({ state: 'visible', timeout: 15000 });
      await shot(page, 'malformed-375-no-results');
      return 'empty state "未找到客户" shown';
    });

    await step(deviceName, 'malformed: insufficient balance -> 422 surfaced, no order', async () => {
      const card = await searchCustomer(page, seed.probe.phone);
      await openDetail(page, card);
      const before = await api(`/orders?customer_id=${seed.probe.id}&page=1&page_size=5`);
      const beforeTotal = before.body.data.total;
      await clickQuickConsume(page);
      await selectService(page);
      await choosePayment(page, '余额');
      await page.locator('button.confirm-card__submit').first().click();
      await page.waitForFunction(() => document.body.innerText.includes('余额不足'), null, { timeout: 15000 });
      const msgs = await page.locator('.el-message--error, .confirm-card__error').allInnerTexts().catch(() => []);
      await shot(page, 'malformed-375-insufficient-balance');
      const hasResult = await page.locator('.el-result').count();
      assert(hasResult === 0, 'order result rendered despite insufficient balance');
      const after = await api(`/orders?customer_id=${seed.probe.id}&page=1&page_size=5`);
      assert(after.body.data.total === beforeTotal, `order count changed: ${beforeTotal} -> ${after.body.data.total}`);
      return `422 surfaced (${(msgs.join(' ').trim() || '余额不足 in body').slice(0, 80)}), orders total stays ${beforeTotal}`;
    });
  } catch (e) {
    aborted = true;
    rec(deviceName, 'layout/probe flow', false, 'aborted: ' + String((e && e.message) || e));
    try { await shot(page, 'failure-iphone375'); } catch (_) { /* ignore */ }
  } finally {
    try { await ctx.tracing.stop({ path: path.join(EVID, 'trace-iphone375.zip') }); artifacts.push('trace-iphone375.zip'); } catch (_) { /* ignore */ }
    await browser.close();
  }
  return { errors, aborted };
}

/* ---------- main ---------- */

function stripDefaultBrowserType(dev) {
  const { defaultBrowserType, ...rest } = dev;
  return rest;
}

(async () => {
  const t0 = Date.now();
  await apiLogin();
  console.log('API login OK (admin)');

  const iphoneOpts = stripDefaultBrowserType(devices['iPhone 12']);
  const androidOpts = stripDefaultBrowserType(devices['Pixel 5']);
  const iphone375Opts = { ...stripDefaultBrowserType(devices['iPhone 12']), viewport: { width: 375, height: 667 } };

  const iphoneCfg = {
    id: seed.iphone.id, name: seed.iphone.name, phone: seed.iphone.phone,
    recharge: 100, gift: 20,
    expectedAfterRecharge: 12000,
    expectedFinalBalance: 12000 - seed.service_price_cents,
    expectedPoints: 2 * Math.floor(seed.service_price_cents / 100),
  };
  const androidCfg = {
    id: seed.android.id, name: seed.android.name, phone: seed.android.phone,
    recharge: 200, gift: 50,
    expectedAfterRecharge: 25000,
    expectedFinalBalance: 25000 - seed.service_price_cents,
    expectedPoints: 2 * Math.floor(seed.service_price_cents / 100),
  };

  await runDeviceFlow('iphone', iphoneOpts, iphoneCfg);
  await runDeviceFlow('android', androidOpts, androidCfg);
  await runLayoutChecks(iphone375Opts);

  // console error check
  for (const dev of Object.keys(consoleErrors)) {
    const errs = consoleErrors[dev];
    rec(dev, 'no page/console errors', errs.length === 0, errs.length === 0 ? '0 errors' : JSON.stringify(errs.slice(0, 3)));
    const exp = expected422ByDevice[dev] || [];
    if (exp.length > 0) rec(dev, 'expected 422 console entry from deliberate probe (ignored)', true, `${exp.length} entry(ies): ${exp[0].slice(0, 80)}`);
  }

  const totalMs = Date.now() - t0;
  const fails = results.filter((r) => !r.pass);
  const report = {
    generatedAt: new Date().toISOString(),
    baseUrl: BASE,
    seed: { service: seed.service_name, service_price_cents: seed.service_price_cents, iphone: seed.iphone, android: seed.android, probe: seed.probe },
    expected: { iphone: iphoneCfg, android: androidCfg },
    playwrightVersion: require(PW_PATH + '/package.json').version,
    chromiumVersion: (await chromium.launch().then(async (b) => { const v = b.version(); await b.close(); return v; })),
    totalChecks: results.length,
    failedChecks: fails.length,
    totalMs,
    artifacts,
    consoleErrors,
    expected422FromProbe: expected422ByDevice,
    results,
  };
  fs.writeFileSync(path.join(EVID, 'task-56-run-report.json'), JSON.stringify(report, null, 2), 'utf8');

  console.log('\n================ SUMMARY ================');
  console.log(`checks=${results.length} failed=${fails.length} durationMs=${totalMs}`);
  for (const f of fails) console.log(`  FAIL ${f.device} :: ${f.check} :: ${f.detail}`);
  console.log(`artifacts=${artifacts.length} -> ${EVID}`);
  process.exit(fails.length === 0 ? 0 : 1);
})().catch((e) => {
  console.error('FATAL', e);
  process.exit(2);
});
