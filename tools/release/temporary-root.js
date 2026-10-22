const fs = require('node:fs');
const path = require('node:path');
const { repoRoot } = require('./package-layout');

// Set only this invocation's environment so native child tools use the same
// repository-local scratch root as the smoke scripts and CI log collector.
function prepareTemporaryRoot() {
  const root = path.join(repoRoot, 'trash', 'verification', 'release', 'tmp');
  fs.mkdirSync(root, { recursive: true });
  process.env.TEMP = root;
  process.env.TMP = root;
  process.env.TMPDIR = root;
  return root;
}

module.exports = { prepareTemporaryRoot };
