import fs from 'node:fs';
import path from 'node:path';
import YAML from 'yaml';

const workflowPath = path.join(process.cwd(), '.github', 'workflows', 'test-reports.yml');
const workflow = YAML.parse(fs.readFileSync(workflowPath, 'utf8'));
const errors = [];

if (workflow?.name !== 'Test Reports') {
  errors.push(`expected workflow name "Test Reports", got ${JSON.stringify(workflow?.name)}`);
}

const pullRequest = workflow?.on?.pull_request;
const pushBranches = workflow?.on?.push?.branches;
if (pullRequest === undefined) {
  errors.push('expected pull_request trigger');
}
if (!Array.isArray(pushBranches) || !pushBranches.includes('main')) {
  errors.push('expected push trigger for main');
}

const steps = workflow?.jobs?.reports?.steps;
if (!Array.isArray(steps)) {
  errors.push('expected jobs.reports.steps array');
}

function findStep(name) {
  return steps.find((step) => step?.name === name);
}

const reportStep = findStep('Generate coverage + E2E reports');
const uploadStep = findStep('Upload test reports');
const expectedGate = "${{ github.event_name == 'push' && github.ref == 'refs/heads/main' }}";
const expectedUploadGate = "${{ github.event_name == 'push' && github.ref == 'refs/heads/main' && always() }}";

if (reportStep?.if !== expectedGate) {
  errors.push(`expected report step gate ${expectedGate}, got ${JSON.stringify(reportStep?.if)}`);
}
if (uploadStep?.if !== expectedUploadGate) {
  errors.push(`expected upload step gate ${expectedUploadGate}, got ${JSON.stringify(uploadStep?.if)}`);
}

function requireFlowJob({ jobName, runsOn, stepName, scriptCommand }) {
  const job = workflow?.jobs?.[jobName];
  if (!job) {
    errors.push(`expected ${jobName} job`);
    return;
  }
  if (job['runs-on'] !== runsOn) {
    errors.push(`expected ${jobName} to run on ${runsOn}, got ${JSON.stringify(job['runs-on'])}`);
  }
  const jobSteps = Array.isArray(job.steps) ? job.steps : [];
  if (!jobSteps.some((step) => step?.name === stepName)) {
    errors.push(`${jobName} must include the "${stepName}" step`);
  }
  const runCommands = jobSteps.map((step) => step?.run).filter(Boolean);
  if (!runCommands.some((cmd) => cmd.includes(scriptCommand))) {
    errors.push(`${jobName} must invoke ${scriptCommand}`);
  }
}

requireFlowJob({
  jobName: 'android-flow-contract',
  runsOn: 'ubuntu-latest',
  stepName: 'Run Android shared flow contract',
  scriptCommand: 'npm run test:flows:android:contract:jvm',
});

requireFlowJob({
  jobName: 'ios-flow-contract',
  runsOn: 'macos-latest',
  stepName: 'Run iOS shared flow contract',
  scriptCommand: 'npm run test:flows:ios:contract',
});

if (errors.length > 0) {
  for (const err of errors) {
    console.error(`[workflow-check] ${err}`);
  }
  process.exit(1);
}

console.log('[workflow-check] Test Reports gating is limited to push events on main, with required iOS and Android flow contract jobs.');
