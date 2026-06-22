import { expect, test } from '@playwright/test';
import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process';
import { once } from 'node:events';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const root = path.resolve(__dirname, '../..');
const port = process.env.PORT || '8099';
const postPath = '/posts/agentic-optimization-self-improving-research-loop';

let server: ChildProcessWithoutNullStreams;

test.beforeAll(async () => {
  server = spawn('go', ['run', '.'], {
    cwd: root,
    env: {
      ...process.env,
      DEV: '1',
      PORT: port,
      GOCACHE: mkdtempSync(path.join(tmpdir(), 'arehman-web-gocache-')),
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });

  const stderr: string[] = [];
  server.stderr.on('data', chunk => stderr.push(String(chunk)));
  server.stdout.on('data', () => {});

  const started = waitForHealth(`http://127.0.0.1:${port}/healthz`, 45_000);
  const exited = once(server, 'exit').then(([code]) => {
    throw new Error(`server exited before tests started with code ${code}\n${stderr.join('')}`);
  });
  await Promise.race([started, exited]);
});

test.afterAll(async () => {
  if (!server || server.killed) return;
  server.kill();
  await Promise.race([
    once(server, 'exit'),
    new Promise(resolve => setTimeout(resolve, 2_000)),
  ]);
});

test('post renders without horizontal overflow', async ({ page }) => {
  await page.goto(postPath);

  await expect(page).toHaveTitle(/Agentic Optimization/);
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Agentic Optimization');

  const overflow = await page.evaluate(() => {
    const root = document.documentElement;
    const body = document.body;
    return Math.max(root.scrollWidth, body.scrollWidth) - root.clientWidth;
  });
  expect(overflow).toBeLessThanOrEqual(1);
});

test('article figures are visible and fit the viewport', async ({ page }) => {
  await page.goto(postPath);

  const figures = page.locator('.prose img');
  await expect(figures).toHaveCount(2);

  const viewportWidth = page.viewportSize()?.width ?? 0;
  for (let i = 0; i < await figures.count(); i++) {
    const box = await figures.nth(i).boundingBox();
    expect(box, `figure ${i + 1} should render`).not.toBeNull();
    expect(box!.width).toBeGreaterThan(250);
    expect(box!.width).toBeLessThanOrEqual(viewportWidth);
    expect(box!.height).toBeGreaterThan(150);
  }
});

async function waitForHealth(url: string, timeoutMs: number) {
  const deadline = Date.now() + timeoutMs;
  let lastError: unknown;

  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
    } catch (error) {
      lastError = error;
    }
    await new Promise(resolve => setTimeout(resolve, 250));
  }

  throw new Error(`server did not become healthy at ${url}: ${String(lastError)}`);
}
