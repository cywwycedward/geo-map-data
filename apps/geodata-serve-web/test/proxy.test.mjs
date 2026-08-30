import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { createApp } from '../server.mjs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const STATIC_DIR = path.join(__dirname, '..', 'static');

function tmpdir(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'gdsw-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}

function writeServerState(dir, state) {
  fs.writeFileSync(path.join(dir, 'server.json'), JSON.stringify(state));
}

function listen(server) {
  return new Promise((resolve) => server.listen(0, '127.0.0.1', () => resolve(server.address().port)));
}

function closeServer(server) {
  return new Promise((resolve) => server.close(resolve));
}

function request(port, { method = 'GET', path: p, body, headers = {} } = {}) {
  return new Promise((resolve, reject) => {
    const req = http.request({ host: '127.0.0.1', port, method, path: p, headers }, (res) => {
      const chunks = [];
      res.on('data', (c) => chunks.push(c));
      res.on('end', () => resolve({ status: res.statusCode, headers: res.headers, body: Buffer.concat(chunks).toString('utf8') }));
    });
    req.on('error', reject);
    if (body != null) req.write(body);
    req.end();
  });
}

function waitFor(fn, timeoutMs = 2000) {
  return new Promise((resolve, reject) => {
    const started = Date.now();
    const timer = setInterval(() => {
      if (fn()) {
        clearInterval(timer);
        resolve();
      } else if (Date.now() - started > timeoutMs) {
        clearInterval(timer);
        reject(new Error('timed out waiting for condition'));
      }
    }, 10);
  });
}

// In-process mock of the geodata-serve HTTP interface. It records what the
// proxy sent (especially Authorization) and responds like the real service.
function startMockGeodata(t, opts = {}) {
  const token = opts.token ?? 'secret-token';
  const observations = [];
  const server = http.createServer((req, res) => {
    const obs = {
      method: req.method,
      url: req.url,
      authorization: req.headers.authorization ?? null,
      contentType: req.headers['content-type'] ?? null,
      closed: false,
    };
    observations.push(obs);
    req.on('close', () => { obs.closed = true; });

    const pathname = (req.url.split('?')[0] || '');
    const authorized = req.headers.authorization === `Bearer ${token}`;

    if (pathname === '/health') {
      res.writeHead(200, { 'content-type': 'application/json; charset=utf-8' });
      res.end(JSON.stringify({ status: 'ok', interface_version: 1, service_version: '0.1.0', duckdb_version: '1.4.5', pid: 123 }));
      return;
    }
    if (!authorized) {
      res.writeHead(401, { 'content-type': 'application/json; charset=utf-8' });
      res.end(JSON.stringify({ error: { code: 'unauthorized', message: 'authorization required' } }));
      return;
    }
    if (pathname === '/execute' && req.method === 'POST') {
      res.writeHead(200, {
        'content-type': 'application/x-ndjson; charset=utf-8',
        'x-request-id': 'req_abc',
        'cache-control': 'no-store',
      });
      const line = (obj) => res.write(JSON.stringify(obj) + '\n');
      line({ type: 'status', request_id: 'req_abc', state: 'queued', at: '2026-01-01T00:00:00Z' });
      line({ type: 'status', request_id: 'req_abc', state: 'running', at: '2026-01-01T00:00:00Z' });
      if (!opts.hangExecute) {
        line({ type: 'schema', request_id: 'req_abc', columns: [{ name: 'name', duckdb_type: 'VARCHAR' }] });
        line({ type: 'row', request_id: 'req_abc', values: ['x'] });
        line({ type: 'summary', request_id: 'req_abc', state: 'finished', row_count: 1, queued_ms: 0, execution_ms: 1 });
      }
      if (opts.hangExecute) {
        // never end; the client must abort, which should close this request
        return;
      }
      res.end();
      return;
    }
    if (pathname.startsWith('/requests/') && req.method === 'GET') {
      res.writeHead(200, { 'content-type': 'application/json; charset=utf-8' });
      res.end(JSON.stringify({ request_id: pathname.slice('/requests/'.length), mode: 'read', state: 'finished' }));
      return;
    }
    if (pathname === '/shutdown' && req.method === 'POST') {
      res.writeHead(202, { 'content-type': 'application/json; charset=utf-8' });
      res.end(JSON.stringify({ status: 'shutting_down' }));
      return;
    }
    res.writeHead(404, { 'content-type': 'application/json; charset=utf-8' });
    res.end(JSON.stringify({ error: { code: 'not_found', message: 'not found' } }));
  });
  const started = listen(server).then((port) => {
    const address = `http://127.0.0.1:${port}`;
    return { address, port, observations, server, close: () => closeServer(server) };
  });
  t.after(async () => closeServer(server));
  return started;
}

test('forwards /api/health without any Authorization header', async (t) => {
  const mock = await startMockGeodata(t);
  const dir = tmpdir(t);
  writeServerState(dir, { address: mock.address, token: 'secret-token', pid: 123, database: '/tmp/x.duckdb', started_at: '2026-01-01T00:00:00Z', interface_version: 1 });
  const app = createApp({ runtimeDir: dir, staticDir: STATIC_DIR });
  const port = await listen(app);
  t.after(() => closeServer(app));

  const res = await request(port, { path: '/api/health' });
  assert.equal(res.status, 200);
  assert.equal(JSON.parse(res.body).status, 'ok');
  const obs = mock.observations.find((o) => o.url.startsWith('/health'));
  assert.equal(obs.authorization, null);
});

test('/api/config returns discovery fields and never leaks the token', async (t) => {
  const mock = await startMockGeodata(t);
  const dir = tmpdir(t);
  writeServerState(dir, { address: mock.address, token: 'secret-token', pid: 123, database: '/tmp/x.duckdb', started_at: '2026-01-01T00:00:00Z', interface_version: 1 });
  const app = createApp({ runtimeDir: dir, staticDir: STATIC_DIR });
  const port = await listen(app);
  t.after(() => closeServer(app));

  const res = await request(port, { path: '/api/config' });
  assert.equal(res.status, 200);
  const body = JSON.parse(res.body);
  assert.equal(body.address, mock.address);
  assert.equal(body.pid, 123);
  assert.equal(body.database, '/tmp/x.duckdb');
  assert.equal(body.token_set, true);
  assert.equal('token' in body, false);
});

test('/api/config returns 404 when server.json is missing', async (t) => {
  const dir = tmpdir(t);
  const app = createApp({ runtimeDir: dir, staticDir: STATIC_DIR });
  const port = await listen(app);
  t.after(() => closeServer(app));

  const res = await request(port, { path: '/api/config' });
  assert.equal(res.status, 404);
  assert.equal(JSON.parse(res.body).error.code, 'no_server_state');
});

test('forwards /api/execute with Bearer token and streams NDJSON verbatim', async (t) => {
  const mock = await startMockGeodata(t);
  const dir = tmpdir(t);
  writeServerState(dir, { address: mock.address, token: 'secret-token' });
  const app = createApp({ runtimeDir: dir, staticDir: STATIC_DIR });
  const port = await listen(app);
  t.after(() => closeServer(app));

  const res = await request(port, {
    method: 'POST',
    path: '/api/execute',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ mode: 'read', sql: 'SELECT 1' }),
  });
  assert.equal(res.status, 200);
  assert.equal(res.headers['content-type'], 'application/x-ndjson; charset=utf-8');
  assert.equal(res.headers['x-request-id'], 'req_abc');
  const lines = res.body.trim().split('\n').map((l) => JSON.parse(l));
  assert.equal(lines.length, 5);
  assert.equal(lines[0].type, 'status');
  assert.equal(lines[4].type, 'summary');
  const obs = mock.observations.find((o) => o.url.startsWith('/execute'));
  assert.equal(obs.authorization, 'Bearer secret-token');
  assert.equal(obs.contentType, 'application/json');
});

test('?skip_auth=1 omits Authorization so execute returns 401', async (t) => {
  const mock = await startMockGeodata(t);
  const dir = tmpdir(t);
  writeServerState(dir, { address: mock.address, token: 'secret-token' });
  const app = createApp({ runtimeDir: dir, staticDir: STATIC_DIR });
  const port = await listen(app);
  t.after(() => closeServer(app));

  const res = await request(port, {
    method: 'POST',
    path: '/api/execute?skip_auth=1',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ mode: 'read', sql: 'SELECT 1' }),
  });
  assert.equal(res.status, 401);
  assert.equal(JSON.parse(res.body).error.code, 'unauthorized');
  const obs = mock.observations.find((o) => o.url.startsWith('/execute'));
  assert.equal(obs.authorization, null);
});

test('forwards /api/requests/{id} with token', async (t) => {
  const mock = await startMockGeodata(t);
  const dir = tmpdir(t);
  writeServerState(dir, { address: mock.address, token: 'secret-token' });
  const app = createApp({ runtimeDir: dir, staticDir: STATIC_DIR });
  const port = await listen(app);
  t.after(() => closeServer(app));

  const res = await request(port, { path: '/api/requests/req_zzz' });
  assert.equal(res.status, 200);
  assert.equal(JSON.parse(res.body).request_id, 'req_zzz');
  const obs = mock.observations.find((o) => o.url.startsWith('/requests/'));
  assert.equal(obs.authorization, 'Bearer secret-token');
});

test('forwards /api/shutdown with token', async (t) => {
  const mock = await startMockGeodata(t);
  const dir = tmpdir(t);
  writeServerState(dir, { address: mock.address, token: 'secret-token' });
  const app = createApp({ runtimeDir: dir, staticDir: STATIC_DIR });
  const port = await listen(app);
  t.after(() => closeServer(app));

  const res = await request(port, { method: 'POST', path: '/api/shutdown' });
  assert.equal(res.status, 202);
  assert.equal(JSON.parse(res.body).status, 'shutting_down');
  const obs = mock.observations.find((o) => o.url.startsWith('/shutdown'));
  assert.equal(obs.authorization, 'Bearer secret-token');
});

test('forwarding endpoints return 502 when server.json is missing', async (t) => {
  const dir = tmpdir(t);
  const app = createApp({ runtimeDir: dir, staticDir: STATIC_DIR });
  const port = await listen(app);
  t.after(() => closeServer(app));

  const res = await request(port, { path: '/api/health' });
  assert.equal(res.status, 502);
  assert.equal(JSON.parse(res.body).error.code, 'no_server_state');
});

test('/api/backups lists directories with a verified flag', async (t) => {
  const mock = await startMockGeodata(t);
  const dir = tmpdir(t);
  writeServerState(dir, { address: mock.address, token: 'secret-token' });
  const backupDir = path.join(dir, 'backups');
  fs.mkdirSync(path.join(backupDir, '20260101T000000000000000Z-req_aaa'), { recursive: true });
  fs.mkdirSync(path.join(backupDir, '20260102T000000000000000Z-req_bbb'), { recursive: true });
  fs.writeFileSync(path.join(backupDir, '20260101T000000000000000Z-req_aaa', '.geodata-serve-verified'), 'verified\n');

  const app = createApp({ runtimeDir: dir, backupDir, staticDir: STATIC_DIR });
  const port = await listen(app);
  t.after(() => closeServer(app));

  const res = await request(port, { path: '/api/backups' });
  assert.equal(res.status, 200);
  const body = JSON.parse(res.body);
  assert.equal(body.count, 2);
  const verified = body.backups.find((b) => b.name.endsWith('req_aaa'));
  const unverified = body.backups.find((b) => b.name.endsWith('req_bbb'));
  assert.equal(verified.verified, true);
  assert.equal(unverified.verified, false);
});

test('/api/backups returns 404 when backup-dir is not configured', async (t) => {
  const dir = tmpdir(t);
  writeServerState(dir, { address: 'http://127.0.0.1:1', token: 'secret-token' });
  const app = createApp({ runtimeDir: dir, staticDir: STATIC_DIR });
  const port = await listen(app);
  t.after(() => closeServer(app));

  const res = await request(port, { path: '/api/backups' });
  assert.equal(res.status, 404);
  assert.equal(JSON.parse(res.body).error.code, 'backup_dir_unset');
});

test('/api/restore rejects a backup name that escapes the backup dir', async (t) => {
  const mock = await startMockGeodata(t);
  const dir = tmpdir(t);
  writeServerState(dir, { address: mock.address, token: 'secret-token', database: '/tmp/db.duckdb' });
  const backupDir = path.join(dir, 'backups');
  fs.mkdirSync(backupDir, { recursive: true });
  const app = createApp({ runtimeDir: dir, backupDir, geodataServeBin: '/bin/false', staticDir: STATIC_DIR });
  const port = await listen(app);
  t.after(() => closeServer(app));

  const res = await request(port, {
    method: 'POST',
    path: '/api/restore',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ backup: '../evil' }),
  });
  assert.equal(res.status, 400);
});

test('/api/restore runs the configured binary and relays output', async (t) => {
  const mock = await startMockGeodata(t);
  const dir = tmpdir(t);
  writeServerState(dir, { address: mock.address, token: 'secret-token', database: '/tmp/db.duckdb' });
  const backupDir = path.join(dir, 'backups');
  fs.mkdirSync(path.join(backupDir, '20260101T000000000000000Z-req_aaa'), { recursive: true });

  let captured = null;
  const app = createApp({
    runtimeDir: dir,
    backupDir,
    geodataServeBin: '/opt/geodata-serve',
    staticDir: STATIC_DIR,
    runProcess: async (bin, args) => {
      captured = { bin, args };
      return { exitCode: 0, stdout: 'RESTORED', stderr: '' };
    },
  });
  const port = await listen(app);
  t.after(() => closeServer(app));

  const res = await request(port, {
    method: 'POST',
    path: '/api/restore',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ backup: '20260101T000000000000000Z-req_aaa' }),
  });
  assert.equal(res.status, 200);
  const body = JSON.parse(res.body);
  assert.equal(body.ok, true);
  assert.equal(body.exit_code, 0);
  assert.equal(body.stdout, 'RESTORED');
  assert.equal(captured.bin, '/opt/geodata-serve');
  assert.deepEqual(captured.args, [
    'restore', '--database', '/tmp/db.duckdb', '--runtime-dir', dir, '--backup',
    path.join(backupDir, '20260101T000000000000000Z-req_aaa'),
  ]);
});

test('/api/restore uses the configured database after the service state is removed', async (t) => {
  const dir = tmpdir(t);
  const backupDir = path.join(dir, 'backups');
  const databasePath = path.join(dir, 'data.duckdb');
  const backup = '20260101T000000000000000Z-req_aaa';
  fs.mkdirSync(path.join(backupDir, backup), { recursive: true });

  let captured = null;
  const app = createApp({
    runtimeDir: dir,
    backupDir,
    databasePath,
    geodataServeBin: '/opt/geodata-serve',
    staticDir: STATIC_DIR,
    runProcess: async (bin, args) => {
      captured = { bin, args };
      return { exitCode: 0, stdout: 'RESTORED', stderr: '' };
    },
  });
  const port = await listen(app);
  t.after(() => closeServer(app));

  const res = await request(port, {
    method: 'POST',
    path: '/api/restore',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ backup }),
  });
  assert.equal(res.status, 200);
  assert.deepEqual(captured.args, [
    'restore', '--database', databasePath, '--runtime-dir', dir, '--backup', path.join(backupDir, backup),
  ]);
});

test('/api/restore reports failure when the binary exits non-zero', async (t) => {
  const mock = await startMockGeodata(t);
  const dir = tmpdir(t);
  writeServerState(dir, { address: mock.address, token: 'secret-token', database: '/tmp/db.duckdb' });
  const backupDir = path.join(dir, 'backups');
  fs.mkdirSync(path.join(backupDir, '20260101T000000000000000Z-req_aaa'), { recursive: true });

  const app = createApp({
    runtimeDir: dir,
    backupDir,
    geodataServeBin: '/opt/geodata-serve',
    staticDir: STATIC_DIR,
    runProcess: async () => ({ exitCode: 1, stdout: '', stderr: 'boom' }),
  });
  const port = await listen(app);
  t.after(() => closeServer(app));

  const res = await request(port, {
    method: 'POST',
    path: '/api/restore',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ backup: '20260101T000000000000000Z-req_aaa' }),
  });
  assert.equal(res.status, 200);
  const body = JSON.parse(res.body);
  assert.equal(body.ok, false);
  assert.equal(body.exit_code, 1);
  assert.equal(body.stderr, 'boom');
});

test('client disconnect aborts the upstream execute request', async (t) => {
  const mock = await startMockGeodata(t, { hangExecute: true });
  const dir = tmpdir(t);
  writeServerState(dir, { address: mock.address, token: 'secret-token' });
  const app = createApp({ runtimeDir: dir, staticDir: STATIC_DIR });
  const port = await listen(app);
  t.after(() => closeServer(app));

  await new Promise((resolve, reject) => {
    const req = http.request(
      { host: '127.0.0.1', port, method: 'POST', path: '/api/execute', headers: { 'content-type': 'application/json' } },
      (res) => {
        res.once('data', () => {
          // abort the client mid-stream; the proxy must destroy upstream
          req.destroy();
        });
        res.on('end', resolve);
        res.on('error', () => {});
      },
    );
    req.on('error', () => {});
    req.write(JSON.stringify({ mode: 'read', sql: 'SELECT 1' }));
    req.end();
    setTimeout(resolve, 1500);
  });

  await waitFor(() => mock.observations.some((o) => o.url.startsWith('/execute') && o.closed));
});

test('serves the static frontend at /', async (t) => {
  const dir = tmpdir(t);
  writeServerState(dir, { address: 'http://127.0.0.1:1', token: 'secret-token' });
  const app = createApp({ runtimeDir: dir, staticDir: STATIC_DIR });
  const port = await listen(app);
  t.after(() => closeServer(app));

  const res = await request(port, { path: '/' });
  assert.equal(res.status, 200);
  assert.match(res.headers['content-type'], /text\/html/);
});
