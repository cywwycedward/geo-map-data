// geodata-serve-web frontend. Talks to the same-origin proxy in server.mjs,
// which forwards to geodata-serve and streams NDJSON back. Thin presentation:
// the NDJSON parsing/accumulation logic lives in ndjson.mjs (unit-tested).

import { NDJSONStream, parseEventLine } from './ndjson.mjs';

const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => Array.from(document.querySelectorAll(sel));

function esc(value) {
  return String(value ?? '').replace(/[&<>"']/g, (c) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
  ));
}

function fmtJSON(value) {
  return JSON.stringify(value, null, 2);
}

// ---------------------------------------------------------------------------
// Presets
// ---------------------------------------------------------------------------

const PRESETS = [
  {
    group: '值编码 · 整数与 DECIMAL',
    mode: 'read',
    label: 'BIGINT / UBIGINT / HUGEINT / DECIMAL（按十进制字符串返回）',
    sql: `SELECT
  9223372036854775807::BIGINT AS bigint_max,
  18446744073709551615::UBIGINT AS ubigint_max,
  123456789012345678901234567890::HUGEINT AS hugeint,
  1234567890.1234567890::DECIMAL(20,10) AS dec`,
  },
  {
    group: '值编码 · 浮点数',
    mode: 'read',
    label: '非有限浮点数 NaN / Infinity / -Infinity',
    sql: `SELECT
  'NaN'::DOUBLE AS nan_val,
  'Infinity'::DOUBLE AS inf_val,
  '-Infinity'::DOUBLE AS neg_inf_val,
  0.1::FLOAT AS float_val`,
  },
  {
    group: '值编码 · 二进制与 UUID',
    mode: 'read',
    label: 'BLOB（base64）与 UUID',
    sql: `SELECT
  from_hex('DEADBEEF')::BLOB AS blob_val,
  '123e4567-e89b-12d3-a456-426614174000'::UUID AS uuid_val`,
  },
  {
    group: '值编码 · 时间',
    mode: 'read',
    label: 'DATE / TIME / TIMESTAMP / INTERVAL',
    sql: `SELECT
  DATE '2024-01-15' AS d,
  TIME '13:45:30' AS t,
  TIMESTAMP '2024-01-15 13:45:30.123' AS ts,
  INTERVAL '1 year 2 months 3 days 4 hours 5 minutes' AS iv`,
  },
  {
    group: '值编码 · 嵌套类型',
    mode: 'read',
    label: 'LIST / STRUCT / MAP / UNION',
    sql: `SELECT
  [1, 2, 3] AS list_val,
  {'a': 1, 'b': 'x'} AS struct_val,
  MAP(['x', 'y'], [10, 20]) AS map_val,
  UNION_VALUE(x := 42) AS union_val`,
  },
  {
    group: '值编码 · JSON 与 NULL',
    mode: 'read',
    label: 'JSON / NULL / BOOLEAN / ENUM',
    sql: `SELECT
  '{"a":[1,2,3],"b":null}'::JSON AS json_val,
  NULL AS null_val,
  TRUE AS bool_val,
  'b'::ENUM('a','b','c') AS enum_val`,
  },
  {
    group: 'Spatial / httpfs',
    mode: 'read',
    label: '已加载扩展（spatial / httpfs）',
    sql: `SELECT extension_name, extension_version, loaded, installed
FROM duckdb_extensions()
WHERE extension_name IN ('spatial','httpfs')`,
  },
  {
    group: 'Spatial / httpfs',
    mode: 'read',
    label: 'ST_Drivers() 支持的格式',
    sql: `SELECT short_name, long_name FROM ST_Drivers() ORDER BY short_name`,
  },
  {
    group: 'Spatial / httpfs',
    mode: 'read',
    label: '读取 GeoJSON（ST_Read）',
    sql: `SELECT name, ST_AsGeoJSON(geom) AS geometry
FROM ST_Read('testdata/points.geojson')`,
  },
  {
    group: 'Spatial / httpfs',
    mode: 'read',
    label: '读取 Shapefile（需本地文件）',
    sql: `SELECT * FROM ST_Read('data/raw/roads.shp') LIMIT 10`,
  },
  {
    group: 'Spatial / httpfs',
    mode: 'read',
    label: '读取 GeoParquet（read_parquet，需本地文件）',
    sql: `SELECT * FROM read_parquet('data/points.parquet') LIMIT 10`,
  },
  {
    group: '并发 / 超时',
    mode: 'read',
    label: '慢查询（观察排队 / 取消 / 超时，可自行调大范围）',
    sql: `SELECT count(*) FROM range(0, 300000000) t(i)`,
  },
  {
    group: '写入',
    mode: 'write',
    label: '建表（触发写前备份）',
    sql: `BEGIN;
CREATE OR REPLACE TABLE t_test AS SELECT i, i * 2 AS j FROM range(0, 1000) t(i);
COMMIT;`,
  },
  {
    group: '写入',
    mode: 'write',
    label: '从 GeoJSON 建表',
    sql: `BEGIN;
CREATE OR REPLACE TABLE points AS SELECT * FROM ST_Read('testdata/points.geojson');
COMMIT;`,
  },
  {
    group: '写入',
    mode: 'write',
    label: '故意失败（观察 error 事件）',
    sql: `INSERT INTO table_that_does_not_exist VALUES (1)`,
  },
];

const SCENARIOS = {
  'basic-read': {
    label: '基础读取',
    mode: 'read',
    sql: 'SELECT 1 AS one',
    expectation: '期望收到 status → schema → row → summary，终态为 finished。',
    expected: { terminalType: 'summary', state: 'finished' },
  },
  'stream-read': {
    label: '流式结果',
    mode: 'read',
    sql: 'SELECT * FROM range(0, 5) AS t(i)',
    expectation: '期望逐行看到 schema 和 row 事件，终态为 finished。',
    expected: { terminalType: 'summary', state: 'finished' },
  },
  'write-backup': {
    label: '写前备份',
    mode: 'write',
    sql: `BEGIN;
CREATE OR REPLACE TABLE t_test AS SELECT i, i * 2 AS j FROM range(0, 1000) t(i);
COMMIT;`,
    expectation: '期望经过 backing_up → running → finished；完成后可在“备份与恢复”刷新列表。',
    expected: { terminalType: 'summary', state: 'finished', requiredStates: ['backing_up'] },
  },
  'sql-error': {
    label: 'SQL 失败',
    mode: 'write',
    sql: 'INSERT INTO table_that_does_not_exist VALUES (1)',
    expectation: '期望收到 error 事件，state=failed 且 code=sql_failed。',
    expected: { terminalType: 'error', state: 'failed', errorCode: 'sql_failed' },
  },
  timeout: {
    label: '请求超时',
    mode: 'read',
    sql: 'SELECT SUM(sin(i::DOUBLE)) FROM range(0, 10000000000) AS t(i)',
    timeoutSeconds: 1,
    expectation: '期望请求在约 1 秒后取消，收到 code=deadline_exceeded。',
    expected: { terminalType: 'error', state: 'cancelled', errorCode: 'deadline_exceeded' },
  },
  'auth-401': {
    label: '401 鉴权',
    mode: 'read',
    sql: 'SELECT 1 AS one',
    skipAuth: true,
    expectation: '期望代理省略 token，服务直接返回 HTTP 401 unauthorized。',
    expected: { httpStatus: 401 },
  },
};

// ---------------------------------------------------------------------------
// API helpers
// ---------------------------------------------------------------------------

async function apiJSON(path, options = {}) {
  const resp = await fetch(path, {
    method: options.method ?? 'GET',
    headers: options.body !== undefined ? { 'content-type': 'application/json' } : undefined,
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
  });
  const text = await resp.text();
  let data = null;
  try {
    data = JSON.parse(text);
  } catch {
    /* non-JSON */
  }
  return { status: resp.status, data, text };
}

// Stream an /execute call, feeding each NDJSON line into an NDJSONStream and
// invoking callbacks as events arrive. Returns { model, requestId }.
async function streamExecute(options, callbacks = {}) {
  const controller = new AbortController();
  callbacks.onController?.(controller);

  const query = new URLSearchParams();
  if (options.skipAuth) query.set('skip_auth', '1');
  const url = '/api/execute' + (query.toString() ? `?${query}` : '');

  let resp;
  try {
    resp = await fetch(url, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({
        mode: options.mode,
        sql: options.sql,
        ...(options.timeoutSeconds ? { timeout_seconds: options.timeoutSeconds } : {}),
      }),
      signal: controller.signal,
    });
  } catch (err) {
    if (err.name === 'AbortError') {
      callbacks.onAbort?.();
      return { model: null, requestId: null, aborted: true };
    }
    callbacks.onNetworkError?.(err);
    return { model: null, requestId: null };
  }

  const status = resp.status;
  const requestId = resp.headers.get('x-request-id');
  const contentType = resp.headers.get('content-type') || '';
  callbacks.onMeta?.({ status, requestId, contentType });

  if (!resp.ok || !resp.body || !contentType.includes('ndjson')) {
    const text = await resp.text();
    callbacks.onHTTPError?.({ status, text });
    return { model: null, requestId, status, error: text };
  }

  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  const stream = new NDJSONStream();
  let buffer = '';

  const handleLine = (line) => {
    if (line.trim() === '') return;
    let event;
    try {
      event = parseEventLine(line);
    } catch (err) {
      callbacks.onParseError?.(err, line);
      return;
    }
    try {
      stream.push(event);
    } catch (err) {
      callbacks.onParseError?.(err, line);
      return;
    }
    callbacks.onEvent?.(event, stream.model());
  };

  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    let index;
    while ((index = buffer.indexOf('\n')) >= 0) {
      const line = buffer.slice(0, index);
      buffer = buffer.slice(index + 1);
      handleLine(line);
    }
  }
  if (buffer.length > 0) handleLine(buffer);
  callbacks.onDone?.(stream.model());
  return { model: stream.model(), requestId };
}

// ---------------------------------------------------------------------------
// Value rendering
// ---------------------------------------------------------------------------

function formatValue(value) {
  if (value === null || value === undefined) return { text: 'NULL', cls: 'null' };
  if (typeof value === 'object') {
    if (value.encoding === 'base64' && typeof value.data === 'string') {
      const preview = value.data.length > 24 ? `${value.data.slice(0, 24)}…` : value.data;
      return { text: `base64:${preview}`, cls: 'blob', title: value.data };
    }
    return { text: JSON.stringify(value), cls: 'nested' };
  }
  if (typeof value === 'number') return { text: String(value), cls: 'num' };
  if (typeof value === 'boolean') return { text: String(value), cls: 'bool' };
  if (typeof value === 'string' && /^-?\d+(\.\d+)?$/.test(value) && value.length > 8) {
    return { text: value, cls: 'numstr', title: '十进制字符串（保精度）' };
  }
  return { text: String(value), cls: 'str' };
}

// ---------------------------------------------------------------------------
// Overview
// ---------------------------------------------------------------------------

function setConnectionState(kind, title, detail) {
  const state = $('#connection-state');
  state.className = `connection-state ${kind}`;
  $('#connection-title').textContent = title;
  $('#connection-detail').textContent = detail;
  $('#conn-dot').className = kind === 'ready' ? 'dot ok' : 'dot';
  $('#conn-label').textContent = kind === 'ready' ? '服务已发现' : kind === 'pending' ? '正在连接…' : '服务未连接';
}

async function loadConfig() {
  const res = await apiJSON('/api/config');
  const pre = $('#cfg-json');
  if (res.status === 200) {
    pre.textContent = fmtJSON(res.data);
    const parts = [
      res.data.address,
      `接口 v${res.data.interface_version ?? '—'}`,
      res.data.token_set ? '鉴权已配置' : '未发现 token',
      res.data.backup_dir ? '备份可用' : '未配置备份目录',
    ];
    setConnectionState('ready', '服务已就绪，可以开始测试。', parts.join(' · '));
  } else {
    pre.textContent = `无法读取 server.json（HTTP ${res.status}）：${res.data?.error?.message ?? res.text}`;
    setConnectionState('error', '未发现运行中的 geodata-serve。', '确认服务已启动，并检查测试台的 --runtime-dir 是否正确。');
  }
}

async function checkHealth() {
  const res = await apiJSON('/api/health');
  const box = $('#health-result');
  box.textContent = `GET /health → HTTP ${res.status}\n${fmtJSON(res.data)}`;
  box.className = res.status === 200 ? 'mono ok-box' : 'mono err-box';
  return res;
}

async function authHealthTest() {
  const res = await apiJSON('/api/health');
  $('#auth-result').textContent = `无 token GET /health → HTTP ${res.status}\n${fmtJSON(res.data)}`;
}

async function auth401Test() {
  const res = await apiJSON('/api/execute?skip_auth=1', {
    method: 'POST',
    body: { mode: 'read', sql: 'SELECT 1' },
  });
  $('#auth-result').textContent = `无 token POST /execute → HTTP ${res.status}\n${fmtJSON(res.data)}`;
}

async function shutdownService() {
  if (!window.confirm('确认关闭 geodata-serve？关闭后需重新启动服务才能继续测试。')) return;
  const res = await apiJSON('/api/shutdown', { method: 'POST' });
  $('#shutdown-result').textContent = `POST /shutdown → HTTP ${res.status}\n${fmtJSON(res.data)}`;
  setTimeout(loadConfig, 500);
}

// ---------------------------------------------------------------------------
// Execute
// ---------------------------------------------------------------------------

let currentController = null;
let activeScenarioId = 'basic-read';

function activeScenario() {
  return activeScenarioId ? SCENARIOS[activeScenarioId] : null;
}

function renderScenarioVerdict(kind, message) {
  const box = $('#exec-verdict');
  box.className = `verdict ${kind}`;
  box.textContent = message;
}

function selectScenario(id, { navigate = true } = {}) {
  const scenario = SCENARIOS[id];
  if (!scenario) return;
  activeScenarioId = id;
  $('#mode').value = scenario.mode;
  $('#sql').value = scenario.sql;
  $('#timeout').value = scenario.timeoutSeconds ?? '';
  $('#skip-auth').checked = Boolean(scenario.skipAuth);
  $('#preset').value = '';
  $$('.scenario-button').forEach((button) => button.classList.toggle('active', button.dataset.scenario === id));
  $('#scenario-hint').textContent = `${scenario.label}：${scenario.expectation}`;
  renderScenarioVerdict('pending', `已准备“${scenario.label}”。${scenario.expectation}`);
  if (navigate) activateTab('execute');
}

function clearScenario() {
  if (!activeScenarioId) return;
  activeScenarioId = null;
  $$('.scenario-button').forEach((button) => button.classList.remove('active'));
  $('#scenario-hint').textContent = '已切换为自定义 SQL；结果将保留原始事件，自动判定已关闭。';
  renderScenarioVerdict('muted', '自定义 SQL 模式：请根据协议事件和预期行为人工判断。');
}

function assessScenario(scenario, result) {
  const expected = scenario.expected;
  if (expected.httpStatus) {
    const passed = result?.status === expected.httpStatus;
    return {
      passed,
      message: passed
        ? `通过：${scenario.label} 返回 HTTP ${expected.httpStatus}。`
        : `未通过：期望 HTTP ${expected.httpStatus}，实际为 HTTP ${result?.status ?? '无响应'}。`,
    };
  }

  const model = result?.model;
  if (!model) {
    return { passed: false, message: `未通过：${scenario.label}未收到可判定的 NDJSON 终态事件。` };
  }
  const checks = [
    model.terminalType === expected.terminalType,
    model.state === expected.state,
    !expected.errorCode || model.errorCode === expected.errorCode,
    !(expected.requiredStates ?? []).some((state) => !model.states.includes(state)),
  ];
  const passed = checks.every(Boolean);
  return {
    passed,
    message: passed
      ? `通过：${scenario.label}符合预期（${scenario.expectation}）。`
      : `未通过：期望 ${scenario.expectation} 实际 terminal=${model.terminalType ?? '—'} state=${model.state ?? '—'} code=${model.errorCode ?? '—'}。`,
  };
}

function resetExecuteView() {
  $('#exec-status').innerHTML = '';
  $('#exec-schema').textContent = '';
  $('#exec-table').innerHTML = '<thead></thead><tbody></tbody>';
  $('#exec-summary').textContent = '';
  $('#exec-raw').textContent = '';
  $('#exec-meta').textContent = '';
}

function renderStatusStrip(states, terminal) {
  const labels = { queued: 'queued', backing_up: 'backing_up', running: 'running', finished: 'finished', failed: 'failed', cancelled: 'cancelled' };
  const box = $('#exec-status');
  box.innerHTML = '';
  for (const s of states) {
    const chip = document.createElement('span');
    chip.className = `chip ${s}`;
    chip.textContent = labels[s] ?? s;
    box.appendChild(chip);
  }
  if (terminal && !states.includes(terminal)) {
    const chip = document.createElement('span');
    chip.className = `chip ${terminal}`;
    chip.textContent = labels[terminal] ?? terminal;
    box.appendChild(chip);
  }
}

function renderSchema(columns) {
  const box = $('#exec-schema');
  if (!columns.length) {
    box.textContent = '（无结果列）';
    return;
  }
  box.textContent = 'schema: ' + columns.map((c) => `${c.name} (${c.duckdb_type})`).join(', ');
}

function renderTable(model) {
  const table = $('#exec-table');
  if (!model.hasSchema) return;
  const thead = table.querySelector('thead');
  const tbody = table.querySelector('tbody');
  if (thead.children.length === 0) {
    const tr = document.createElement('tr');
    tr.innerHTML = '<th class="idx">#</th>' + model.columns.map((c) => `<th>${esc(c.name)}<div class="coltype">${esc(c.duckdb_type)}</div></th>`).join('');
    thead.appendChild(tr);
  }
  const start = tbody.children.length;
  for (let i = start; i < model.rows.length; i += 1) {
    const tr = document.createElement('tr');
    tr.innerHTML = `<td class="idx">${i + 1}</td>` + model.rows[i].map((v) => {
      const f = formatValue(v);
      return `<td class="${f.cls}"${f.title ? ` title="${esc(f.title)}"` : ''}>${esc(f.text)}</td>`;
    }).join('');
    tbody.appendChild(tr);
  }
}

function renderSummary(model) {
  const box = $('#exec-summary');
  if (!model.terminalType) {
    box.textContent = '（尚未收到终态事件）';
    box.className = 'mono';
    return;
  }
  const timing = `queued_ms=${model.queuedMS} execution_ms=${model.executionMS}`;
  if (model.terminalType === 'summary') {
    box.textContent = `summary: state=${model.state} row_count=${model.rowCount} ${timing}`;
    box.className = 'mono ok-box';
  } else {
    box.textContent = `error: state=${model.state} code=${model.errorCode} message="${model.errorMessage}" ${timing}`;
    box.className = 'mono err-box';
  }
}

async function runExecute() {
  const mode = $('#mode').value;
  const sql = $('#sql').value;
  const timeoutRaw = $('#timeout').value;
  const timeoutSeconds = timeoutRaw ? Number(timeoutRaw) : null;
  const skipAuth = $('#skip-auth').checked;
  const scenario = activeScenario();

  if (!sql.trim()) {
    window.alert('SQL 不能为空');
    return;
  }

  resetExecuteView();
  if (scenario) renderScenarioVerdict('pending', `正在验证“${scenario.label}”…`);
  $('#btn-run').disabled = true;
  $('#btn-cancel').disabled = false;
  const startedAt = performance.now();
  const rawLines = [];

  const result = await streamExecute(
    { mode, sql, timeoutSeconds, skipAuth },
    {
      onController: (c) => { currentController = c; },
      onMeta: ({ status, requestId, contentType }) => {
        $('#exec-meta').textContent = `HTTP ${status} · X-Request-ID: ${requestId ?? '—'} · ${contentType}`;
      },
      onEvent: (event, model) => {
        rawLines.push(JSON.stringify(event));
        $('#exec-raw').textContent = rawLines.join('\n');
        renderStatusStrip(model.states, model.terminalType ? model.state : null);
        renderSchema(model.columns);
        renderTable(model);
        renderSummary(model);
      },
      onParseError: (err, line) => {
        rawLines.push(`# parse error: ${err.message} | ${line}`);
        $('#exec-raw').textContent = rawLines.join('\n');
      },
      onHTTPError: ({ status, text }) => {
        $('#exec-summary').textContent = `HTTP ${status}：${text}`;
        $('#exec-summary').className = 'mono err-box';
      },
      onAbort: () => {
        $('#exec-meta').textContent += ' · 已取消（客户端断开）';
      },
      onNetworkError: (err) => {
        $('#exec-summary').textContent = `网络错误：${err.message}`;
        $('#exec-summary').className = 'mono err-box';
      },
      onDone: (model) => {
        const elapsed = Math.round(performance.now() - startedAt);
        $('#exec-meta').textContent += ` · 总耗时 ${elapsed} ms`;
        if (model) {
          renderStatusStrip(model.states, model.state);
          renderSummary(model);
        }
      },
    },
  );

  $('#btn-run').disabled = false;
  $('#btn-cancel').disabled = true;
  currentController = null;
  if (scenario) {
    const verdict = assessScenario(scenario, result);
    renderScenarioVerdict(verdict.passed ? 'pass' : 'fail', verdict.message);
  }
  return result;
}

function cancelExecute() {
  if (currentController) currentController.abort();
}

// ---------------------------------------------------------------------------
// Concurrency
// ---------------------------------------------------------------------------

function statusColor(state) {
  return {
    queued: '#55606e', backing_up: '#b8860b', running: '#3b82f6',
    finished: '#22c55e', failed: '#ef4444', cancelled: '#a855f7',
  }[state] ?? '#55606e';
}

function computeMaxOverlap(intervals) {
  // intervals: [{start, end}] with numeric ms
  const points = [];
  for (const it of intervals) {
    points.push([it.start, 1], [it.end, -1]);
  }
  points.sort((a, b) => a[0] - b[0] || a[1] - b[1]);
  let current = 0;
  let max = 0;
  for (const [, delta] of points) {
    current += delta;
    max = Math.max(max, current);
  }
  return max;
}

async function runConcurrency() {
  const reads = Math.max(0, Number($('#conc-reads').value) || 0);
  const writes = Math.max(0, Number($('#conc-writes').value) || 0);
  const sql = $('#conc-sql').value.trim() || 'SELECT count(*) FROM range(0, 300000000) t(i)';
  const writeSQL = 'BEGIN; CREATE OR REPLACE TABLE t_conc AS SELECT i FROM range(0, 1000) t(i); COMMIT;';

  $('#conc-timeline').innerHTML = '';
  $('#conc-summary').textContent = '';
  $('#btn-conc').disabled = true;

  const jobs = [];
  for (let i = 0; i < reads; i += 1) jobs.push({ mode: 'read', sql });
  for (let i = 0; i < writes; i += 1) jobs.push({ mode: 'write', sql: writeSQL });

  const startedAt = performance.now();
  const records = jobs.map((job, i) => ({
    index: i,
    mode: job.mode,
    requestId: null,
    states: [], // [{state, t}]
    terminal: null,
    interval: null, // {start, end} for the slot it occupies
  }));

  await Promise.all(jobs.map((job, i) => streamExecute(
    { mode: job.mode, sql: job.sql },
    {
      onMeta: ({ requestId }) => { records[i].requestId = requestId; },
      onEvent: (event) => {
        if (event.type === 'status') {
          records[i].states.push({ state: event.state, t: performance.now() });
        } else if (event.type === 'summary' || event.type === 'error') {
          records[i].terminal = event.state;
          records[i].states.push({ state: event.state, t: performance.now() });
        }
        renderConcurrency(records, startedAt);
      },
    },
  )));

  renderConcurrency(records, startedAt);

  // Compute observed slot usage: read slot from running→terminal, write slot
  // from backing_up→terminal.
  const readIntervals = [];
  const writeIntervals = [];
  for (const r of records) {
    const startState = r.mode === 'read' ? 'running' : 'backing_up';
    const start = r.states.find((s) => s.state === startState);
    const end = r.states.find((s) => ['finished', 'failed', 'cancelled'].includes(s.state));
    if (start && end) {
      const interval = { start: start.t, end: end.t };
      r.interval = interval;
      (r.mode === 'read' ? readIntervals : writeIntervals).push(interval);
    }
  }
  const maxReads = computeMaxOverlap(readIntervals);
  const maxWrites = computeMaxOverlap(writeIntervals);
  const finished = records.filter((r) => r.terminal === 'finished').length;

  $('#conc-summary').textContent =
    `完成 ${finished}/${records.length} 个请求 · ` +
    `观察到的最大并发读 = ${maxReads}（期望 ≤ 2）· ` +
    `最大并发写 = ${maxWrites}（期望 ≤ 1）`;

  $('#btn-conc').disabled = false;
}

function renderConcurrency(records, startedAt) {
  const box = $('#conc-timeline');
  const end = performance.now();
  const total = Math.max(1, end - startedAt);
  box.innerHTML = '';

  for (const r of records) {
    const row = document.createElement('div');
    row.className = 'timeline-row';

    const label = document.createElement('div');
    label.className = 'timeline-label';
    label.textContent = `${r.mode} #${r.index} ${r.requestId ? r.requestId : ''}`;
    row.appendChild(label);

    const track = document.createElement('div');
    track.className = 'timeline-track';

    // Render each state as a segment spanning to the next state (or now).
    for (let i = 0; i < r.states.length; i += 1) {
      const start = r.states[i].t;
      const next = i + 1 < r.states.length ? r.states[i + 1].t : end;
      const left = ((start - startedAt) / total) * 100;
      const width = Math.max(0, ((next - start) / total) * 100);
      const seg = document.createElement('div');
      seg.className = 'seg';
      seg.style.left = `${Math.max(0, left)}%`;
      seg.style.width = `${width}%`;
      seg.style.backgroundColor = statusColor(r.states[i].state);
      seg.title = `${r.states[i].state}`;
      track.appendChild(seg);
    }
    if (r.interval) {
      const span = document.createElement('div');
      const left = ((r.interval.start - startedAt) / total) * 100;
      const width = ((r.interval.end - r.interval.start) / total) * 100;
      span.className = 'seg-slot';
      span.style.left = `${Math.max(0, left)}%`;
      span.style.width = `${Math.min(100, width)}%`;
      track.appendChild(span);
    }
    row.appendChild(track);
    box.appendChild(row);
  }
}

// ---------------------------------------------------------------------------
// Backups
// ---------------------------------------------------------------------------

async function loadBackups() {
  const res = await apiJSON('/api/backups');
  const box = $('#backups-list');
  if (res.status !== 200) {
    box.textContent = `无法读取备份目录（HTTP ${res.status}）：${res.data?.error?.message ?? res.text}`;
    box.className = 'mono err-box';
    return;
  }
  if (!res.data.backups.length) {
    box.textContent = '备份目录为空。执行一次 write 请求后刷新。';
    box.className = 'mono muted';
    return;
  }
  box.innerHTML = '';
  box.className = 'mono';
  for (const b of res.data.backups) {
    const row = document.createElement('div');
    row.className = 'backup-row';
    const badge = document.createElement('span');
    badge.className = `badge ${b.verified ? 'ok' : 'warn'}`;
    badge.textContent = b.verified ? 'verified' : '未验证';
    row.appendChild(badge);
    const name = document.createElement('span');
    name.className = 'backup-name';
    name.textContent = b.name;
    row.appendChild(name);
    if (b.modified_at) {
      const mtime = document.createElement('span');
      mtime.className = 'muted';
      mtime.textContent = new Date(b.modified_at).toLocaleString();
      row.appendChild(mtime);
    }
    if (b.verified) {
      const btn = document.createElement('button');
      btn.textContent = '恢复到此备份';
      btn.className = 'restore-btn';
      btn.addEventListener('click', () => restoreBackup(b.name));
      row.appendChild(btn);
    }
    box.appendChild(row);
  }
}

async function restoreBackup(name) {
  if (!window.confirm(`恢复备份“${name}”？恢复前请先关闭 geodata-serve（离线恢复）。`)) return;
  const res = await apiJSON('/api/restore', { method: 'POST', body: { backup: name } });
  const box = $('#backups-list');
  const detail = document.createElement('pre');
  detail.className = 'mono';
  detail.textContent = `restore 结果（exit ${res.data?.exit_code ?? '—'}）：\n${res.data?.stdout ?? ''}${res.data?.stderr ?? ''}`;
  box.appendChild(detail);
  if (res.status !== 200 || res.data?.ok === false) {
    detail.className = 'mono err-box';
  }
}

// ---------------------------------------------------------------------------
// Tabs
// ---------------------------------------------------------------------------

function activateTab(name) {
  $$('.tab').forEach((tab) => tab.classList.toggle('active', tab.dataset.tab === name));
  $$('.panel').forEach((panel) => panel.classList.toggle('active', panel.id === `tab-${name}`));
}

function setupTabs() {
  $$('.tab').forEach((tab) => {
    tab.addEventListener('click', () => activateTab(tab.dataset.tab));
  });
}

function setupPresets() {
  const select = $('#preset');
  let currentGroup = null;
  for (const p of PRESETS) {
    if (p.group !== currentGroup) {
      const opt = document.createElement('optgroup');
      opt.label = p.group;
      select.appendChild(opt);
      currentGroup = p.group;
    }
    const opt = document.createElement('option');
    opt.value = PRESETS.indexOf(p);
    opt.textContent = p.label;
    select.appendChild(opt);
  }
  select.addEventListener('change', () => {
    const index = Number.parseInt(select.value, 10);
    if (!Number.isInteger(index)) return;
    const p = PRESETS[index];
    if (!p) return;
    clearScenario();
    $('#mode').value = p.mode;
    $('#sql').value = p.sql;
  });
}

function setupScenarioControls() {
  $$('[data-scenario]').forEach((button) => {
    button.addEventListener('click', () => selectScenario(button.dataset.scenario));
  });
  $$('[data-tab-target]').forEach((button) => {
    button.addEventListener('click', () => activateTab(button.dataset.tabTarget));
  });
  $('#mode').addEventListener('change', clearScenario);
  $('#sql').addEventListener('input', clearScenario);
  $('#timeout').addEventListener('input', clearScenario);
  $('#skip-auth').addEventListener('change', clearScenario);
}

function setupKeyboardShortcut() {
  document.addEventListener('keydown', (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key === 'Enter' && $('#tab-execute').classList.contains('active')) {
      event.preventDefault();
      if (!$('#btn-run').disabled) runExecute();
    }
  });
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

function init() {
  setupTabs();
  setupPresets();
  setupScenarioControls();
  setupKeyboardShortcut();
  selectScenario('basic-read', { navigate: false });

  $('#btn-config').addEventListener('click', loadConfig);
  $('#btn-health').addEventListener('click', checkHealth);
  $('#btn-auth-health').addEventListener('click', authHealthTest);
  $('#btn-auth-401').addEventListener('click', auth401Test);
  $('#btn-shutdown').addEventListener('click', shutdownService);

  $('#btn-run').addEventListener('click', runExecute);
  $('#btn-cancel').addEventListener('click', cancelExecute);

  $('#btn-conc').addEventListener('click', runConcurrency);
  $('#btn-backups').addEventListener('click', loadBackups);

  loadConfig();
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}
