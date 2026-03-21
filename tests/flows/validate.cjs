const { loadFlowsSync } = require('./flow-loader.cjs');

const flows = loadFlowsSync();
console.log(`Validated ${flows.length} flow files.`);
