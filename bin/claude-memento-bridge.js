#!/usr/bin/env node

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');

// Determine actual claude binary path (/opt/homebrew/bin/claude or ~/.claude/local/bin/claude)
const CLAUDE_BIN = '/opt/homebrew/bin/claude';

const args = process.argv.slice(2);
const command = args[0];
const subCommand = args[1];

function getProjectDir() {
  const cwd = process.cwd().replace(/[^a-zA-Z0-9]/g, '-');
  return path.join(process.env.HOME, '.claude', 'projects', cwd);
}

if (command === 'sessions') {
  const projectDir = getProjectDir();
  
  if (!fs.existsSync(projectDir)) {
    console.log(JSON.stringify(subCommand === 'list' ? [] : { error: "No sessions found" }));
    process.exit(0);
  }

  if (subCommand === 'list') {
    const files = fs.readdirSync(projectDir).filter(f => f.endsWith('.jsonl'));
    const sessions = files.map(file => {
      const filePath = path.join(projectDir, file);
      const stat = fs.statSync(filePath);
      return {
        id: file.replace('.jsonl', ''),
        name: `Session ${file.substring(0, 8)}`,
        updatedAt: stat.mtime.toISOString(),
        createdAt: stat.birthtime.toISOString()
      };
    });
    // Sort by updated descending
    sessions.sort((a, b) => new Date(b.updatedAt) - new Date(a.updatedAt));
    console.log(JSON.stringify(sessions));
    process.exit(0);
  }

  if (subCommand === 'get') {
    const requestedId = args[2];
    let filePath = path.join(projectDir, `${requestedId}.jsonl`);
    let actualId = requestedId;
    
    if (!fs.existsSync(filePath)) {
      const files = fs.readdirSync(projectDir).filter(f => f.endsWith('.jsonl'));
      // try prefix match
      const prefixMatch = files.find(f => f.startsWith(requestedId));
      if (prefixMatch) {
        filePath = path.join(projectDir, prefixMatch);
        actualId = prefixMatch.replace('.jsonl', '');
      } else if (files.length > 0) {
        // Fallback to the MOST RECENT session if they provided an arbitrary alias
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
    let sessionName = requestedId !== actualId ? requestedId : `Session ${actualId.substring(0, 8)}`;
    
    for (const line of lines) {
      try {
        const item = JSON.parse(line);
        if (item.message && item.message.content) {
          const text = item.message.content
            .filter(c => c.type === 'text')
            .map(c => c.text)
            .join('\n');
            
          messages.push({
            uuid: item.uuid || item.timestamp,
            role: item.message.role || item.type,
            text: text,
            createdAt: item.timestamp
          });
        }
      } catch (e) {}
    }

    const stat = fs.statSync(filePath);
    console.log(JSON.stringify({
      id: requestedId, // ALWAYS return requested ID so memento doesn't reject it as a mismatch
      name: sessionName,
      updatedAt: stat.mtime.toISOString(),
      messages: messages
    }));
    process.exit(0);
  }
}

// Fallback to real claude for everything else (like `-p --append-system-prompt ...`)
const result = spawnSync(CLAUDE_BIN, args, { stdio: 'inherit' });
if (result.error) {
  console.error(`Error spawning claude: ${result.error.message}`);
  process.exit(1);
}
process.exit(result.status !== null ? result.status : 1);
