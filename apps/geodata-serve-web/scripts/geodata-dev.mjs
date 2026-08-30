import { spawn } from 'node:child_process';
import fs from 'node:fs';
import http from 'node:http';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const appDir = path.resolve(scriptDir, '..');
const repositoryDir = path.resolve(appDir, '..', '..');
const serviceDir = path.join(repositoryDir, 'services', 'geodata-serve');
const defaultConfigPath = path.join(repositoryDir, 'geodata-serve.local.json');
const binaryDir = path.join(repositoryDir, '.local', 'geodata-serve');
const binaryPath = path.join(binaryDir, process.platform === 'win32' ? 'geodata-serve.exe' : 'geodata-serve');
const readyTimeoutMs = 30_000;
const shutdownTimeoutMs = 15_000;

export function parseArguments(argv) {
  const [command = '', ...rest] = argv;
  let configPath = defaultConfigPath;
  for (let index = 0; index < rest.length; index += 1) {
    if (rest[index] !== '--config' || index + 1 >= rest.length) {
      throw new Error('usage: geodata-dev.mjs <build|init|dev|stop> [--config <path>]');
    }
    configPath = path.resolve(rest[index + 1]);
    index += 1;
  }
  if (!['build', 'init', 'dev', 'stop'].includes(command)) {
    throw new Error('usage: geodata-dev.mjs <build|init|dev|stop> [--config <path>]');
  }
  return { command, configPath };
}

export function readConfig(configPath) {
  let value;
  try {
    value = JSON.parse(fs.readFileSync(configPath, 'utf8'));
  } catch (error) {
    if (error.code === 'ENOENT') {
      throw new Error(`local config not found: ${configPath}\nCopy geodata-serve.local.example.json to geodata-serve.local.json, then set its paths.`);
    }
    throw new Error(`read local config: ${error.message}`);
  }
  return validateConfig(value, configPath);
}

export function validateConfig(value, configPath) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`local config must contain one JSON object: ${configPath}`);
  }
  const config = {};
  for (const name of ['database', 'runtimeDir', 'backupDir', 'workingDir']) {
    const configuredPath = value[name];
    if (typeof configuredPath !== 'string' || configuredPath.trim() === '') {
      throw new Error(`local config field ${name} must be a non-empty path`);
    }
    if (!path.isAbsolute(configuredPath)) {
      throw new Error(`local config field ${name} must be an absolute path`);
    }
    config[name] = path.normalize(configuredPath);
  }
  let workingDirectory;
  try {
    workingDirectory = fs.statSync(config.workingDir);
  } catch (error) {
    throw new Error(`workingDir is unavailable: ${error.message}`);
  }
  if (!workingDirectory.isDirectory()) throw new Error('workingDir must be a directory');

  const port = value.webPort ?? 8787;
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error('local config field webPort must be an integer between 1 and 65535');
  }
  return { ...config, webPort: port };
}

export function readServerState(statePath) {
  try {
    const value = JSON.parse(fs.readFileSync(statePath, 'utf8'));
    if (!value || typeof value.address !== 'string' || typeof value.token !== 'string' || !Number.isInteger(value.pid)) return null;
    return value;
  } catch {
    return null;
  }
}

export function requestServer(state, requestPath, method = 'GET') {
  return new Promise((resolve, reject) => {
    let endpoint;
    try {
      endpoint = new URL(requestPath, state.address);
    } catch {
      reject(new Error('server state has an invalid address'));
      return;
    }
    if (endpoint.protocol !== 'http:') {
      reject(new Error('server state must use http'));
      return;
    }
    const request = http.request(endpoint, {
      method,
      headers: state.token ? { authorization: `Bearer ${state.token}` } : {},
      timeout: 2_000,
    }, (response) => {
      const chunks = [];
      response.on('data', (chunk) => chunks.push(chunk));
      response.on('end', () => {
        const body = Buffer.concat(chunks).toString('utf8');
        let json = null;
        try { json = JSON.parse(body); } catch { /* server may be stopping */ }
        resolve({ statusCode: response.statusCode ?? 0, json });
      });
    });
    request.on('timeout', () => request.destroy(new Error('request timed out')));
    request.on('error', reject);
    request.end();
  });
}

export async function isHealthy(state) {
  try {
    const response = await requestServer({ ...state, token: '' }, '/health');
    return response.statusCode === 200 && response.json?.status === 'ok';
  } catch {
    return false;
  }
}

export async function waitForReady({ statePath, expectedPID, timeoutMs = readyTimeoutMs, probe = isHealthy, cancelled = () => false }) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (cancelled()) throw new Error('startup interrupted');
    const state = readServerState(statePath);
    if (state?.pid === expectedPID && await probe(state)) return state;
    await delay(200);
  }
  throw new Error(`geodata-serve did not become ready within ${Math.round(timeoutMs / 1000)} seconds`);
}

export async function shutdownServer(state) {
  const response = await requestServer(state, '/shutdown', 'POST');
  if (response.statusCode !== 202 && response.statusCode !== 503) {
    throw new Error(`shutdown request failed with HTTP ${response.statusCode}`);
  }
}

async function build() {
  fs.mkdirSync(binaryDir, { recursive: true });
  console.log('Building geodata-serve...');
  await run('go', ['build', '-o', binaryPath, './cmd/geodata-serve'], serviceDir);
  return binaryPath;
}

async function initialize(config) {
  const executable = await build();
  console.log('Preparing DuckDB extensions...');
  await run(executable, ['init', '--runtime-dir', config.runtimeDir], serviceDir);
  return executable;
}

async function start(config) {
  const statePath = path.join(config.runtimeDir, 'server.json');
  const existingState = readServerState(statePath);
  if (existingState && await isHealthy(existingState)) {
    throw new Error('geodata-serve is already running for this runtimeDir; run npm run stop first');
  }

  const executable = await initialize(config);
  const service = spawn(executable, [
    'serve',
    '--database', config.database,
    '--runtime-dir', config.runtimeDir,
    '--backup-dir', config.backupDir,
    '--working-dir', config.workingDir,
  ], { cwd: serviceDir, stdio: 'inherit', windowsHide: true });
  let interrupted = false;
  let web = null;
  const onSignal = () => {
    interrupted = true;
    if (web?.exitCode === null) web.kill('SIGTERM');
  };
  process.once('SIGINT', onSignal);
  process.once('SIGTERM', onSignal);

  try {
    await waitForReady({
      statePath,
      expectedPID: service.pid,
      cancelled: () => interrupted || service.exitCode !== null || service.signalCode !== null,
    });
    web = spawn(process.execPath, [
      path.join(appDir, 'server.mjs'),
      '--runtime-dir', config.runtimeDir,
      '--backup-dir', config.backupDir,
      '--database', config.database,
      '--geodata-serve-bin', executable,
      '--port', String(config.webPort),
    ], { cwd: appDir, stdio: 'inherit', windowsHide: true });
    console.log(`Open http://127.0.0.1:${config.webPort}`);
    const result = await waitForExit(web);
    if (!interrupted && result.code !== 0) throw new Error(`geodata-serve-web exited with code ${result.code ?? 'unknown'}`);
  } finally {
    process.off('SIGINT', onSignal);
    process.off('SIGTERM', onSignal);
    await stopChildren({ web, service, statePath, expectedPID: service.pid });
  }
}

async function stop(config) {
  const statePath = path.join(config.runtimeDir, 'server.json');
  const state = readServerState(statePath);
  if (!state) {
    console.log('geodata-serve is not running.');
    return;
  }
  await shutdownServer(state);
  await waitForStateRemoval(statePath);
  console.log('geodata-serve stopped.');
}

async function stopChildren({ web, service, statePath, expectedPID }) {
  if (web?.exitCode === null) web.kill('SIGTERM');
  const state = readServerState(statePath);
  if (state?.pid === expectedPID) {
    try {
      await shutdownServer(state);
    } catch (error) {
      console.error(`Could not request geodata-serve shutdown: ${error.message}`);
    }
  }
  if (service?.exitCode === null && !await waitForExit(service, shutdownTimeoutMs)) {
    console.error('geodata-serve did not stop cleanly; terminating it.');
    service.kill('SIGTERM');
  }
}

function run(command, args, cwd) {
  const child = spawn(command, args, { cwd, stdio: 'inherit', windowsHide: true });
  return waitForExit(child).then((result) => {
    if (result.code !== 0) throw new Error(`${command} exited with code ${result.code ?? 'unknown'}`);
  });
}

function waitForExit(child, timeoutMs) {
  if (child.exitCode !== null) return Promise.resolve({ code: child.exitCode, signal: child.signalCode });
  return new Promise((resolve, reject) => {
    const done = (result) => {
      if (timer) clearTimeout(timer);
      child.off('error', onError);
      child.off('exit', onExit);
      resolve(result);
    };
    const onError = (error) => {
      if (timer) clearTimeout(timer);
      child.off('exit', onExit);
      reject(error);
    };
    const onExit = (code, signal) => done({ code, signal });
    const timer = timeoutMs ? setTimeout(() => done(null), timeoutMs) : null;
    child.once('error', onError);
    child.once('exit', onExit);
  });
}

async function waitForStateRemoval(statePath) {
  const deadline = Date.now() + shutdownTimeoutMs;
  while (Date.now() < deadline) {
    if (!readServerState(statePath)) return;
    await delay(200);
  }
  throw new Error('geodata-serve did not remove server.json after shutdown');
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

async function main() {
  const { command, configPath } = parseArguments(process.argv.slice(2));
  if (command === 'build') return build();
  const config = readConfig(configPath);
  if (command === 'init') return initialize(config);
  if (command === 'stop') return stop(config);
  return start(config);
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
