// Generates src/generated/protocol.ts from schema/protocol.schema.json, which the daemon generates
// from its Go structs. With --check it only reports whether the committed file is stale.
import { compile } from 'json-schema-to-typescript';
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const schemaPath = join(root, '..', 'schema', 'protocol.schema.json');
const outPath = join(root, 'src', 'generated', 'protocol.ts');

const schema = JSON.parse(readFileSync(schemaPath, 'utf8'));

// "Error" would shadow the global Error class wherever it is imported.
schema.$defs.Error.title = 'ErrorBody';

// The Go side titles each "exactly one of" branch (selector, text, ...). Those titles would become
// TypeScript types that collide with DOM globals such as Text, so the branches stay anonymous.
(function stripBranchTitles(node) {
  if (Array.isArray(node)) return node.forEach(stripBranchTitles);
  if (node === null || typeof node !== 'object') return;
  for (const key of ['oneOf', 'anyOf']) {
    for (const branch of node[key] ?? []) delete branch.title;
  }
  Object.values(node).forEach(stripBranchTitles);
})(schema);

const ts = await compile(schema, 'BrowserBridgeProtocol', {
  bannerComment: '/* Generated from schema/protocol.schema.json by scripts/gen-types.mjs. Do not edit. */',
  unreachableDefinitions: true,
  additionalProperties: false,
  style: { singleQuote: true },
});

if (process.argv.includes('--check')) {
  const current = readFileSync(outPath, 'utf8').replace(/\r\n/g, '\n');
  if (current !== ts) {
    console.error('src/generated/protocol.ts is stale: run npm run gen');
    process.exit(1);
  }
  console.log('src/generated/protocol.ts is up to date');
} else {
  writeFileSync(outPath, ts);
  console.log('wrote', outPath);
}
