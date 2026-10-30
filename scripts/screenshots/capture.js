// Drives the browser for scripts/screenshots/capture.sh.
//
// The shell script builds the server, prepares a demo config directory and
// waits for /health; this file only navigates the pages and writes the PNGs.
// Configuration through the environment:
//
//   GOSPIN_SHOT_BASE  URL the browser opens, e.g. http://demo.local:18084
//   GOSPIN_SHOT_API   URL this process calls over plain HTTP, e.g.
//                     http://127.0.0.1:18084 (the browser reaches demo.local
//                     through --host-resolver-rules, node does not)
//   GOSPIN_SHOT_DIR   output directory, default docs/screenshots

const path = require('path');
const { chromium } = require('playwright');

const BASE = process.env.GOSPIN_SHOT_BASE || 'http://demo.local:18084';
const API = process.env.GOSPIN_SHOT_API || 'http://127.0.0.1:18084';
const OUT = process.env.GOSPIN_SHOT_DIR || path.join(__dirname, '..', '..', 'docs', 'screenshots');

// Attachment seeded on the board of the screenshot, so the file panel is part
// of the picture instead of an empty drop zone.
const BOARD_FILE = path.join(__dirname, 'fixtures', 'demo-note.txt');

// Text seeded on the shared board before the ghostboard screenshot.
const BOARD_TEXT = `GoSpin demo board

- build 0.0.13 green
- screenshots regenerated
- gb-cli: gb-cli add -b demo "note from the shell"

TODO: tag the release`;

const problems = [];

function report(message) {
  console.log(`  ${message}`);
}

// contentHeight trims the dashboard captures to the content, so the images do
// not end with half a screen of empty background. The height never exceeds the
// viewport.
async function contentHeight(page, max) {
  const height = await page.evaluate(() => {
    let bottom = 0;
    for (const el of document.querySelectorAll('table, .rounded-lg, .shadow')) {
      const rect = el.getBoundingClientRect();
      if (rect.height > 0) bottom = Math.max(bottom, rect.bottom + window.scrollY);
    }
    return Math.ceil(bottom);
  });
  return Math.min(max, height + 32);
}

async function openPage(browser, width, height) {
  const context = await browser.newContext({
    viewport: { width, height },
    deviceScaleFactor: 1,
    locale: 'en-GB',
    timezoneId: 'Europe/Rome',
    colorScheme: 'light',
  });
  const page = await context.newPage();
  page.on('pageerror', (error) => problems.push(`page error: ${error.message}`));
  page.on('console', (message) => {
    if (message.type() === 'error') problems.push(`console: ${message.text()}`);
  });
  return page;
}

async function seedBoard() {
  const response = await fetch(`${API}/api/boards/demo`, {
    method: 'POST',
    headers: { 'Content-Type': 'text/plain' },
    body: BOARD_TEXT,
  });
  if (!response.ok) {
    throw new Error(`seeding the demo board failed with HTTP ${response.status}`);
  }
}

async function main() {
  await seedBoard();

  const browser = await chromium.launch({
    args: ['--host-resolver-rules=MAP demo.local 127.0.0.1'],
  });

  try {
    // Dashboard: containers is the default tab, the other two are clicked.
    const ui = await openPage(browser, 1600, 1000);
    await ui.goto(`${BASE}/ui`, { waitUntil: 'load' });
    await ui.waitForSelector('table tbody tr', { timeout: 30000 });
    await ui.waitForTimeout(3000);

    const tabs = [
      { file: 'containers.png', label: null },
      { file: 'groups.png', label: 'Groups' },
      { file: 'schedules.png', label: 'Schedules' },
    ];
    for (const tab of tabs) {
      if (tab.label) {
        await ui.click(`button:has-text("${tab.label}")`);
        await ui.waitForTimeout(1500);
      }
      await ui.screenshot({
        path: path.join(OUT, tab.file),
        // Freeze transitions before capturing: the tab content fades in, and a
        // shot taken mid-fade shows washed-out rows.
        animations: 'disabled',
        clip: { x: 0, y: 0, width: 1600, height: await contentHeight(ui, 1000) },
      });
      report(`wrote ${tab.file}`);
    }

    const home = await openPage(browser, 1600, 1000);
    await home.goto(`${BASE}/ui/home`, { waitUntil: 'load' });
    await home.waitForSelector('text=INFRASTRUCTURE', { timeout: 30000 });
    await home.waitForTimeout(3000);
    await home.screenshot({ path: path.join(OUT, 'home.png'), animations: 'disabled' });
    report('wrote home.png');

    const ghostboard = await openPage(browser, 1400, 760);
    await ghostboard.goto(`${BASE}/ui/ghostboard/demo`, { waitUntil: 'load' });
    await ghostboard.waitForSelector('textarea', { timeout: 30000 });
    await ghostboard.getByRole('button', { name: /Files/ }).click();
    await ghostboard.setInputFiles('#ghostboard-files input[type=file]', BOARD_FILE);
    await ghostboard.waitForSelector('#ghostboard-files a[download="demo-note.txt"]', { timeout: 30000 });
    await ghostboard.waitForTimeout(1000);
    await ghostboard.screenshot({ path: path.join(OUT, 'ghostboard.png'), animations: 'disabled' });
    report('wrote ghostboard.png');
  } finally {
    await browser.close();
  }

  if (problems.length > 0) {
    console.log('browser reported problems:');
    for (const problem of problems) console.log(`  ${problem}`);
  } else {
    console.log('no browser errors');
  }
}

main().catch((error) => {
  console.error(`capture failed: ${error.message}`);
  process.exit(1);
});
