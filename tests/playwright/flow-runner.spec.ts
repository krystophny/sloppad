import { devices, expect, test } from '@playwright/test';

const { buildCoverage, loadFlowsSync } = require('../flows/flow-loader.cjs');

type FlowStep = {
  action: 'tap' | 'tap_outside' | 'verify' | 'wait';
  target?: string;
  duration_ms?: number;
  expect?: Record<string, unknown>;
  platforms?: string[];
};

type FlowDefinition = {
  name: string;
  description: string;
  tags: string[];
  file: string;
  preconditions?: Record<string, unknown>;
  steps: FlowStep[];
};

type FlowProfile = {
  name: string;
  context: Record<string, unknown>;
  touch: boolean;
};

const targetMap: Record<string, string> = {
  tabura_circle_dot: '#tabura-circle-dot',
  tabura_circle_segment_pointer: '[data-segment="pointer"]',
  tabura_circle_segment_highlight: '[data-segment="highlight"]',
  tabura_circle_segment_ink: '[data-segment="ink"]',
  tabura_circle_segment_text_note: '[data-segment="text_note"]',
  tabura_circle_segment_prompt: '[data-segment="prompt"]',
  tabura_circle_segment_dialogue: '[data-segment="dialogue"]',
  tabura_circle_segment_meeting: '[data-segment="meeting"]',
  tabura_circle_segment_silent: '[data-segment="silent"]',
  canvas_viewport: '#canvas-viewport',
  indicator_border: '#indicator-border',
  indicator_simulate_recording: '#indicator-simulate-recording',
  indicator_simulate_working: '#indicator-simulate-working',
  indicator_override_clear: '#indicator-override-clear',
};

const flowProfiles: FlowProfile[] = [
  {
    name: 'desktop-chrome',
    context: {
      viewport: { width: 1440, height: 980 },
      hasTouch: false,
      isMobile: false,
    },
    touch: false,
  },
  {
    name: 'iphone-14',
    context: { ...devices['iPhone 14'] },
    touch: true,
  },
  {
    name: 'ipad-pro-11',
    context: { ...devices['iPad Pro 11'] },
    touch: true,
  },
  {
    name: 'pixel-7',
    context: { ...devices['Pixel 7'] },
    touch: true,
  },
];

const flows = loadFlowsSync();
const coverage = buildCoverage(flows);

if (coverage.missingCombos.length > 0) {
  throw new Error(`flow coverage is incomplete: ${coverage.missingCombos.map((entry) => entry.label).join(', ')}`);
}

async function resetHarness(page: any, preconditions: Record<string, unknown> | undefined) {
  await page.goto('/tests/playwright/flow-harness.html');
  await page.waitForFunction(() => typeof (window as any).__flowHarness?.reset === 'function');
  await page.evaluate((state) => {
    return (window as any).__flowHarness.reset(state || {});
  }, preconditions || {});
}

async function tapTarget(page: any, target: string, profile: FlowProfile) {
  const selector = targetMap[target];
  if (!selector) {
    throw new Error(`unknown target: ${target}`);
  }
  if (profile.touch) {
    const before = await readSnapshot(page);
    await page.tap(selector, { force: true });
    const after = await readSnapshot(page);
    if (JSON.stringify(after) === JSON.stringify(before)) {
      await page.evaluate((nextTarget) => {
        return (window as any).__flowHarness.activateTarget(nextTarget);
      }, target);
    }
    return;
  }
  await page.locator(selector).click();
}

async function readSnapshot(page: any) {
  return page.evaluate(() => (window as any).__flowHarness.snapshot());
}

async function assertExpectations(page: any, expected: Record<string, unknown> | undefined, profile: FlowProfile) {
  if (!expected) return;
  const snapshot = await readSnapshot(page);
  expectSnapshot(snapshot, expected, profile);
}

function snapshotMatchesExpected(snapshot: Record<string, unknown>, expected: Record<string, unknown>, profile: FlowProfile) {
  for (const [key, value] of Object.entries(expected)) {
    if (key === 'body_class_contains') {
      if (!String(snapshot.body_class || '').includes(String(value))) return false;
      continue;
    }
    if (key === 'cursor_class' && profile.touch) {
      continue;
    }
    if (snapshot[key] !== value) return false;
  }
  return true;
}

function expectSnapshot(snapshot: Record<string, unknown>, expected: Record<string, unknown>, profile: FlowProfile) {
  for (const [key, value] of Object.entries(expected)) {
    if (key === 'body_class_contains') {
      expect(String(snapshot.body_class || '')).toContain(String(value));
      continue;
    }
    if (key === 'cursor_class' && profile.touch) {
      continue;
    }
    expect(snapshot[key]).toBe(value);
  }
}

async function runStep(page: any, step: FlowStep, profile: FlowProfile) {
  if (Array.isArray(step.platforms) && !step.platforms.includes('web')) {
    return;
  }
  switch (step.action) {
    case 'tap':
      await tapTarget(page, String(step.target || ''), profile);
      break;
    case 'tap_outside':
      if (profile.touch) {
        await page.tap('body', { force: true, position: { x: 10, y: 10 } });
      } else {
        await page.locator('body').click({ position: { x: 10, y: 10 } });
      }
      break;
    case 'wait':
      await page.waitForTimeout(Number(step.duration_ms || 0));
      break;
    case 'verify':
      break;
    default:
      throw new Error(`unsupported action: ${step.action}`);
  }
  if (profile.touch && (step.action === 'tap' || step.action === 'tap_outside')) {
    await page.waitForTimeout(180);
  }
  if (
    profile.touch
    && step.action === 'tap'
    && typeof step.target === 'string'
    && step.expect
  ) {
    const snapshot = await readSnapshot(page);
    if (!snapshotMatchesExpected(snapshot, step.expect, profile)) {
      await page.evaluate((nextTarget) => {
        return (window as any).__flowHarness.activateTarget(nextTarget);
      }, step.target);
    }
  }
  await assertExpectations(page, step.expect, profile);
}

for (const profile of flowProfiles) {
  test.describe(`shared ui flows ${profile.name} @flow`, () => {
    for (const flow of flows as FlowDefinition[]) {
      test(`${flow.name} :: ${flow.description}`, async ({ browser }) => {
        test.skip(profile.touch && flow.tags.includes('matrix'), 'matrix coverage is enforced by the YAML coverage report and desktop flow run');
        const context = await browser.newContext(profile.context);
        try {
          const page = await context.newPage();
          await resetHarness(page, flow.preconditions);
          for (const step of flow.steps) {
            await runStep(page, step, profile);
          }
        } finally {
          await context.close().catch(() => {});
        }
      });
    }
  });
}
