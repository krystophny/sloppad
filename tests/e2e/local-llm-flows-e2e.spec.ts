import type { Page } from '@playwright/test';
import { expect, openLiveApp, test } from './live';
import { authenticate, clearLiveChat } from './helpers';
import {
  resetCircleRuntimeState,
  setCircleToggle,
  setLiveSession,
  submitPrompt,
  waitForAssistantReply,
  waitForLiveAppReady,
} from './live-ui';

async function sendPromptAndExpect(page: Page, prompt: string, needle: string) {
  const before = await page.locator('#chat-history .chat-message.chat-assistant:not(.is-pending)').count();
  await submitPrompt(page, prompt);
  const reply = await waitForAssistantReply(page, before, needle, 120_000);
  expect(reply.toLowerCase()).toContain(needle.toLowerCase());
}

test.describe('local llm conversation flows @local-only', () => {
  let sessionToken: string;

  test.beforeAll(async () => {
    sessionToken = await authenticate();
  });

  test('usual typed chat returns a local model answer', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);
    await sendPromptAndExpect(page, 'Reply with the single word ORBIT.', 'orbit');
  });

  test('fast typed chat returns a local model answer', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);
    await setCircleToggle(page, 'fast', true);
    await sendPromptAndExpect(page, 'Reply with the single word RIVET.', 'rivet');
  });

  test('silent typed chat returns a local model answer', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);
    await setCircleToggle(page, 'silent', true);
    await expect(page.locator('body')).toHaveClass(/silent-mode/);
    await sendPromptAndExpect(page, 'Reply with the single word KESTREL.', 'kestrel');
  });

  test('dialogue typed flow returns a local model answer while dialogue stays active', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);
    await setCircleToggle(page, 'silent', true);
    await setLiveSession(page, 'dialogue', true);
    await sendPromptAndExpect(page, 'Reply with the single word HARBOR.', 'harbor');
    await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Dialogue');
  });

  test('meeting typed flow returns a local model answer while meeting stays active', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);
    await setCircleToggle(page, 'silent', true);
    await setLiveSession(page, 'meeting', true);
    await sendPromptAndExpect(page, 'Reply with the single word LANTERN.', 'lantern');
    await expect(page.locator('#edge-top-models .edge-live-status')).toContainText('Meeting');
  });

  test('usual local mode can show canvas content through explicit local tools', async ({ page }) => {
    await clearLiveChat(sessionToken);
    await openLiveApp(page, sessionToken);
    await waitForLiveAppReady(page);
    await resetCircleRuntimeState(page);

    const before = await page.locator('#chat-history .chat-message.chat-assistant:not(.is-pending)').count();
    await submitPrompt(
      page,
      'Show a text artifact on the canvas titled TOOL TEST with the exact body ORBIT CANVAS. Use tools, then reply with the single word DONE.',
    );
    await waitForAssistantReply(page, before, 'done', 120_000);
    await expect(page.locator('#canvas-text')).toContainText('ORBIT CANVAS', { timeout: 120_000 });
  });
});
