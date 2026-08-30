import assert from 'node:assert/strict';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

import {
  parseArguments,
  readServerState,
  shutdownServer,
  validateConfig,
  waitForReady,
} from '../scripts/geodata-dev.mjs';

test('parseArguments accepts an explicit local config', () => {
  const configPath = path.resolve('config.json');
  assert.deepEqual(parseArguments(['dev', '--config', 'config.json']), { command: 'dev', configPath });
  assert.throws(() => parseArguments(['dev', '--unknown']), /usage/);
});

test('validateConfig requires explicit paths and an existing working directory', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'geodata-dev-'));
  const config = validateConfig({
    database: path.join(root, 'data.duckdb'),
    runtimeDir: path.join(root, 'runtime'),
    backupDir: path.join(root, 'backups'),
    workingDir: root,
    webPort: 8788,
  }, path.join(root, 'geodata-serve.local.json'));

  assert.equal(config.webPort, 8788);
  assert.equal(config.workingDir, root);
  assert.throws(() => validateConfig({ ...config, database: 'relative.duckdb' }, 'config.json'), /absolute path/);
});

test('waitForReady accepts only the state record from the started service', async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'geodata-ready-'));
  const statePath = path.join(root, 'server.json');
  fs.writeFileSync(statePath, JSON.stringify({ address: 'http://127.0.0.1:1', token: 'secret', pid: 42 }));

  const state = await waitForReady({
    statePath,
    expectedPID: 42,
    timeoutMs: 100,
    probe: async (candidate) => candidate.pid === 42,
  });

  assert.equal(state.pid, 42);
  fs.writeFileSync(statePath, '{not-json');
  assert.equal(readServerState(statePath), null);
});

test('shutdownServer uses the server token and public shutdown endpoint', async () => {
  const server = http.createServer((request, response) => {
    assert.equal(request.method, 'POST');
    assert.equal(request.url, '/shutdown');
    assert.equal(request.headers.authorization, 'Bearer test-token');
    response.writeHead(202, { 'content-type': 'application/json' });
    response.end(JSON.stringify({ status: 'shutting_down' }));
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  try {
    const address = `http://127.0.0.1:${server.address().port}`;
    await shutdownServer({ address, token: 'test-token', pid: 1 });
  } finally {
    await new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
  }
});
