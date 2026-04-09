#!/usr/bin/env node

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');

const args = process.argv.slice(2);
const command = args[0];
const subCommand = args[1];

function emitJson(payload) {
  process.stdout.write(JSON.stringify(payload));
  process.stdout.write('\n');
}

function resolveCodexBin() {
  if (process.env.CODEX_BIN) {
    return process.env.CODEX_BIN;
  }

  const whichResult = spawnSync('which', ['codex'], { encoding: 'utf8' });
  if (whichResult.status === 0) {
    const candidate = whichResult.stdout.trim();
    if (candidate) {
      return candidate;
    }
  }

  const homeDir = process.env.HOME || '';
  const fallbacks = [
    '/opt/homebrew/bin/codex',
    '/usr/local/bin/codex',
    path.join(homeDir, '.local', 'bin', 'codex'),
  ];

  for (const candidate of fallbacks) {
    if (candidate && fs.existsSync(candidate)) {
      return candidate;
    }
  }

  return null;
}

function getCodexHome() {
  if (process.env.CODEX_HOME) {
    return process.env.CODEX_HOME;
  }
  return path.join(process.env.HOME || '', '.codex');
}

function loadSessionIndex() {
  const indexPath = path.join(getCodexHome(), 'session_index.jsonl');
  if (!fs.existsSync(indexPath)) {
    return [];
  }

  const lines = fs.readFileSync(indexPath, 'utf8').split('\n').filter(Boolean);
  return lines
    .map((line) => {
      try {
        return JSON.parse(line);
      } catch (_) {
        return null;
      }
    })
    .filter(Boolean);
}

function findSessionFileById(requestedId) {
  const sessionsRoot = path.join(getCodexHome(), 'sessions');
  if (!fs.existsSync(sessionsRoot)) {
    return null;
  }

  let fallback = null;
  const stack = [sessionsRoot];
  while (stack.length > 0) {
    const current = stack.pop();
    const entries = fs.readdirSync(current, { withFileTypes: true });
    for (const entry of entries) {
      const fullPath = path.join(current, entry.name);
      if (entry.isDirectory()) {
        stack.push(fullPath);
        continue;
      }
      if (!entry.isFile() || !entry.name.endsWith('.jsonl')) {
        continue;
      }

      if (entry.name.includes(requestedId)) {
        return fullPath;
      }
      if (!fallback) {
        fallback = fullPath;
      }
    }
  }
  return fallback;
}

function parseSessionMeta(filePath) {
  try {
    const firstLine = fs.readFileSync(filePath, 'utf8').split('\n').find(Boolean);
    if (!firstLine) {
      return null;
    }
    const entry = JSON.parse(firstLine);
    if (entry.type !== 'session_meta') {
      return null;
    }
    return entry.payload || null;
  } catch (_) {
    return null;
  }
}

function extractText(content) {
  if (!Array.isArray(content)) {
    return '';
  }
  return content
    .filter((item) => item && (item.type === 'input_text' || item.type === 'output_text' || item.type === 'text'))
    .map((item) => item.text || '')
    .join('\n')
    .trim();
}

function loadMessages(filePath) {
  const messages = [];
  const lines = fs.readFileSync(filePath, 'utf8').split('\n').filter(Boolean);
  for (const line of lines) {
    try {
      const entry = JSON.parse(line);
      if (entry.type !== 'response_item') {
        continue;
      }
      const payload = entry.payload || {};
      if (payload.type !== 'message') {
        continue;
      }
      if (payload.role !== 'user' && payload.role !== 'assistant') {
        continue;
      }
      const text = extractText(payload.content);
      if (!text) {
        continue;
      }
      messages.push({
        uuid: entry.id || entry.timestamp,
        role: payload.role,
        text,
        createdAt: entry.timestamp,
      });
    } catch (_) {}
  }
  return messages;
}

function normalizeSession(indexEntry) {
  const sessionId = indexEntry.id;
  const filePath = findSessionFileById(sessionId);
  const stat = filePath && fs.existsSync(filePath) ? fs.statSync(filePath) : null;
  const meta = filePath ? parseSessionMeta(filePath) : null;
  return {
    id: sessionId,
    name: indexEntry.thread_name || `Session ${sessionId.substring(0, 8)}`,
    updatedAt: indexEntry.updated_at || (stat ? stat.mtime.toISOString() : new Date().toISOString()),
    createdAt: (meta && meta.timestamp) || (stat ? stat.birthtime.toISOString() : indexEntry.updated_at || new Date().toISOString()),
    filePath,
  };
}

function resolveSession(requestedId) {
  const sessionIndex = loadSessionIndex().map(normalizeSession);
  if (sessionIndex.length === 0) {
    return null;
  }

  const wanted = requestedId || process.env.CODEX_THREAD_ID || '';
  const exact = sessionIndex.find((entry) => entry.id === wanted);
  if (exact) {
    return exact;
  }

  const prefix = sessionIndex.find((entry) => entry.id.startsWith(wanted));
  if (prefix) {
    return prefix;
  }

  const byPath = wanted ? findSessionFileById(wanted) : null;
  if (byPath) {
    const meta = parseSessionMeta(byPath);
    const stat = fs.statSync(byPath);
    return {
      id: (meta && meta.id) || wanted,
      name: `Session ${((meta && meta.id) || wanted).substring(0, 8)}`,
      updatedAt: stat.mtime.toISOString(),
      createdAt: (meta && meta.timestamp) || stat.birthtime.toISOString(),
      filePath: byPath,
    };
  }

  const sorted = sessionIndex.sort((a, b) => new Date(b.updatedAt) - new Date(a.updatedAt));
  return sorted[0];
}

function main() {
  if (command === 'sessions') {
    if (subCommand === 'list') {
      const sessions = loadSessionIndex()
        .map(normalizeSession)
        .map(({ filePath, name, ...session }) => ({ ...session, title: name, name }));
      sessions.sort((a, b) => new Date(b.updatedAt) - new Date(a.updatedAt));
      emitJson(sessions);
      return;
    }

    if (subCommand === 'get') {
      const requestedId = args[2];
      const session = resolveSession(requestedId);
      if (!session || !session.filePath) {
        console.error(`Session ${requestedId || '(current)'} not found`);
        process.exit(1);
      }

      emitJson({
        id: requestedId || session.id,
        title: session.name,
        name: session.name,
        updatedAt: session.updatedAt,
        createdAt: session.createdAt,
        messages: loadMessages(session.filePath),
      });
      return;
    }
  }

  const codexBin = resolveCodexBin();
  if (!codexBin) {
    console.error('Could not locate the real codex binary. Set CODEX_BIN if needed.');
    process.exit(1);
  }

  const result = spawnSync(codexBin, args, { stdio: 'inherit' });
  if (result.error) {
    console.error(`Error spawning codex: ${result.error.message}`);
    process.exit(1);
  }
  process.exit(result.status !== null ? result.status : 1);
}

main();
