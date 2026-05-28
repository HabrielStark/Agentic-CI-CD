#!/usr/bin/env node
/**
 * Post-build verification for the ReproForge GitHub Action.
 *
 * The action runs the compiled `dist/index.js` directly on the runner
 * (no install step), so this guards against shipping a broken bundle:
 *   - the file exists and is non-empty;
 *   - it carries an executable shebang for `node20`;
 *   - it still exposes the `--self-test` hook used by CI.
 */
'use strict';

const fs = require('node:fs');
const path = require('node:path');

const distEntry = path.resolve(__dirname, '..', 'dist', 'index.js');

if (!fs.existsSync(distEntry)) {
    console.error(`postbuild: missing compiled entrypoint ${distEntry}`);
    process.exit(1);
}

let source = fs.readFileSync(distEntry, 'utf8');

if (source.trim().length === 0) {
    console.error('postbuild: compiled entrypoint is empty');
    process.exit(1);
}

const shebang = '#!/usr/bin/env node\n';
if (!source.startsWith('#!')) {
    source = shebang + source;
    fs.writeFileSync(distEntry, source);
}

if (!source.includes('--self-test')) {
    console.error('postbuild: compiled entrypoint is missing the --self-test hook');
    process.exit(1);
}

console.log(`postbuild: dist/index.js ready (${Buffer.byteLength(source)} bytes)`);
