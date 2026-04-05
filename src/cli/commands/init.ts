import { mkdir, writeFile, access, readFile } from 'fs/promises';
import { constants } from 'fs';
import { join } from 'path';
import { Config, DEFAULT_CONFIG } from '../../types/config.js';

const HARNESS_DIR = '.wiki-harness';
const CONFIG_FILE = 'config.json';
const OUTPUT_DIR = '.wiki-harness/output';
const GITKEEP = '.gitkeep';

async function fileExists(path: string): Promise<boolean> {
  try {
    await access(path, constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

async function promptOverwrite(): Promise<boolean> {
  const readline = await import('readline');
  const rl = readline.createInterface({
    input: process.stdin,
    output: process.stdout,
  });

  return new Promise((resolve) => {
    rl.question('Config file already exists. Overwrite? (y/N): ', (answer) => {
      rl.close();
      resolve(answer.toLowerCase() === 'y');
    });
  });
}

export async function initAction(force: boolean = false): Promise<void> {
  const harnessPath = join(process.cwd(), HARNESS_DIR);
  const configPath = join(harnessPath, CONFIG_FILE);
  const outputPath = join(process.cwd(), OUTPUT_DIR);
  const gitkeepPath = join(outputPath, GITKEEP);

  const configExists = await fileExists(configPath);

  if (configExists && !force) {
    const overwrite = await promptOverwrite();
    if (!overwrite) {
      console.log('Skipped. Existing config preserved.');
      return;
    }
  }

  await mkdir(harnessPath, { recursive: true });
  await mkdir(outputPath, { recursive: true });

  const configContent = JSON.stringify(DEFAULT_CONFIG, null, 2);
  await writeFile(configPath, configContent, 'utf-8');

  await writeFile(gitkeepPath, '', 'utf-8');

  console.log(`\n✓ Created ${HARNESS_DIR}/`);
  console.log(`✓ Created ${HARNESS_DIR}/${CONFIG_FILE}`);
  console.log(`✓ Created ${OUTPUT_DIR}/${GITKEEP}`);
  console.log('\n---');
  console.log('Next steps:');
  console.log(`1. Edit ${HARNESS_DIR}/${CONFIG_FILE} and set wikiRepo`);
  console.log('2. Run: auto-wiki doctor  # validate configuration');
  console.log('3. Run: auto-wiki scan   # scan your markdown files');
}
