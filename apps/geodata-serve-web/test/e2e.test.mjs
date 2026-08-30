import test from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import http from 'node:http';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');

function get(url) {
  return new Promise((resolve, reject) => {
    http.get(url, (res) => {
      let body = '';
      res.on('data', (c) => { body += c; });
      res.on('end', () => resolve({ status: res.statusCode, headers: res.headers, body }));
    }).on('error', reject);
  });
}

function post(url, json) {
  return new Promise((resolve, reject) => {
    const body = JSON.stringify(json);
    const req = http.request(url, { method: 'POST', headers: { 'content-type': 'application/json' } }, (res) => {
      let out = '';
      res.on('data', (c) => { out += c; });
      res.on('end', () => resolve({ status: res.statusCode, headers: res.headers, body: out }));
    });
    req.on('error', reject);
    req.write(body);
    req.end();
  });
}

async function waitFor(fn, timeoutMs = 8000, intervalMs = 100) {
  const started = Date.now();
  for (;;) {
    try {
      const value = await fn();
      if (value) return value;
    } catch {
      /* retry */
    }
    if (Date.now() - started > timeoutMs) throw new Error('timed out waiting for condition');
    await new Promise((r) => setTimeout(r, intervalMs));
  }
}

test('end-to-end: mock geodata-serve + proxy CLI + static frontend', async (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'gdsw-e2e-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const runtimeDir = path.join(dir, 'runtime');
  const backupDir = path.join(dir, 'backups');

  const mock = spawn(process.execPath, [path.join(ROOT, 'scripts', 'mock-geodata.mjs'), '--runtime-dir', runtimeDir], { stdio: 'ignore' });
  t.after(() => { try { mock.kill(); } catch { /* ignore */ } });
  await waitFor(() => fs.existsSync(path.join(runtimeDir, 'server.json')));

  const proxy = spawn(process.execPath, [path.join(ROOT, 'server.mjs'), '--runtime-dir', runtimeDir, '--backup-dir', backupDir, '--port', '0'], { stdio: ['ignore', 'pipe', 'pipe'] });
  t.after(() => { try { proxy.kill(); } catch { /* ignore */ } });

  const port = await waitFor(() => new Promise((resolve) => {
    let out = '';
    proxy.stdout.on('data', (c) => {
      out += c.toString('utf8');
      const match = out.match(/listening on http:\/\/127\.0\.0\.1:(\d+)/);
      if (match) resolve(Number(match[1]));
    });
  }), 8000);

  const base = `http://127.0.0.1:${port}`;

  const cfg = await waitFor(() => get(`${base}/api/config`));
  assert.equal(cfg.status, 200);
  assert.equal(JSON.parse(cfg.body).token_set, true);
  assert.equal('token' in JSON.parse(cfg.body), false);

  const health = await get(`${base}/api/health`);
  assert.equal(health.status, 200);
  assert.equal(JSON.parse(health.body).status, 'ok');

  const execute = await post(`${base}/api/execute`, { mode: 'read', sql: 'SELECT 1' });
  assert.equal(execute.status, 200);
  assert.match(execute.headers['content-type'], /ndjson/);
  const lines = execute.body.trim().split('\n').map((l) => JSON.parse(l));
  assert.equal(lines[0].type, 'status');
  assert.equal(lines[lines.length - 1].type, 'summary');

  const slowStartedAt = Date.now();
  const timeout = await post(`${base}/api/execute`, {
    mode: 'read',
    sql: 'SELECT * FROM range(0, 2)',
    timeout_seconds: 1,
  });
  const elapsed = Date.now() - slowStartedAt;
  assert.equal(timeout.status, 200);
  assert.ok(elapsed >= 1200, `mock timeout returned too quickly: ${elapsed}ms`);
  const timeoutEvents = timeout.body.trim().split('\n').map((line) => JSON.parse(line));
  assert.deepEqual(timeoutEvents.at(-1), {
    type: 'error',
    request_id: timeoutEvents[0].request_id,
    state: 'cancelled',
    code: 'deadline_exceeded',
    message: 'request cancelled',
    queued_ms: 10,
    execution_ms: 1000,
  });

  // Static assets must be served with module-friendly MIME types.
  const appJs = await get(`${base}/app.js`);
  assert.equal(appJs.status, 200);
  assert.match(appJs.headers['content-type'], /text\/javascript/);
  const ndjson = await get(`${base}/ndjson.mjs`);
  assert.equal(ndjson.status, 200);
  assert.match(ndjson.headers['content-type'], /text\/javascript/);
  const css = await get(`${base}/style.css`);
  assert.equal(css.status, 200);
  assert.match(css.headers['content-type'], /text\/css/);

  const shutdown = await post(`${base}/api/shutdown`, {});
  assert.equal(shutdown.status, 202);
});
