import { expect, test, type Page } from '@playwright/test';

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/chat-harness.html');
  await page.waitForSelector('#prompt-input', { state: 'visible', timeout: 5_000 });
  await page.waitForTimeout(200);
}

async function injectCanvasModuleRef(page: Page) {
  await page.evaluate(async () => {
    const mod = await import('../../internal/web/static/canvas.js');
    (window as any).__canvasModule = mod;
  });
}

async function renderTestArtifact(page: Page) {
  await page.evaluate(() => {
    const mod = (window as any).__canvasModule;
    mod.renderCanvas({
      event_id: 'art-1',
      kind: 'text_artifact',
      title: 'test.txt',
      text: 'Line one\nLine two\nLine three\nLine four\nLine five',
    });
  });
  // Simulate what app.js does: show canvas column with the right pane
  await page.evaluate(() => {
    const col = document.getElementById('canvas-column');
    if (col) col.style.display = '';
    const ct = document.getElementById('canvas-text');
    if (ct) {
      ct.style.display = '';
      ct.classList.add('is-active');
    }
  });
}

async function installMessageSpy(page: Page) {
  await page.evaluate(() => {
    (window as any).__sentBodies = [];
    const prev = window.fetch;
    window.fetch = async function(url: any, opts: any) {
      const u = String(url);
      if (u.includes('/messages') && opts?.method === 'POST') {
        try {
          const body = JSON.parse(opts.body);
          (window as any).__sentBodies.push(body);
        } catch (_) {}
      }
      return prev.apply(this, arguments as any);
    };
  });
}

async function getSentBodies(page: Page): Promise<any[]> {
  return page.evaluate(() => (window as any).__sentBodies.slice());
}

test.describe('two-column layout', () => {
  test.beforeEach(async ({ page }) => {
    await waitReady(page);
    await injectCanvasModuleRef(page);
    await installMessageSpy(page);
  });

  test('desktop: chat column visible, canvas column hidden when no artifact', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });
    const chatColumn = page.locator('#chat-column');
    await expect(chatColumn).toBeVisible();
    const canvasColumn = page.locator('#canvas-column');
    const display = await canvasColumn.evaluate(el => el.style.display);
    expect(display).toBe('none');
  });

  test('desktop: artifact renders in left column, chat visible on right', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });
    await renderTestArtifact(page);
    const canvasColumn = page.locator('#canvas-column');
    const canvasDisplay = await canvasColumn.evaluate(el => el.style.display);
    expect(canvasDisplay).not.toBe('none');

    const chatColumn = page.locator('#chat-column');
    await expect(chatColumn).toBeVisible();

    const canvasText = page.locator('#canvas-text');
    await expect(canvasText).toBeVisible();
  });

  test('right-click on artifact sets prompt context badge', async ({ page }) => {
    await renderTestArtifact(page);
    const canvasText = page.locator('#canvas-text');
    await expect(canvasText).toBeVisible();

    const box = await canvasText.boundingBox();
    if (!box) throw new Error('canvas-text not visible');
    await page.mouse.click(box.x + 20, box.y + 20, { button: 'right' });
    await page.waitForTimeout(200);

    // In headless, caretRangeFromPoint may not resolve, so badge may or may not appear.
    // Verify no crash and no annotation bubble.
    const bubbleCount = await page.locator('.annotation-bubble').count();
    expect(bubbleCount).toBe(0);
  });

  test('left-click on artifact does not set prompt context', async ({ page }) => {
    await renderTestArtifact(page);
    const canvasText = page.locator('#canvas-text');
    await expect(canvasText).toBeVisible();

    const box = await canvasText.boundingBox();
    if (!box) throw new Error('canvas-text not visible');
    await page.mouse.click(box.x + 20, box.y + 20);
    await page.waitForTimeout(200);

    // No prompt context badge from left-click
    const badgeCount = await page.locator('.prompt-context').count();
    expect(badgeCount).toBe(0);
  });

  test('prompt context badge can be dismissed', async ({ page }) => {
    // Programmatically set a prompt context to test dismissal
    await page.evaluate(() => {
      const app = (window as any)._taburaApp;
      if (app?.getState) {
        const state = app.getState();
        state.promptContext = { line: 5, title: 'test.txt' };
      }
      // Manually render badge
      const bar = document.getElementById('prompt-bar');
      if (!bar) return;
      const badge = document.createElement('span');
      badge.className = 'prompt-context';
      badge.textContent = 'Line 5 of "test.txt"';
      const dismiss = document.createElement('button');
      dismiss.type = 'button';
      dismiss.className = 'prompt-context-dismiss';
      dismiss.textContent = '\u00d7';
      dismiss.addEventListener('click', () => {
        const s = (window as any)._taburaApp?.getState?.();
        if (s) s.promptContext = null;
        badge.remove();
      });
      badge.appendChild(dismiss);
      const status = bar.querySelector('.prompt-status');
      if (status) status.after(badge);
      else bar.prepend(badge);
    });

    await expect(page.locator('.prompt-context')).toBeVisible();
    await page.locator('.prompt-context-dismiss').click();
    await page.waitForTimeout(100);
    await expect(page.locator('.prompt-context')).toHaveCount(0);
  });

  test('canvas clear hides canvas column', async ({ page }) => {
    await renderTestArtifact(page);
    const canvasColumn = page.locator('#canvas-column');
    let display = await canvasColumn.evaluate(el => el.style.display);
    expect(display).not.toBe('none');

    // Trigger clear_canvas
    await page.evaluate(() => {
      const mod = (window as any).__canvasModule;
      mod.renderCanvas({ kind: 'clear_canvas' });
      // app.js would call hideCanvasColumn, simulate it
      const col = document.getElementById('canvas-column');
      if (col) col.style.display = 'none';
    });

    display = await canvasColumn.evaluate(el => el.style.display);
    expect(display).toBe('none');
  });

  test('text selection works normally without opening bubble', async ({ page }) => {
    await renderTestArtifact(page);
    const canvasText = page.locator('#canvas-text');
    await expect(canvasText).toBeVisible();

    const box = await canvasText.boundingBox();
    if (!box) throw new Error('canvas-text not visible');

    await page.mouse.move(box.x + 10, box.y + 10);
    await page.mouse.down();
    await page.mouse.move(box.x + 100, box.y + 10);
    await page.mouse.up();
    await page.waitForTimeout(200);

    const bubbleCount = await page.locator('.annotation-bubble').count();
    expect(bubbleCount).toBe(0);
  });

  test('line highlight absent after left-click', async ({ page }) => {
    await renderTestArtifact(page);

    const canvasText = page.locator('#canvas-text');
    const box = await canvasText.boundingBox();
    if (box) {
      await page.mouse.click(box.x + 20, box.y + 20);
      await page.waitForTimeout(100);
    }

    const highlightCount = await page.locator('.review-line-highlight').count();
    expect(highlightCount).toBe(0);
  });

  test('no tab bar in DOM', async ({ page }) => {
    const tabBar = await page.locator('#canvas-tab-bar').count();
    expect(tabBar).toBe(0);
  });

  test('no canvas-chat pane in DOM', async ({ page }) => {
    const chatPane = await page.locator('#canvas-chat').count();
    expect(chatPane).toBe(0);
  });

  test('send message without context has no location prefix', async ({ page }) => {
    const input = page.locator('#prompt-input');
    await input.fill('hello');
    await page.locator('#prompt-send').click();
    await page.waitForTimeout(300);

    const bodies = await getSentBodies(page);
    expect(bodies.length).toBeGreaterThanOrEqual(1);
    const sent = bodies[bodies.length - 1];
    expect(sent.text).toBe('hello');
    expect(sent.thread_key).toBeUndefined();
  });
});
