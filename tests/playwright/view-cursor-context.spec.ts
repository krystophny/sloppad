import { expect, test, type Page } from '@playwright/test';

type HarnessLogEntry = {
  type: string;
  text?: string;
  cursor?: Record<string, unknown> | null;
};

async function waitReady(page: Page) {
  await page.goto('/tests/playwright/harness.html');
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    if (typeof app?.getState !== 'function') return false;
    const state = app.getState();
    const wsOpen = (window as any).WebSocket.OPEN;
    return state.chatWs?.readyState === wsOpen && state.canvasWs?.readyState === wsOpen;
  }, null, { timeout: 8_000 });
}

async function clearLog(page: Page) {
  await page.evaluate(() => {
    (window as any).__harnessLog.splice(0);
  });
}

async function getLog(page: Page): Promise<HarnessLogEntry[]> {
  return page.evaluate(() => (window as any).__harnessLog.slice());
}

async function waitForSentMessage(page: Page) {
  await expect.poll(async () => {
    const log = await getLog(page);
    return log.find((entry) => entry.type === 'message_sent') || null;
  }).not.toBeNull();
}

async function submitVoiceStyleMessage(page: Page, text: string) {
  await page.evaluate(async (messageText) => {
    const app = (window as any)._taburaApp;
    if (app?.getState) app.getState().lastInputOrigin = 'voice';
    const mod = await import('../../internal/web/static/app-chat-submit.js');
    await mod.submitMessage(messageText, { kind: 'voice_transcript' });
  }, text);
}

async function injectChatEvent(page: Page, payload: Record<string, unknown>) {
  await page.evaluate((eventPayload) => {
    const app = (window as any)._taburaApp;
    const activeChatWs = app?.getState?.().chatWs;
    if (activeChatWs && typeof activeChatWs.injectEvent === 'function') {
      activeChatWs.injectEvent(eventPayload);
    }
  }, payload);
}

test('pointed inbox and workspace views send structured cursor context', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await waitReady(page);

  await page.locator('#edge-left-tap').click();
  await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);

  await page.locator('#pr-file-list .pr-file-item[data-item-id="101"]').click();
  await clearLog(page);
  await submitVoiceStyleMessage(page, 'delete this');
  await waitForSentMessage(page);

  const itemSent = (await getLog(page)).find((entry) => entry.type === 'message_sent');
  expect(itemSent?.cursor).toMatchObject({
    view: 'inbox',
    element: 'item_row',
    item_id: 101,
    item_title: 'Review parser cleanup',
    item_state: 'inbox',
  });

  await page.getByRole('button', { name: 'Files' }).click();
  await page.locator('#pr-file-list .pr-file-item', { hasText: 'docs' }).focus();
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    return app?.getState?.().workspaceBrowserActivePath === 'docs';
  });

  await clearLog(page);
  await submitVoiceStyleMessage(page, 'open this');
  await waitForSentMessage(page);

  const workspaceSent = (await getLog(page)).find((entry) => entry.type === 'message_sent');
  expect(workspaceSent?.cursor).toMatchObject({
    view: 'workspace_browser',
    element: 'workspace_folder',
    path: 'docs',
    is_dir: true,
  });
  expect(String(workspaceSent?.cursor?.workspace_name || '')).not.toBe('');
});

test('cursor-driven system actions reopen the pointed item and workspace path', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await waitReady(page);

  await page.locator('#edge-left-tap').click();
  await expect(page.locator('#pr-file-pane')).toHaveClass(/is-open/);

  await injectChatEvent(page, {
    type: 'system_action',
    action: {
      type: 'open_item_sidebar_item',
      item_id: 102,
    },
  });
  await expect(page.locator('#pr-file-list .pr-file-item.is-active[data-item-id="102"]')).toHaveCount(1);

  await page.getByRole('button', { name: 'Files' }).click();
  await injectChatEvent(page, {
    type: 'system_action',
    action: {
      type: 'open_workspace_path',
      path: 'docs',
      is_dir: true,
    },
  });
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    return app?.getState?.().workspaceBrowserPath === 'docs';
  });
  await expect(page.locator('#pr-file-list')).toContainText('guide.md');

  await injectChatEvent(page, {
    type: 'system_action',
    action: {
      type: 'open_workspace_path',
      path: 'docs/guide.md',
      is_dir: false,
    },
  });
  await page.waitForFunction(() => {
    const app = (window as any)._taburaApp;
    return app?.getState?.().workspaceOpenFilePath === 'docs/guide.md';
  });
  await expect(page.locator('#canvas-text')).toContainText('docs/guide.md');
});
