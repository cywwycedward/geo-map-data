// mock-geodata.mjs — a throwaway stand-in for the real geodata-serve Go
// service, used ONLY to preview the geodata-serve-web UI without building the
// Go binary. It writes a server.json (address + token) and answers the same
// HTTP endpoints with canned NDJSON, including slow queries for exercising the
// cancel/timeout/concurrency flows in the UI.
//
// It does NOT run DuckDB. Do not use it to validate geodata-serve behavior.
//
// Usage: node scripts/mock-geodata.mjs --runtime-dir /tmp/gdsw-runtime

import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

function parseArgs(argv) {
  const options = { runtimeDir: null };
  for (let i = 0; i < argv.length; i += 1) {
    if (argv[i] === '--runtime-dir') options.runtimeDir = argv[++i];
  }
  return options;
}

function json(res, status, body) {
  res.writeHead(status, { 'content-type': 'application/json; charset=utf-8' });
  res.end(JSON.stringify(body));
}

const TOKEN = 'mock-token';

function main() {
  const { runtimeDir } = parseArgs(process.argv.slice(2));
  if (!runtimeDir) {
    console.error('usage: node scripts/mock-geodata.mjs --runtime-dir <path>');
    process.exit(2);
  }
  const absRuntime = path.resolve(runtimeDir);
  fs.mkdirSync(absRuntime, { recursive: true });

  const requests = new Map();
  let closing = false;
  let counter = 0;

  const server = http.createServer((req, res) => {
    const pathname = req.url.split('?')[0];

    if (pathname === '/health') {
      if (closing) return json(res, 503, { status: 'shutting_down', interface_version: 1, service_version: '0.1.0' });
      return json(res, 200, {
        status: 'ok', interface_version: 1, service_version: '0.1.0',
        duckdb_version: '1.4.5', pid: process.pid,
      });
    }

    if (req.headers.authorization !== `Bearer ${TOKEN}`) {
      return json(res, 401, { error: { code: 'unauthorized', message: 'authorization required' } });
    }

    if (pathname === '/shutdown' && req.method === 'POST') {
      closing = true;
      json(res, 202, { status: 'shutting_down' });
      setTimeout(() => server.close(() => process.exit(0)), 200);
      return;
    }

    if (pathname.startsWith('/requests/') && req.method === 'GET') {
      const id = pathname.slice('/requests/'.length);
      const status = requests.get(id);
      if (!status) return json(res, 404, { error: { code: 'request_not_found', message: 'request not found' } });
      return json(res, 200, status);
    }

    if (pathname === '/execute' && req.method === 'POST') {
      let body = '';
      req.on('data', (c) => { body += c; });
      req.on('end', () => {
        if (closing) return json(res, 409, { error: { code: 'service_shutting_down', message: 'service is shutting down' } });
        let parsed = {};
        try { parsed = JSON.parse(body); } catch { return json(res, 400, { error: { code: 'invalid_request', message: 'invalid JSON request' } }); }
        handleExecute(res, parsed, requests, () => counter += 1);
      });
      return;
    }

    json(res, 404, { error: { code: 'not_found', message: 'not found' } });
  });

  server.listen(0, '127.0.0.1', () => {
    const address = `http://127.0.0.1:${server.address().port}`;
    fs.writeFileSync(path.join(absRuntime, 'server.json'), JSON.stringify({
      interface_version: 1,
      pid: process.pid,
      address,
      token: TOKEN,
      database: path.join(absRuntime, 'mock.duckdb'),
      started_at: new Date().toISOString(),
    }, null, 2));
    console.log(`mock geodata-serve on ${address}`);
    console.log(`server.json written to ${path.join(absRuntime, 'server.json')}`);
    console.log('Run: node server.mjs --runtime-dir ' + absRuntime);
  });
}

function handleExecute(res, body, requests, nextId) {
  const mode = body.mode === 'write' ? 'write' : 'read';
  const timeoutSeconds = body.timeout_seconds ? Number(body.timeout_seconds) : null;
  const sql = String(body.sql ?? '');
  const slow = /range\s*\(|SLOW/i.test(sql);
  const failing = /fail|error|nonexistent|not_exist/i.test(sql);
  const id = `req_${String(nextId()).padStart(4, '0')}`;

  res.writeHead(200, { 'content-type': 'application/x-ndjson; charset=utf-8', 'x-request-id': id, 'cache-control': 'no-store' });

  const acceptedAt = new Date();
  const status = { request_id: id, mode, state: 'queued', accepted_at: acceptedAt.toISOString(), started_at: null, finished_at: null, row_count: null, error_code: null };
  requests.set(id, status);

  const write = (event) => res.write(JSON.stringify(event) + '\n');
  const emitState = (state) => {
    status.state = state;
    if (status.started_at === null && state !== 'queued') status.started_at = new Date().toISOString();
    write({ type: 'status', request_id: id, state, at: new Date().toISOString() });
  };

  emitState('queued');
  const duration = slow ? 1500 : 100;
  const startDelay = mode === 'write' ? 50 : 0;

  setTimeout(() => {
    if (mode === 'write') emitState('backing_up');
    emitState('running');
  }, startDelay);

  setTimeout(() => {
    if (failing) {
      status.state = 'failed';
      status.error_code = 'sql_failed';
      status.finished_at = new Date().toISOString();
      requests.set(id, status);
      write({ type: 'error', request_id: id, state: 'failed', code: 'sql_failed', message: 'DuckDB query failed (mock)', queued_ms: 10, execution_ms: 20 });
      return res.end();
    }
    if (timeoutSeconds && slow) {
      status.state = 'cancelled';
      status.error_code = 'deadline_exceeded';
      status.finished_at = new Date().toISOString();
      requests.set(id, status);
      write({ type: 'error', request_id: id, state: 'cancelled', code: 'deadline_exceeded', message: 'request cancelled', queued_ms: 10, execution_ms: timeoutSeconds * 1000 });
      return res.end();
    }
    write({ type: 'schema', request_id: id, columns: [{ name: 'name', duckdb_type: 'VARCHAR' }, { name: 'value', duckdb_type: 'DOUBLE' }] });
    write({ type: 'row', request_id: id, values: ['alpha', 1.5] });
    write({ type: 'row', request_id: id, values: ['beta', 2.5] });
    status.state = 'finished';
    status.row_count = 2;
    status.finished_at = new Date().toISOString();
    requests.set(id, status);
    write({ type: 'summary', request_id: id, state: 'finished', row_count: 2, queued_ms: 10, execution_ms: 30 });
    res.end();
  }, startDelay + duration);
}

main();
