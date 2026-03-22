import { expect, test, type Page } from '@playwright/test';

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const s = app.getState();
    const wsOpen = (window as any).WebSocket.OPEN;
    return s.chatWs?.readyState === wsOpen && s.canvasWs?.readyState === wsOpen;
  }, null, { timeout: 8_000 });
}

async function seedSidebarItems(page: Page) {
  await page.evaluate(() => {
    (window as any).__setItemSidebarData({
      inbox: [
        {
          id: 501,
          title: 'Keep the close strip free',
          state: 'inbox',
          sphere: 'private',
          artifact_id: 701,
          artifact_title: 'Sidebar gutter note',
          artifact_kind: 'idea_note',
          created_at: '2026-03-10 09:00:00',
          updated_at: '2026-03-10 09:05:00',
        },
        {
          id: 502,
          title: 'Second row for overlap coverage',
          state: 'inbox',
          sphere: 'private',
          artifact_id: 702,
          artifact_title: 'Second sidebar note',
          artifact_kind: 'idea_note',
          created_at: '2026-03-10 09:10:00',
          updated_at: '2026-03-10 09:15:00',
        },
      ],
      waiting: [],
      someday: [],
      done: [],
    });
  });
}

async function openInbox(page: Page) {
  await page.locator('#edge-left-tap').click();
  await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);
  await page.locator('.sidebar-tab', { hasText: 'Inbox' }).click();
  await expect(page.locator('.sidebar-tab.is-active')).toContainText('Inbox');
}

async function expectSidebarCloseStripToStayFree(page: Page) {
  const row = page.locator('#pr-file-list .pr-file-item[data-item-id="501"]');
  const edgeLeftTap = page.locator('#edge-left-tap');
  const rowBox = await row.boundingBox();
  const edgeBox = await edgeLeftTap.boundingBox();
  expect(rowBox).not.toBeNull();
  expect(edgeBox).not.toBeNull();
  const stripRight = Number(edgeBox?.x || 0) + Number(edgeBox?.width || 0);
  expect(Number(rowBox?.x || 0)).toBeGreaterThanOrEqual(stripRight - 1);
}

async function clickSidebarCloseStripAtRowHeight(page: Page) {
  const row = page.locator('#pr-file-list .pr-file-item[data-item-id="501"]');
  const edgeLeftTap = page.locator('#edge-left-tap');
  const rowBox = await row.boundingBox();
  const edgeBox = await edgeLeftTap.boundingBox();
  expect(rowBox).not.toBeNull();
  expect(edgeBox).not.toBeNull();
  await page.mouse.click(
    Number(edgeBox?.x || 0) + Number(edgeBox?.width || 0) / 2,
    Number(rowBox?.y || 0) + Number(rowBox?.height || 0) / 2,
  );
}

test.describe('sidebar close strip', () => {
  test('desktop close strip stays outside sidebar row hit targets', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);
    await seedSidebarItems(page);
    await openInbox(page);

    await expectSidebarCloseStripToStayFree(page);
    await clickSidebarCloseStripAtRowHeight(page);

    await expect(page.locator('#pr-file-pane')).not.toHaveClass(/is-open/);
    await expect.poll(async () => page.evaluate(() => {
      const app = (window as any)._taburaApp;
      return Number(app?.getState?.().itemSidebarActiveItemID || 0);
    })).toBe(0);
  });

  test('firefox-bug-report keeps the desktop close strip clickable over sidebar rows', async ({ page, browserName }) => {
    test.skip(browserName !== 'firefox', 'Firefox-only regression coverage');
    await page.setViewportSize({ width: 1280, height: 800 });
    await waitReady(page);
    await seedSidebarItems(page);
    await openInbox(page);

    await expectSidebarCloseStripToStayFree(page);
    await clickSidebarCloseStripAtRowHeight(page);

    await expect(page.locator('#pr-file-pane')).not.toHaveClass(/is-open/);
    await expect.poll(async () => page.evaluate(() => {
      const app = (window as any)._taburaApp;
      return ({
        open: Boolean(app?.getState?.().prReviewDrawerOpen),
        activeItemID: Number(app?.getState?.().itemSidebarActiveItemID || 0),
      });
    })).toEqual({ open: false, activeItemID: 0 });
  });
});
