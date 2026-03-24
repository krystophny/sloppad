import { expect, test } from '@playwright/test';

test('hotword management lists downloadable wake words and activates an installed model', async ({ page }) => {
  await page.goto('/tests/playwright/manage-harness.html');

  await expect(page.getByRole('heading', { name: 'Hotword' })).toBeVisible();
  await expect(page.locator('strong', { hasText: 'Computer V2' })).toBeVisible();
  await page.getByRole('button', { name: 'Download' }).click();
  await expect(page.getByText('Downloaded Computer V2.')).toBeVisible();
  const computerRow = page.locator('.manage-row').filter({ has: page.locator('strong', { hasText: 'Computer V2' }) });
  await expect(computerRow.getByRole('button', { name: 'Use' })).toBeVisible();

  await computerRow.getByRole('button', { name: 'Use' }).click();
  await expect(page.getByText('Activated Computer V2. Clients will reload revision rev-2.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Active' })).toBeVisible();
  await expect(page.getByText('Computer V2 · computer · Home Assistant Community')).toBeVisible();
});

test('hotword management filter narrows the catalog list', async ({ page }) => {
  await page.goto('/tests/playwright/manage-harness.html');

  await page.getByPlaceholder('computer, jarvis, wake word').fill('computer');
  await expect(page.locator('strong', { hasText: 'Computer V2' })).toBeVisible();
});
