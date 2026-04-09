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

function resolveClaudeBin() {
  if (process.env.CLAUDE_BIN) {
    return process.env.CLAUDE_BIN;
  }

  const whichResult = spawnSync('which', ['claude'], { encoding: 'utf8' });
  if (whichResult.status === 0) {
    const candidate = whichResult.stdout.trim();
    if (candidate) {
      return candidate;
    }
  }

  const homeDir = process.env.HOME || '';
  const fallbacks = [
    '/opt/homebrew/bin/claude',
    '/usr/local/bin/claude',
    path.join(homeDir, '.claude', 'local', 'bin', 'claude'),
  ];

  for (const candidate of fallbacks) {
    if (candidate && fs.existsSync(candidate)) {
      return candidate;
    }
  }

  return null;
}

function getProjectDir() {
  const cwd = process.cwd().replace(/[^a-zA-Z0-9]/g, '-');
  return path.join(process.env.HOME, '.claude', 'projects', cwd);
}

function main() {
  if (command === 'sessions') {
    const projectDir = getProjectDir();

    if (!fs.existsSync(projectDir)) {
      emitJson(subCommand === 'list' ? [] : { error: 'No sessions found' });
      return;
    }

    if (subCommand === 'list') {
      const files = fs.readdirSync(projectDir).filter((f) => f.endsWith('.jsonl'));
      const sessions = files.map((file) => {
        const filePath = path.join(projectDir, file);
        const stat = fs.statSync(filePath);
        const title = `Session ${file.substring(0, 8)}`;
        return {
          id: file.replace('.jsonl', ''),
          title,
          name: title,
          updatedAt: stat.mtime.toISOString(),
          createdAt: stat.birthtime.toISOString(),
        };
      });
      sessions.sort((a, b) => new Date(b.updatedAt) - new Date(a.updatedAt));
      emitJson(sessions);
      return;
    }

    if (subCommand === 'get') {
      const requestedId = args[2];
      let filePath = path.join(projectDir, `${requestedId}.jsonl`);
      let actualId = requestedId;

      if (!fs.existsSync(filePath)) {
        const files = fs.readdirSync(projectDir).filter((f) => f.endsWith('.jsonl'));
        const prefixMatch = files.find((f) => f.startsWith(requestedId));
        if (prefixMatch) {
          filePath = path.join(projectDir, prefixMatch);
          actualId = prefixMatch.replace('.jsonl', '');
        } else if (files.length > 0) {
          files.sort((a, b) => fs.statSync(path.join(projectDir, b)).mtimeMs - fs.statSync(path.join(projectDir, a)).mtimeMs);
          filePath = path.join(projectDir, files[0]);
          actualId = files[0].replace('.jsonl', '');
        } else {
          console.error(`Session ${requestedId} not found`);
          process.exit(1);
        }
      }

      const lines = fs.readFileSync(filePath, 'utf8').split('\n').filter(Boolean);
      const messages = [];
      const sessionTitle = requestedId !== actualId ? requestedId : `Session ${actualId.substring(0, 8)}`;

      for (const line of lines) {
        try {
          const item = JSON.parse(line);
          if (item.message && item.message.content) {
            const text = item.message.content
              .filter((c) => c.type === 'text')
              .map((c) => c.text)
              .join('\n');

            messages.push({
              uuid: item.uuid || item.timestamp,
              role: item.message.role || item.type,
              text,
              createdAt: item.timestamp,
            });
          }
        } catch (e) {}
      }

      const stat = fs.statSync(filePath);
      emitJson({
        id: requestedId,
        title: sessionTitle,
        name: sessionTitle,
        updatedAt: stat.mtime.toISOString(),
        messages,
      });
      return;
    }
  }

  const claudeBin = resolveClaudeBin();
  if (!claudeBin) {
    console.error('Could not locate the real claude binary. Set CLAUDE_BIN if needed.');
    process.exit(1);
  }

  const result = spawnSync(claudeBin, args, { stdio: 'inherit' });
  if (result.error) {
    console.error(`Error spawning claude: ${result.error.message}`);
    process.exit(1);
  }
  process.exit(result.status !== null ? result.status : 1);
}

main();
