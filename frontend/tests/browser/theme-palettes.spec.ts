import { expect, test } from '@playwright/test';

const palettes = [
  { id: 'mint', label: '薄荷深色', background: 'rgb(17, 20, 23)', scheme: 'dark' },
  { id: 'midnight', label: '午夜蓝', background: 'rgb(16, 24, 38)', scheme: 'dark' },
  { id: 'plum', label: '石墨紫', background: 'rgb(25, 22, 32)', scheme: 'dark' },
  { id: 'cloud', label: '云白蓝', background: 'rgb(244, 247, 251)', scheme: 'light' },
  { id: 'paper', label: '暖纸棕', background: 'rgb(248, 244, 237)', scheme: 'light' },
];

for (const desktop of [false, true]) test(`shared color palettes persist and fit ${desktop ? 'desktop' : 'mobile'} settings`, async ({ page }, testInfo) => {
  if (desktop) await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto('/#settings');
  const themes = page.getByRole('group', { name: 'Theme', exact: true });
  await expect(themes).toBeVisible();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'nord');
  // Preferences must not reload the document or overwrite other local storage.
  await page.evaluate(() => {
    localStorage.setItem('appearance-test-draft', 'synthetic retained local draft');
    localStorage.setItem('appearance-test-delivery', 'unconfirmed');
    document.documentElement.dataset.appearanceTest = 'same-document';
  });
  for (const palette of palettes) {
    const choice = themes.getByRole('button', { name: palette.label, exact: true });
    await choice.click();
    await expect(choice).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('html')).toHaveAttribute('data-theme', palette.id);
    await expect(page.locator('html')).toHaveAttribute('data-appearance-test', 'same-document');
    const state = await page.evaluate(() => {
      const style = getComputedStyle(document.documentElement);
      const terminal = document.createElement('div'); terminal.className = 'term-content'; terminal.hidden = true;
      document.body.append(terminal);
      const terminalStyle = getComputedStyle(terminal);
      const terminalText = terminalStyle.color, terminalBackground = terminalStyle.backgroundColor;
      terminal.remove();
      return {
        scheme: style.colorScheme, background: getComputedStyle(document.body).backgroundColor,
        terminal: terminalBackground, terminalText,
        saved: localStorage.getItem('herdr_theme'), draft: localStorage.getItem('appearance-test-draft'),
        delivery: localStorage.getItem('appearance-test-delivery'),
        fits: document.documentElement.scrollWidth <= document.documentElement.clientWidth,
      };
    });
    expect(state).toEqual({
      scheme: palette.scheme, background: palette.background, terminal: 'rgb(17, 20, 23)', terminalText: 'rgb(237, 240, 243)',
      saved: palette.id, draft: 'synthetic retained local draft', delivery: 'unconfirmed', fits: true,
    });
    await themes.screenshot({ path: testInfo.outputPath(`${palette.id}-${desktop ? 'desktop' : 'mobile'}.png`) });
  }
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'paper');
  await expect(themes.getByRole('button', { name: '暖纸棕', exact: true })).toHaveAttribute('aria-pressed', 'true');
  for (const legacy of ['dark', 'light', 'nord', 'solarized', 'rose', 'latte']) {
    await themes.getByRole('button', { name: legacy, exact: true }).click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', legacy);
  }
  expect(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--terminal').trim())).toBe('#eff1f5');
});

test('loads settings on demand without resetting the saved light palette', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('herdr_theme', 'cloud'));
  let release!: () => void;
  const delayed = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/SettingsView-*.js', async route => { await delayed; await route.continue(); });
  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'cloud');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Loading settings…' })).toBeVisible();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'cloud');
  release();
  await expect(page.getByRole('group', { name: 'Theme', exact: true }).getByRole('button', { name: '云白蓝', exact: true })).toHaveAttribute('aria-pressed', 'true');
});
