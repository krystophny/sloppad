const fs = require('node:fs');
const path = require('node:path');

const { parse } = require('yaml');

const repoRoot = path.resolve(__dirname, '..', '..');
const flowRoot = path.resolve(__dirname);

const toolValues = new Set(['pointer', 'highlight', 'ink', 'text_note', 'prompt']);
const sessionValues = new Set(['none', 'dialogue', 'meeting']);
const indicatorValues = new Set(['idle', 'listening', 'paused', 'recording', 'working']);
const actionValues = new Set(['tap', 'tap_outside', 'verify', 'wait']);
const platformValues = new Set(['web', 'ios', 'android']);
const expectationKeys = new Set([
  'active_tool',
  'session',
  'silent',
  'tabura_circle',
  'dot_inner_icon',
  'body_class_contains',
  'indicator_state',
  'cursor_class',
]);

function fail(message) {
  throw new Error(message);
}

function isPlainObject(value) {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function assertString(value, context) {
  if (typeof value !== 'string' || value.trim() === '') {
    fail(`${context} must be a non-empty string`);
  }
}

function assertEnum(value, allowed, context) {
  if (!allowed.has(value)) {
    fail(`${context} must be one of: ${Array.from(allowed).join(', ')}`);
  }
}

function assertBoolean(value, context) {
  if (typeof value !== 'boolean') {
    fail(`${context} must be a boolean`);
  }
}

function assertArrayOfStrings(values, context, allowed = null) {
  if (!Array.isArray(values) || values.length === 0) {
    fail(`${context} must be a non-empty string array`);
  }
  for (const [index, value] of values.entries()) {
    assertString(value, `${context}[${index}]`);
    if (allowed) {
      assertEnum(value, allowed, `${context}[${index}]`);
    }
  }
}

function validatePreconditions(preconditions, context) {
  if (preconditions == null) return;
  if (!isPlainObject(preconditions)) {
    fail(`${context} must be an object`);
  }
  for (const key of Object.keys(preconditions)) {
    if (!['tool', 'session', 'silent', 'indicator_state'].includes(key)) {
      fail(`${context}.${key} is not supported`);
    }
  }
  if ('tool' in preconditions) {
    assertEnum(preconditions.tool, toolValues, `${context}.tool`);
  }
  if ('session' in preconditions) {
    assertEnum(preconditions.session, sessionValues, `${context}.session`);
  }
  if ('silent' in preconditions) {
    assertBoolean(preconditions.silent, `${context}.silent`);
  }
  if ('indicator_state' in preconditions) {
    assertEnum(preconditions.indicator_state, indicatorValues, `${context}.indicator_state`);
  }
}

function validateExpectations(expect, context) {
  if (!isPlainObject(expect) || Object.keys(expect).length === 0) {
    fail(`${context} must be a non-empty object`);
  }
  for (const [key, value] of Object.entries(expect)) {
    if (!expectationKeys.has(key)) {
      fail(`${context}.${key} is not supported`);
    }
    switch (key) {
      case 'active_tool':
        assertEnum(value, toolValues, `${context}.${key}`);
        break;
      case 'session':
        assertEnum(value, sessionValues, `${context}.${key}`);
        break;
      case 'silent':
        assertBoolean(value, `${context}.${key}`);
        break;
      case 'tabura_circle':
        assertEnum(value, new Set(['expanded', 'collapsed']), `${context}.${key}`);
        break;
      case 'indicator_state':
        assertEnum(value, indicatorValues, `${context}.${key}`);
        break;
      case 'dot_inner_icon':
      case 'body_class_contains':
      case 'cursor_class':
        assertString(value, `${context}.${key}`);
        break;
      default:
        break;
    }
  }
}

function validateStep(step, context) {
  if (!isPlainObject(step)) {
    fail(`${context} must be an object`);
  }
  assertEnum(step.action, actionValues, `${context}.action`);
  if ('platforms' in step) {
    assertArrayOfStrings(step.platforms, `${context}.platforms`, platformValues);
  }
  if (step.action === 'tap') {
    assertString(step.target, `${context}.target`);
  }
  if (step.action === 'wait') {
    if (!Number.isFinite(step.duration_ms) || step.duration_ms < 0) {
      fail(`${context}.duration_ms must be a non-negative number`);
    }
  }
  if ('expect' in step) {
    validateExpectations(step.expect, `${context}.expect`);
  }
}

function validateFlow(flow, relativePath) {
  if (!isPlainObject(flow)) {
    fail(`${relativePath} must contain a single flow object`);
  }
  assertString(flow.name, `${relativePath}.name`);
  assertString(flow.description, `${relativePath}.description`);
  assertArrayOfStrings(flow.tags, `${relativePath}.tags`);
  validatePreconditions(flow.preconditions, `${relativePath}.preconditions`);
  if (!Array.isArray(flow.steps) || flow.steps.length === 0) {
    fail(`${relativePath}.steps must be a non-empty array`);
  }
  flow.steps.forEach((step, index) => validateStep(step, `${relativePath}.steps[${index}]`));
}

function collectFlowFiles(rootDir) {
  const entries = fs.readdirSync(rootDir, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const fullPath = path.join(rootDir, entry.name);
    if (entry.isDirectory()) {
      files.push(...collectFlowFiles(fullPath));
      continue;
    }
    if (entry.isFile() && entry.name.endsWith('.yaml')) {
      files.push(fullPath);
    }
  }
  files.sort();
  return files;
}

function loadFlowsSync() {
  const files = collectFlowFiles(flowRoot);
  const seenNames = new Set();
  const flows = [];
  for (const filePath of files) {
    const text = fs.readFileSync(filePath, 'utf8');
    const parsed = parse(text);
    const relativePath = path.relative(repoRoot, filePath);
    validateFlow(parsed, relativePath);
    if (seenNames.has(parsed.name)) {
      fail(`duplicate flow name: ${parsed.name}`);
    }
    seenNames.add(parsed.name);
    flows.push({
      ...parsed,
      file: relativePath,
    });
  }
  return flows;
}

function comboKey(tool, session, silent) {
  return `${tool}|${session}|${silent ? 'silent' : 'audible'}`;
}

function comboLabel(tool, session, silent) {
  return `${tool} / ${session} / ${silent ? 'silent' : 'audible'}`;
}

function buildCoverage(flows) {
  const combosCovered = new Set();
  const targetsCovered = new Set();
  for (const flow of flows) {
    for (const step of flow.steps) {
      if (typeof step.target === 'string' && step.target.trim() !== '') {
        targetsCovered.add(step.target);
      }
      const expect = step.expect;
      if (
        expect
        && typeof expect.active_tool === 'string'
        && typeof expect.session === 'string'
        && typeof expect.silent === 'boolean'
      ) {
        combosCovered.add(comboKey(expect.active_tool, expect.session, expect.silent));
      }
    }
  }

  const expectedCombos = [];
  for (const tool of toolValues) {
    for (const session of sessionValues) {
      for (const silent of [false, true]) {
        expectedCombos.push({
          key: comboKey(tool, session, silent),
          label: comboLabel(tool, session, silent),
        });
      }
    }
  }

  const missingCombos = expectedCombos.filter((entry) => !combosCovered.has(entry.key));

  return {
    flowCount: flows.length,
    targetsCovered: Array.from(targetsCovered).sort(),
    comboCount: expectedCombos.length,
    combosCovered: Array.from(combosCovered).sort(),
    missingCombos,
  };
}

module.exports = {
  buildCoverage,
  loadFlowsSync,
};
