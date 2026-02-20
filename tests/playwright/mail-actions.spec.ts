import { expect, test, type Page } from '@playwright/test';

type Header = {
  id: string;
  date: string;
  sender: string;
  subject: string;
};

function mailEvent(provider: string, headers: Header[]) {
  return {
    kind: 'text_artifact',
    event_id: `evt-${provider}-${headers.length}`,
    title: 'Mail Headers',
    text: '# Mail Headers',
    meta: {
      producer_mcp_url: 'http://127.0.0.1:8090/mcp',
      message_triage_v1: {
        provider,
        folder: 'INBOX',
        count: headers.length,
        headers,
      },
    },
  };
}

async function renderMail(page: Page, provider: string, headers: Header[]) {
  await page.waitForFunction(() => typeof (window as any).renderHarnessArtifact === 'function');
  await page.evaluate((event) => {
    // @ts-expect-error injected by harness module
    window.renderHarnessArtifact(event);
  }, mailEvent(provider, headers));
}

async function swipeRow(page: Page, selector: string, deltaX: number) {
  const row = page.locator(selector);
  const box = await row.boundingBox();
  if (!box) throw new Error(`missing bounding box for ${selector}`);
  const startX = box.x + box.width / 2;
  const startY = box.y + box.height / 2;
  await page.mouse.move(startX, startY);
  await page.mouse.down();
  await page.mouse.move(startX + deltaX, startY, { steps: 10 });
  await page.mouse.up();
}

test.beforeEach(async ({ page }) => {
  await page.goto('/tests/playwright/harness.html');
});

test('gmail defer includes until_at and shows success state', async ({ page }) => {
  const actionCalls: Array<Record<string, unknown>> = [];

  await page.route('**/api/mail/action-capabilities', async (route) => {
    await route.fulfill({
      json: {
        capabilities: {
          provider: 'gmail',
          supports_open: true,
          supports_archive: true,
          supports_delete_to_trash: true,
          supports_native_defer: true,
        },
      },
    });
  });

  await page.route('**/api/mail/action', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    actionCalls.push(body);
    await route.fulfill({
      json: {
        result: {
          action: body.action,
          message_id: body.message_id,
          status: 'ok',
          effective_provider_mode: 'native',
          deferred_until_at: body.until_at,
        },
      },
    });
  });

  await renderMail(page, 'gmail', [
    { id: 'm1', date: '2026-02-20T09:00:00Z', sender: 'a@example.com', subject: 'One' },
  ]);

  await page.click('tr[data-message-id="m1"] button[data-mail-action="defer"]');
  await page.fill('tr[data-message-id="m1"] [data-mail-defer-input]', '2026-03-10T09:30');
  await page.click('tr[data-message-id="m1"] button[data-mail-action="defer-apply"]');

  await expect.poll(() => actionCalls.length).toBe(1);
  expect(actionCalls[0]?.action).toBe('defer');
  expect(String(actionCalls[0]?.until_at || '')).toContain('2026-03-10T');

  await expect(page.locator('tr[data-message-id="m1"] [data-mail-row-status]')).toContainText('Deferred until');
});

test('imap defer shows stub and sends no mutate call', async ({ page }) => {
  let mutateCalls = 0;

  await page.route('**/api/mail/action-capabilities', async (route) => {
    await route.fulfill({
      json: {
        capabilities: {
          provider: 'imap',
          supports_open: true,
          supports_archive: true,
          supports_delete_to_trash: true,
          supports_native_defer: false,
        },
      },
    });
  });

  await page.route('**/api/mail/action', async (route) => {
    mutateCalls += 1;
    await route.fulfill({ status: 500, body: 'should not be called' });
  });

  await renderMail(page, 'imap', [
    { id: 'm9', date: '2026-02-20T09:00:00Z', sender: 'imap@example.com', subject: 'IMAP' },
  ]);

  await page.click('tr[data-message-id="m9"] button[data-mail-action="defer"]');

  await expect(page.locator('tr[data-message-id="m9"] [data-mail-row-status]')).toContainText('stub');
  await page.waitForTimeout(120);
  expect(mutateCalls).toBe(0);
});

test('swipe thresholds map to archive/delete exactly once', async ({ page }) => {
  const actionCalls: string[] = [];

  await page.route('**/api/mail/action-capabilities', async (route) => {
    await route.fulfill({
      json: {
        capabilities: {
          provider: 'gmail',
          supports_open: true,
          supports_archive: true,
          supports_delete_to_trash: true,
          supports_native_defer: true,
        },
      },
    });
  });

  await page.route('**/api/mail/action', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    actionCalls.push(String(body.action || ''));
    await route.fulfill({
      json: {
        result: {
          action: body.action,
          message_id: body.message_id,
          status: 'ok',
          effective_provider_mode: 'native',
        },
      },
    });
  });

  await renderMail(page, 'gmail', [
    { id: 'm1', date: '2026-02-20T09:00:00Z', sender: 'a@example.com', subject: 'One' },
    { id: 'm2', date: '2026-02-20T08:00:00Z', sender: 'b@example.com', subject: 'Two' },
  ]);

  await swipeRow(page, 'tr[data-message-id="m1"]', -160);
  await expect.poll(() => actionCalls.filter((a) => a === 'archive').length).toBe(1);

  await swipeRow(page, 'tr[data-message-id="m2"]', -320);
  await expect.poll(() => actionCalls.filter((a) => a === 'delete').length).toBe(1);

  expect(actionCalls.filter((a) => a === 'archive')).toHaveLength(1);
  expect(actionCalls.filter((a) => a === 'delete')).toHaveLength(1);
});

test('draft reply shows editable unsent draft and cancel does not trigger action mutate', async ({ page }) => {
  let mutateCalls = 0;

  await page.route('**/api/mail/action-capabilities', async (route) => {
    await route.fulfill({
      json: {
        capabilities: {
          provider: 'gmail',
          supports_open: true,
          supports_archive: true,
          supports_delete_to_trash: true,
          supports_native_defer: true,
        },
      },
    });
  });

  await page.route('**/api/mail/action', async (route) => {
    mutateCalls += 1;
    await route.fulfill({ json: { result: { status: 'ok' } } });
  });

  await page.route('**/api/mail/draft-reply', async (route) => {
    await route.fulfill({
      json: {
        source: 'llm',
        draft_text: 'Hi Alice,\\n\\nThanks for the update.\\n\\nBest,\\nMe',
      },
    });
  });

  await renderMail(page, 'gmail', [
    { id: 'm3', date: '2026-02-20T07:00:00Z', sender: 'Alice <alice@example.com>', subject: 'Status' },
  ]);

  await page.click('tr[data-message-id="m3"] button[data-mail-action="draft-reply"]');
  const draftText = page.locator('[data-mail-draft-panel] [data-mail-draft-text]');
  await expect(draftText).toHaveValue(/Thanks for the update/);

  await page.click('[data-mail-draft-panel] button[data-mail-action="draft-cancel"]');
  await expect(page.locator('[data-mail-draft-panel]')).toBeHidden();
  expect(mutateCalls).toBe(0);
});
