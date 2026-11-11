import { get } from 'svelte/store';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { THEME_COLORS, THEME_KEY, THEME_LABELS, THEME_TERMINAL_SCHEMES, THEMES } from '$lib/config';
import { readFileSync } from 'node:fs';

const css = readFileSync('src/app.css', 'utf8');

const shared = ['mint', 'midnight', 'plum', 'cloud', 'paper'] as const;
const legacy = ['dark', 'light', 'nord', 'solarized', 'rose', 'latte'] as const;

function contrast(a: string, b: string): number {
  const luminance = (hex: string) => {
    const channels = [1, 3, 5].map(offset => {
      const n = parseInt(hex.slice(offset, offset + 2), 16) / 255;
      return n <= .04045 ? n / 12.92 : ((n + .055) / 1.055) ** 2.4;
    });
    return channels[0] * .2126 + channels[1] * .7152 + channels[2] * .0722;
  };
  const l1 = luminance(a), l2 = luminance(b);
  return (Math.max(l1, l2) + .05) / (Math.min(l1, l2) + .05);
}

afterEach(() => {
  document.documentElement.removeAttribute('data-theme');
  document.head.querySelector('meta[name="theme-color"]')?.remove();
  vi.resetModules();
});

describe('local interface palettes', () => {
  it.each([...legacy, ...shared])('loads and persists %s without changing other local state', async id => {
    localStorage.setItem(THEME_KEY, id);
    localStorage.setItem('herdr_relays', '[{"id":"fixture","token":"synthetic"}]');
    localStorage.setItem('herdr_prompt_draft_v1:fixture', 'keep my draft');
    localStorage.setItem('herdr_pi_prompt_delivery_v1', 'unconfirmed');
    // The test storage exposes key()/length, like the browser's Storage interface.
    const snapshot = Array.from({ length: localStorage.length }, (_, i) => localStorage.key(i)!).filter(key => key !== THEME_KEY).map(key => [key, localStorage.getItem(key)]);
    const { theme, initializePreferences, setTheme } = await import('$lib/preferences');
    expect(get(theme)).toBe(id);
    const meta = document.createElement('meta'); meta.name = 'theme-color'; document.head.append(meta);
    initializePreferences();
    expect(document.documentElement.dataset.theme).toBe(id);
    setTheme(id);
    expect(localStorage.getItem(THEME_KEY)).toBe(id);
    expect(meta.content).toBe(THEME_COLORS[id]);
    for (const [key, value] of snapshot) expect(localStorage.getItem(key!)).toBe(value);
  });

  it.each([null, 'removed-theme'])('retains the original nord default for %s', async saved => {
    if (saved !== null) localStorage.setItem(THEME_KEY, saved);
    const { theme } = await import('$lib/preferences');
    expect(get(theme)).toBe('nord');
  });

  it('keeps ANSI rendering dark for all new palettes, including the light interfaces', async () => {
    const { setTheme } = await import('$lib/preferences');
    const { terminalHtml } = await import('$lib/terminal');
    const ansi = '\x1b[31mred output\x1b[0m \x1b[38;2;220;230;240mtrue color\x1b[0m';
    setTheme('dark');
    const original = terminalHtml(ansi);
    for (const id of shared) {
      setTheme(id);
      expect(THEME_TERMINAL_SCHEMES[id]).toBe('dark');
      expect(terminalHtml(ansi)).toBe(original);
    }
    expect(THEME_TERMINAL_SCHEMES.latte).toBe('light');
  });

  it('offers the original themes and readable shared palettes with consistent browser colors', () => {
    expect(THEMES).toEqual([...legacy, ...shared]);
    for (const id of shared) {
      expect(THEME_LABELS[id]).not.toBe(id);
      const block = css.split(`:root[data-theme='${id}'] {`)[1]?.split('}')[0];
      expect(block).toBeDefined();
      const tokens = Object.fromEntries([...block.matchAll(/--([a-z-]+): (#[a-f0-9]{6});/g)].map(match => [match[1], match[2]]));
      expect(tokens.background).toBe(THEME_COLORS[id]);
      expect(block).toContain(`color-scheme: ${id === 'cloud' || id === 'paper' ? 'light' : 'dark'};`);
      expect(tokens.terminal).toBe('#111417');
      for (const bg of ['background', 'card', 'card-hover', 'secondary', 'input']) {
        for (const fg of ['foreground', 'muted', 'primary', 'danger', 'warning', 'success', 'trust']) {
          expect(contrast(tokens[fg], tokens[bg]), `${id}: ${fg} on ${bg}`).toBeGreaterThanOrEqual(4.5);
        }
      }
      expect(contrast(tokens['primary-foreground'], tokens.primary)).toBeGreaterThanOrEqual(4.5);
    }
  });
});
