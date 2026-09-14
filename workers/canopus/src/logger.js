'use strict';

const fs = require('fs');
const path = require('path');

class Logger {
  constructor(logDir) {
    this.logDir = logDir;
    const stamp = new Date().toISOString().replace(/[:.]/g, '-');
    this.logFile = path.join(logDir, `run-${stamp}.jsonl`);
    fs.mkdirSync(logDir, { recursive: true });
    this.stream = fs.createWriteStream(this.logFile, { flags: 'a' });
  }

  _write(level, msg, extra) {
    const rec = { ts: new Date().toISOString(), level, msg, ...(extra || {}) };
    this.stream.write(JSON.stringify(rec) + '\n');
    const prefix = { info: '·', warn: '!', error: 'x', ok: '✓' }[level] || '-';
    const extraStr = extra ? ' ' + Object.entries(extra).filter(([k]) => k !== 'stack').map(([k, v]) => `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`).join(' ') : '';
    // eslint-disable-next-line no-console
    console.log(`${prefix} ${msg}${extraStr}`);
    if (extra && extra.stack) console.log(extra.stack);
  }

  info(msg, extra) { this._write('info', msg, extra); }
  ok(msg, extra) { this._write('ok', msg, extra); }
  warn(msg, extra) { this._write('warn', msg, extra); }
  error(msg, extra) { this._write('error', msg, extra); }

  close() {
    this.stream.end();
  }
}

module.exports = { Logger };
