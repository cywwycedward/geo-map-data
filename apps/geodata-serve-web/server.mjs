// geodata-serve-web — a tiny local test harness for the geodata-serve HTTP
// interface. It serves a vanilla JS frontend and acts as a same-origin reverse
// proxy to the running geodata-serve process, reading the address + token from
// its `server.json` discovery file.
//
// Why a proxy: geodata-serve binds 127.0.0.1 with no CORS headers, and a
// browser cannot read the local `server.json`. A same-origin proxy solves both
// without modifying the service under test, and streams NDJSON back verbatim so
// cancellation (client abort) and timeout behavior can be observed.
//
// Standard library only. Run: node server.mjs --runtime-dir <path>

import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const MAX_BODY_BYTES = 8 * 1024 * 1024;

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.ico': 'image/x-icon',
  '.txt': 'text/plain; charset=utf-8',
};

export function createApp(config) {
  const ctx = {
    runtimeDir: path.resolve(config.runtimeDir),
    statePath: path.join(path.resolve(config.runtimeDir), 'server.json'),
    staticDir: path.resolve(config.staticDir),
    backupDir: config.backupDir ? path.resolve(config.backupDir) : null,
    databasePath: config.databasePath ? path.resolve(config.databasePath) : null,
    geodataServeBin: config.geodataServeBin || null,
    runProcess: config.runProcess || runProcess,
  };
  return http.createServer((req, res) => dispatch(req, res, ctx));
}

function dispatch(req, res, ctx) {
  let url;
  try {
    url = new URL(req.url, 'http://localhost');
  } catch {
    return jsonError(res, 400, 'invalid_request', 'invalid request URL');
  }
  const pathname = url.pathname;

  if (pathname.startsWith('/api/')) {
    if (pathname === '/api/config' && req.method === 'GET') return handleConfig(res, ctx);
    if (pathname === '/api/backups' && req.method === 'GET') return handleBackups(res, ctx);
    if (pathname === '/api/restore' && req.method === 'POST') {
      return handleRestore(req, res, ctx).catch((err) => {
        if (!res.headersSent) jsonError(res, 500, 'internal_error', err.message);
        else res.end();
      });
    }
    return forward(req, res, ctx);
  }
  return serveStatic(req, res, ctx.staticDir);
}

// --- discovery ---------------------------------------------------------------

function readServerState(statePath) {
  let raw;
  try {
    raw = fs.readFileSync(statePath, 'utf8');
  } catch (err) {
    if (err.code === 'ENOENT') return null;
    return null;
  }
  try {
    const state = JSON.parse(raw);
    if (!state || typeof state.address !== 'string' || state.address === '') return null;
    return state;
  } catch {
    return null;
  }
}

// --- reverse proxy -----------------------------------------------------------

async function forward(req, res, ctx) {
  const url = new URL(req.url, 'http://localhost');
  const upstreamPath = url.pathname.replace(/^\/api/, '');
  const params = new URLSearchParams(url.searchParams);
  const skipAuth = params.get('skip_auth') === '1';
  params.delete('skip_auth');
  const query = params.toString();
  const targetPath = upstreamPath + (query ? `?${query}` : '');

  const state = readServerState(ctx.statePath);
  if (!state) {
    return jsonError(res, 502, 'no_server_state', `geodata-serve server.json not found at ${ctx.statePath}`);
  }

  let body = Buffer.alloc(0);
  try {
    body = await readBody(req);
  } catch (err) {
    return jsonError(res, 400, 'invalid_request', err.message);
  }

  const headers = {};
  if (req.headers['content-type']) headers['content-type'] = req.headers['content-type'];
  if (req.headers.accept) headers.accept = req.headers.accept;
  const isHealth = upstreamPath === '/health';
  if (!skipAuth && !isHealth && state.token) headers.authorization = `Bearer ${state.token}`;

  const baseAddress = state.address.replace(/\/+$/, '');
  const upstream = http.request(baseAddress + targetPath, { method: req.method, headers }, (upRes) => {
    const resHeaders = {};
    for (const name of ['content-type', 'x-request-id', 'cache-control', 'content-length']) {
      if (upRes.headers[name] != null) resHeaders[name] = upRes.headers[name];
    }
    res.writeHead(upRes.statusCode || 502, resHeaders);
    upRes.pipe(res);
  });

  upstream.on('error', (err) => {
    if (res.headersSent) {
      res.end();
      return;
    }
    jsonError(res, 502, 'upstream_unreachable', `could not reach geodata-serve: ${err.message}`);
  });

  // When the browser aborts the fetch, destroy the upstream connection so the
  // service observes the disconnect and cancels the running/queued request.
  res.on('close', () => {
    if (!res.writableEnded) upstream.destroy();
  });

  if (body.length) upstream.write(body);
  upstream.end();
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    req.on('data', (chunk) => {
      size += chunk.length;
      if (size > MAX_BODY_BYTES) {
        reject(new Error('request body too large'));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on('end', () => resolve(Buffer.concat(chunks)));
    req.on('error', reject);
  });
}

async function readJSONBody(req) {
  const buf = await readBody(req);
  if (buf.length === 0) return {};
  return JSON.parse(buf.toString('utf8'));
}

// --- introspection helpers ---------------------------------------------------

function handleConfig(res, ctx) {
  const state = readServerState(ctx.statePath);
  if (!state) {
    return jsonError(res, 404, 'no_server_state', `geodata-serve server.json not found at ${ctx.statePath}`);
  }
  res.writeHead(200, { 'content-type': 'application/json; charset=utf-8' });
  res.end(JSON.stringify({
    ok: true,
    interface_version: state.interface_version ?? null,
    pid: state.pid ?? null,
    address: state.address,
    database: state.database ?? null,
    started_at: state.started_at ?? null,
    token_set: typeof state.token === 'string' && state.token.length > 0,
    runtime_dir: ctx.runtimeDir,
    backup_dir: ctx.backupDir,
    restore_available: Boolean(ctx.geodataServeBin),
  }));
}

function handleBackups(res, ctx) {
  if (!ctx.backupDir) {
    return jsonError(res, 404, 'backup_dir_unset', '--backup-dir not configured');
  }
  let entries;
  try {
    entries = fs.readdirSync(ctx.backupDir, { withFileTypes: true });
  } catch (err) {
    return jsonError(res, 500, 'backup_dir_error', err.message);
  }
  const backups = [];
  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    const full = path.join(ctx.backupDir, entry.name);
    let verified = false;
    let modifiedAt = null;
    try {
      verified = fs.readFileSync(path.join(full, '.geodata-serve-verified'), 'utf8') === 'verified\n';
    } catch {
      /* unverified */
    }
    try {
      modifiedAt = fs.statSync(full).mtime.toISOString();
    } catch {
      /* ignore */
    }
    backups.push({ name: entry.name, modified_at: modifiedAt, verified });
  }
  backups.sort((a, b) => String(b.modified_at ?? '').localeCompare(String(a.modified_at ?? '')));
  res.writeHead(200, { 'content-type': 'application/json; charset=utf-8' });
  res.end(JSON.stringify({ ok: true, backup_dir: ctx.backupDir, count: backups.length, backups }));
}

async function handleRestore(req, res, ctx) {
  if (!ctx.backupDir) return jsonError(res, 400, 'backup_dir_unset', '--backup-dir not configured');
  if (!ctx.geodataServeBin) return jsonError(res, 400, 'restore_unavailable', '--geodata-serve-bin not configured');
  const state = readServerState(ctx.statePath);
  const databasePath = ctx.databasePath || state?.database;
  if (!databasePath) return jsonError(res, 400, 'database_unset', '--database is required after geodata-serve has stopped');

  let body;
  try {
    body = await readJSONBody(req);
  } catch {
    return jsonError(res, 400, 'invalid_request', 'invalid JSON request body');
  }
  const backup = body && typeof body.backup === 'string' ? body.backup : '';
  if (!backup || path.basename(backup) !== backup || backup === '.' || backup === '..') {
    return jsonError(res, 400, 'invalid_request', 'backup must be a directory name inside the backup dir');
  }
  const backupPath = path.join(ctx.backupDir, backup);
  let stat;
  try {
    stat = fs.statSync(backupPath);
  } catch {
    return jsonError(res, 404, 'backup_not_found', `backup not found: ${backup}`);
  }
  if (!stat.isDirectory()) return jsonError(res, 400, 'invalid_request', 'backup is not a directory');

  const args = ['restore', '--database', databasePath, '--runtime-dir', ctx.runtimeDir, '--backup', backupPath];
  let result;
  try {
    result = await ctx.runProcess(ctx.geodataServeBin, args);
  } catch (err) {
    return jsonError(res, 500, 'restore_failed', err.message);
  }
  res.writeHead(200, { 'content-type': 'application/json; charset=utf-8' });
  res.end(JSON.stringify({
    ok: result.exitCode === 0,
    exit_code: result.exitCode,
    stdout: result.stdout,
    stderr: result.stderr,
    command: [ctx.geodataServeBin, ...args].join(' '),
  }));
}

function runProcess(bin, args) {
  return new Promise((resolve, reject) => {
    const child = spawn(bin, args, { stdio: ['ignore', 'pipe', 'pipe'] });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk) => { stdout += chunk.toString('utf8'); });
    child.stderr.on('data', (chunk) => { stderr += chunk.toString('utf8'); });
    child.on('error', reject);
    child.on('close', (code) => resolve({ exitCode: code ?? -1, stdout, stderr }));
  });
}

// --- static files ------------------------------------------------------------

function serveStatic(req, res, staticDir) {
  if (req.method !== 'GET' && req.method !== 'HEAD') {
    res.writeHead(405, { 'content-type': 'application/json; charset=utf-8', allow: 'GET' });
    return res.end(JSON.stringify({ error: { code: 'method_not_allowed', message: 'method not allowed' } }));
  }
  let urlPath;
  try {
    urlPath = decodeURIComponent(new URL(req.url, 'http://localhost').pathname);
  } catch {
    res.writeHead(400, { 'content-type': 'text/plain; charset=utf-8' });
    return res.end('bad request');
  }
  if (urlPath === '/' || urlPath === '') urlPath = '/index.html';

  const root = path.normalize(staticDir);
  const filePath = path.normalize(path.join(root, urlPath));
  if (filePath !== root && !filePath.startsWith(root + path.sep)) {
    res.writeHead(403, { 'content-type': 'text/plain; charset=utf-8' });
    return res.end('forbidden');
  }
  fs.readFile(filePath, (err, data) => {
    if (err) {
      res.writeHead(404, { 'content-type': 'text/plain; charset=utf-8' });
      return res.end('not found');
    }
    const ext = path.extname(filePath).toLowerCase();
    res.writeHead(200, { 'content-type': MIME[ext] || 'application/octet-stream' });
    if (req.method === 'HEAD') res.end();
    else res.end(data);
  });
}

function jsonError(res, status, code, message) {
  if (res.headersSent) return;
  res.writeHead(status, { 'content-type': 'application/json; charset=utf-8' });
  res.end(JSON.stringify({ error: { code, message } }));
}

// --- CLI ---------------------------------------------------------------------

function parseArgs(argv) {
  const options = { port: 8787, runtimeDir: null, backupDir: null, databasePath: null, geodataServeBin: null, help: false };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    const next = () => argv[++i];
    switch (arg) {
      case '--runtime-dir': options.runtimeDir = next(); break;
      case '--backup-dir': options.backupDir = next(); break;
      case '--database': options.databasePath = next(); break;
      case '--geodata-serve-bin': options.geodataServeBin = next(); break;
      case '--port': options.port = Number(next()); break;
      case '--help': case '-h': options.help = true; break;
      default:
        throw new Error(`unknown option: ${arg}`);
    }
  }
  return options;
}

function usage() {
  const text = [
    'geodata-serve-web — test harness for the geodata-serve HTTP interface',
    '',
    'Usage:',
    '  node server.mjs --runtime-dir <path> [options]',
    '',
    'Options:',
    '  --runtime-dir <path>       geodata-serve runtime dir (contains server.json)',
    '  --backup-dir <path>        backup root (enables backup listing + restore)',
    '  --database <path>          database path required to restore after shutdown',
    '  --geodata-serve-bin <path> path to the geodata-serve binary (enables restore)',
    '  --port <n>                 port to serve the UI on (default 8787)',
  ];
  console.log(text.join('\n'));
}

function main() {
  let options;
  try {
    options = parseArgs(process.argv.slice(2));
  } catch (err) {
    console.error(err.message);
    usage();
    process.exit(2);
  }
  if (options.help || !options.runtimeDir) {
    usage();
    process.exit(options.help ? 0 : 2);
  }

  const app = createApp({
    runtimeDir: options.runtimeDir,
    staticDir: path.join(__dirname, 'static'),
    backupDir: options.backupDir,
    databasePath: options.databasePath,
    geodataServeBin: options.geodataServeBin,
  });
  app.listen(options.port, '127.0.0.1', () => {
    console.log(`geodata-serve-web listening on http://127.0.0.1:${app.address().port}`);
    console.log(`runtime-dir: ${path.resolve(options.runtimeDir)}`);
  });
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
