import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const STATIC_DIR = path.join(__dirname, '..', 'static');
const APP_ROOT = path.join(__dirname, '..');

function readStatic(name) {
  return fs.readFileSync(path.join(STATIC_DIR, name), 'utf8');
}

test('frontend scripts are syntactically valid', () => {
  for (const name of ['app.js', 'ndjson.mjs']) {
    const result = spawnSync(process.execPath, ['--check', path.join(STATIC_DIR, name)], { encoding: 'utf8' });
    assert.equal(result.status, 0, `${name} failed syntax check:\n${result.stderr}`);
  }
});

test('index.html wires up the module script and stylesheet', () => {
  const html = readStatic('index.html');
  assert.match(html, /<script type="module" src="\.\/app\.js"><\/script>/);
  assert.match(html, /<link rel="stylesheet" href="\.\/style\.css" \/>/);
});

test('app.js imports the unit-tested NDJSON module', () => {
  const app = readStatic('app.js');
  assert.match(app, /from '\.\/ndjson\.mjs'/);
  assert.match(app, /new NDJSONStream/);
});

test('GeoParquet preset uses DuckDB native parquet reader', () => {
  const app = readStatic('app.js');
  assert.match(app, /read_parquet\('data\/points\.parquet'\)/);
  assert.doesNotMatch(app, /ST_Read\('data\/points\.parquet'\)/);
});

test('frontend exposes guided scenarios with automatic outcome checks', () => {
  const html = readStatic('index.html');
  const app = readStatic('app.js');
  for (const scenario of ['basic-read', 'write-backup', 'sql-error', 'auth-401']) {
    assert.match(html, new RegExp(`data-scenario="${scenario}"`));
    assert.match(app, new RegExp(`['"]${scenario}['"]`));
  }
  assert.match(html, /data-scenario="timeout"/);
  assert.match(app, /timeout:\s*\{/);
  assert.match(app, /function assessScenario\(/);
  assert.match(app, /function setupKeyboardShortcut\(/);
  assert.match(app, /event\.ctrlKey \|\| event\.metaKey/);
});

test('proxy CLI does not offer an external bind address option', () => {
  const result = spawnSync(process.execPath, [
    path.join(APP_ROOT, 'server.mjs'), '--runtime-dir', 'runtime', '--host', '0.0.0.0', '--help',
  ], { encoding: 'utf8' });
  assert.equal(result.status, 2);
  assert.match(result.stderr, /unknown option: --host/);
});
