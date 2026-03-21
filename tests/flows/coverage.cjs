const { buildCoverage, loadFlowsSync } = require('./flow-loader.cjs');

const flows = loadFlowsSync();
const coverage = buildCoverage(flows);

console.log(`Flows: ${coverage.flowCount}`);
console.log(`Mode combinations covered: ${coverage.combosCovered.length}/${coverage.comboCount}`);
console.log(`Targets covered: ${coverage.targetsCovered.join(', ')}`);

if (coverage.missingCombos.length > 0) {
  console.error('Missing mode combinations:');
  for (const combo of coverage.missingCombos) {
    console.error(`- ${combo.label}`);
  }
  process.exitCode = 1;
}
