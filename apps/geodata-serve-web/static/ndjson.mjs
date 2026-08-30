// NDJSON stream parser and accumulator for the geodata-serve HTTP interface.
//
// Pure logic module: no DOM, no I/O. It turns the wire event stream
// (status → [status …] → [schema → row …] → summary | error) into a render
// model for the UI. It is imported by app.js in the browser and unit-tested
// directly in Node.

const EVENT_TYPES = new Set(['status', 'schema', 'row', 'summary', 'error']);

export function parseEventLine(line) {
  if (typeof line !== 'string') {
    throw new Error('invalid NDJSON line: not a string');
  }
  const trimmed = line.trim();
  if (trimmed === '') {
    return null;
  }
  try {
    return JSON.parse(trimmed);
  } catch (cause) {
    throw new Error(`invalid NDJSON line: ${trimmed.slice(0, 200)}`);
  }
}

export class NDJSONStream {
  constructor() {
    this.events = [];
    this.columns = [];
    this.rows = [];
    this.states = [];
    this.requestId = null;
    this.terminal = null; // { type: 'summary' | 'error', event }
    this.schemaSeen = false;
  }

  push(event) {
    if (!event || typeof event !== 'object' || Array.isArray(event)) {
      throw new Error('invalid NDJSON event: expected an object');
    }
    const type = event.type;
    if (!EVENT_TYPES.has(type)) {
      throw new Error(`unknown event type: ${String(type)}`);
    }
    if (this.terminal) {
      throw new Error('received event after terminal event');
    }
    if (this.events.length === 0 && type !== 'status') {
      throw new Error('stream must begin with a status event');
    }
    this.rememberRequestId(event.request_id);

    switch (type) {
      case 'status':
        if (typeof event.state !== 'string' || event.state === '') {
          throw new Error('status event is missing state');
        }
        this.states.push(event.state);
        break;
      case 'schema':
        if (this.schemaSeen) {
          throw new Error('duplicate schema event');
        }
        if (!Array.isArray(event.columns)) {
          throw new Error('schema event is missing columns');
        }
        this.schemaSeen = true;
        this.columns = event.columns;
        break;
      case 'row':
        if (!this.schemaSeen) {
          throw new Error('row before schema event');
        }
        if (!Array.isArray(event.values)) {
          throw new Error('row event is missing values');
        }
        this.rows.push(event.values);
        break;
      case 'summary':
        this.terminal = { type: 'summary', event };
        break;
      case 'error':
        this.terminal = { type: 'error', event };
        break;
      default:
        throw new Error(`unknown event type: ${String(type)}`);
    }

    this.events.push(event);
    return event;
  }

  rememberRequestId(id) {
    if (this.requestId == null && typeof id === 'string' && id !== '') {
      this.requestId = id;
    }
  }

  get isTerminal() {
    return this.terminal !== null;
  }

  model() {
    const terminal = this.terminal;
    let state = this.states[this.states.length - 1] ?? null;
    let rowCount = this.rows.length;
    let queuedMS = 0;
    let executionMS = 0;
    let errorCode = null;
    let errorMessage = null;

    if (terminal) {
      const ev = terminal.event;
      state = ev.state ?? state;
      if (terminal.type === 'summary') {
        if (Number.isFinite(ev.row_count)) rowCount = ev.row_count;
        queuedMS = ev.queued_ms ?? 0;
        executionMS = ev.execution_ms ?? 0;
      } else {
        errorCode = ev.code ?? null;
        errorMessage = ev.message ?? null;
        queuedMS = ev.queued_ms ?? 0;
        executionMS = ev.execution_ms ?? 0;
      }
    }

    return {
      requestId: this.requestId,
      state,
      states: [...this.states],
      columns: this.columns,
      rows: this.rows,
      rowCount,
      queuedMS,
      executionMS,
      errorCode,
      errorMessage,
      terminalType: terminal ? terminal.type : null,
      eventCount: this.events.length,
      hasSchema: this.schemaSeen,
    };
  }
}
