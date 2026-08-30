import test from 'node:test';
import assert from 'node:assert/strict';

import { parseEventLine, NDJSONStream } from '../static/ndjson.mjs';

test('parseEventLine parses a valid event line', () => {
  const line = '{"type":"status","request_id":"req_a","state":"queued","at":"2026-08-27T14:31:00Z"}';
  const event = parseEventLine(line);
  assert.equal(event.type, 'status');
  assert.equal(event.request_id, 'req_a');
});

test('parseEventLine returns null for blank lines', () => {
  assert.equal(parseEventLine(''), null);
  assert.equal(parseEventLine('   \n'), null);
  assert.equal(parseEventLine('\n'), null);
});

test('parseEventLine throws for invalid JSON', () => {
  assert.throws(() => parseEventLine('not json'), /invalid NDJSON/i);
});

test('NDJSONStream accepts status → schema → row → summary and builds a model', () => {
  const stream = new NDJSONStream();
  stream.push({ type: 'status', request_id: 'req_1', state: 'queued' });
  stream.push({ type: 'status', request_id: 'req_1', state: 'running' });
  stream.push({ type: 'schema', request_id: 'req_1', columns: [{ name: 'name', duckdb_type: 'VARCHAR' }] });
  stream.push({ type: 'row', request_id: 'req_1', values: ['樱花大道'] });
  stream.push({ type: 'summary', request_id: 'req_1', state: 'finished', row_count: 1, queued_ms: 5, execution_ms: 7 });

  const model = stream.model();
  assert.equal(model.requestId, 'req_1');
  assert.equal(model.state, 'finished');
  assert.deepEqual(model.columns, [{ name: 'name', duckdb_type: 'VARCHAR' }]);
  assert.deepEqual(model.rows, [['樱花大道']]);
  assert.equal(model.rowCount, 1);
  assert.equal(model.queuedMS, 5);
  assert.equal(model.executionMS, 7);
  assert.equal(model.terminalType, 'summary');
});

test('NDJSONStream accepts status → error (no schema/rows)', () => {
  const stream = new NDJSONStream();
  stream.push({ type: 'status', request_id: 'req_2', state: 'queued' });
  stream.push({ type: 'error', request_id: 'req_2', state: 'failed', code: 'sql_failed', message: 'DuckDB query failed' });

  const model = stream.model();
  assert.equal(model.state, 'failed');
  assert.equal(model.errorCode, 'sql_failed');
  assert.equal(model.terminalType, 'error');
  assert.equal(model.rows.length, 0);
});

test('NDJSONStream rejects a row before schema', () => {
  const stream = new NDJSONStream();
  stream.push({ type: 'status', request_id: 'req_3', state: 'running' });
  assert.throws(() => stream.push({ type: 'row', request_id: 'req_3', values: [1] }), /row before schema/i);
});

test('NDJSONStream rejects events after terminal', () => {
  const stream = new NDJSONStream();
  stream.push({ type: 'status', request_id: 'req_4', state: 'queued' });
  stream.push({ type: 'summary', request_id: 'req_4', state: 'finished', row_count: 0 });
  assert.throws(() => stream.push({ type: 'status', request_id: 'req_4', state: 'running' }), /after terminal/i);
});

test('NDJSONStream rejects a second terminal event', () => {
  const stream = new NDJSONStream();
  stream.push({ type: 'status', request_id: 'req_5', state: 'queued' });
  stream.push({ type: 'summary', request_id: 'req_5', state: 'finished', row_count: 0 });
  assert.throws(() => stream.push({ type: 'error', request_id: 'req_5', state: 'failed', code: 'x' }), /terminal/i);
});

test('NDJSONStream rejects unknown event types', () => {
  const stream = new NDJSONStream();
  assert.throws(() => stream.push({ type: 'mystery' }), /unknown event type/i);
});

test('NDJSONStream accepts backing_up for write flows', () => {
  const stream = new NDJSONStream();
  stream.push({ type: 'status', request_id: 'req_6', state: 'queued' });
  stream.push({ type: 'status', request_id: 'req_6', state: 'backing_up' });
  stream.push({ type: 'status', request_id: 'req_6', state: 'running' });
  stream.push({ type: 'summary', request_id: 'req_6', state: 'finished', row_count: 0 });
  assert.equal(stream.model().state, 'finished');
});
