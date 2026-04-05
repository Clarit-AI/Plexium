import { access, readFile, constants } from 'fs/promises';
import { resolve } from 'path';
import fg from 'fast-glob';
import { ConfigLoader, ConfigValidationError } from '../../core/config.js';
import type { WikiConfig } from '../../core/types.js';

const GREEN = '\x1b[32m';
const RED = '\x1b[31m';
const YELLOW = '\x1b[33m';
const RESET = '\x1b[0m';
const BOLD = '\x1b[1m';

function success(msg: string): string {
  return `${GREEN}✓${RESET} ${msg}`;
}

function error(msg: string): string {
  return `${RED}✗${RESET} ${msg}`;
}

function warning(msg: string): string {
  return `${YELLOW}⚠${RESET} ${msg}`;
}

function section(title: string): void {
  console.log(`\n${BOLD}${title}${RESET}`);
  console.log('─'.repeat(40));
}

interface CheckResult {
  passed: boolean;
  message: string;
}

export async function checkConfigFileExists(configPath: string): Promise<CheckResult> {
  try {
    await access(configPath, constants.R_OK);
    return { passed: true, message: `Config file found at ${configPath}` };
  } catch {
    return { passed: false, message: `Config file not found at ${configPath}` };
  }
}

export async function checkConfigValidJson(configPath: string): Promise<CheckResult> {
  try {
    const content = await readFile(configPath, 'utf-8');
    JSON.parse(content);
    return { passed: true, message: 'Config is valid JSON' };
  } catch (e) {
    const msg = e instanceof Error ? e.message : 'Unknown error';
    return { passed: false, message: `Invalid JSON: ${msg}` };
  }
}

export function checkRequiredFields(config: Partial<WikiConfig>): CheckResult {
  const errors: string[] = [];
  
  if (!config.version) errors.push('version');
  if (!config.sourceDir) errors.push('sourceDir');
  
  if (errors.length > 0) {
    return { passed: false, message: `Missing required fields: ${errors.join(', ')}` };
  }
  return { passed: true, message: 'Required fields present (version, sourceDir)' };
}

export async function checkSourceDir(config: WikiConfig): Promise<CheckResult> {
  const sourceDir = resolve(config.sourceDir);
  try {
    await access(sourceDir, constants.R_OK);
    return { passed: true, message: `sourceDir exists and is readable: ${config.sourceDir}` };
  } catch {
    return { passed: false, message: `sourceDir is not accessible: ${config.sourceDir}` };
  }
}

export function checkIncludePatterns(config: WikiConfig): CheckResult {
  if (!Array.isArray(config.include) || config.include.length === 0) {
    return { passed: false, message: 'include patterns must be a non-empty array' };
  }
  
  const pattern = config.include[0];
  if (!pattern || typeof pattern !== 'string' || pattern.length === 0) {
    return { passed: false, message: `Invalid include pattern: ${pattern}` };
  }
  
  return { passed: true, message: `Include patterns valid: ${config.include.join(', ')}` };
}

export function checkExcludePatterns(config: WikiConfig): CheckResult {
  if (!Array.isArray(config.exclude)) {
    return { passed: false, message: 'exclude patterns must be an array' };
  }
  
  for (const pattern of config.exclude) {
    if (!pattern || typeof pattern !== 'string') {
      return { passed: false, message: 'Invalid exclude pattern found' };
    }
  }
  
  return { passed: true, message: `Exclude patterns valid: ${config.exclude.join(', ')}` };
}

export function checkTaxonomy(config: WikiConfig): CheckResult {
  if (!Array.isArray(config.taxonomy) || config.taxonomy.length === 0) {
    return { passed: false, message: 'taxonomy must be a non-empty array' };
  }
  return { passed: true, message: `Taxonomy has ${config.taxonomy.length} categories` };
}

export function checkAuthMode(config: WikiConfig): CheckResult {
  const validModes = ['token', 'ci', 'none'];
  if (!config.auth || !validModes.includes(config.auth.mode)) {
    return { passed: false, message: `Invalid auth mode: ${config.auth?.mode}` };
  }
  
  if (config.auth.mode === 'token' && !config.auth.token) {
    return { passed: false, message: 'Auth mode is "token" but no token configured' };
  }
  
  if (config.auth.mode === 'token' && config.auth.token) {
    return { passed: true, message: 'Auth mode is "token" with token configured' };
  }
  
  return { passed: true, message: `Auth mode is "${config.auth.mode}"` };
}

export async function scanFiles(config: WikiConfig): Promise<{ found: number; excluded: number; sample: string[] }> {
  const sourceDir = resolve(config.sourceDir);
  
  const allMatches = await fg(config.include, {
    cwd: sourceDir,
    onlyFiles: true,
    dot: false,
  });
  
  const excludedMatches = await fg(config.exclude, {
    cwd: sourceDir,
    onlyFiles: true,
    dot: false,
  });
  
  const finalMatches = await fg(config.include, {
    cwd: sourceDir,
    onlyFiles: true,
    ignore: config.exclude,
    dot: false,
  });
  
  const sample = finalMatches.slice(0, 5);
  
  return {
    found: finalMatches.length,
    excluded: allMatches.length - finalMatches.length,
    sample,
  };
}

export async function doctorCommand(): Promise<number> {
  const configPath = '.wiki-harness/config.json';
  let hasErrors = false;
  
  console.log(`${BOLD}AutoWiki Doctor${RESET} - Configuration & Status Report`);
  
  section('Configuration Checks');
  
  const configExists = await checkConfigFileExists(configPath);
  console.log(configExists.passed ? success(configExists.message) : error(configExists.message));
  if (!configExists.passed) {
    hasErrors = true;
    return 1;
  }
  
  const validJson = await checkConfigValidJson(configPath);
  console.log(validJson.passed ? success(validJson.message) : error(validJson.message));
  if (!validJson.passed) {
    hasErrors = true;
    return 1;
  }
  
  const loader = new ConfigLoader(configPath);
  let config: WikiConfig;
  
  try {
    config = await loader.load();
  } catch (e) {
    if (e instanceof ConfigValidationError) {
      console.log(error('Config validation failed:'));
      for (const err of e.errors) {
        console.log(`  ${RED}•${RESET} ${err}`);
      }
    } else {
      console.log(error(`Failed to load config: ${e}`));
    }
    return 1;
  }
  
  const requiredFields = checkRequiredFields(config);
  console.log(requiredFields.passed ? success(requiredFields.message) : error(requiredFields.message));
  if (!requiredFields.passed) hasErrors = true;
  
  const sourceDir = await checkSourceDir(config);
  console.log(sourceDir.passed ? success(sourceDir.message) : error(sourceDir.message));
  if (!sourceDir.passed) hasErrors = true;
  
  const includePatterns = checkIncludePatterns(config);
  console.log(includePatterns.passed ? success(includePatterns.message) : error(includePatterns.message));
  if (!includePatterns.passed) hasErrors = true;
  
  const excludePatterns = checkExcludePatterns(config);
  console.log(excludePatterns.passed ? success(excludePatterns.message) : error(excludePatterns.message));
  if (!excludePatterns.passed) hasErrors = true;
  
  const taxonomy = checkTaxonomy(config);
  console.log(taxonomy.passed ? success(taxonomy.message) : error(taxonomy.message));
  if (!taxonomy.passed) hasErrors = true;
  
  const auth = checkAuthMode(config);
  console.log(auth.passed ? success(auth.message) : error(auth.message));
  if (!auth.passed) hasErrors = true;
  
  section('File Scan Results');
  
  const scanResults = await scanFiles(config);
  console.log(`${GREEN}•${RESET} Markdown files found: ${scanResults.found}`);
  console.log(`${YELLOW}•${RESET} Files excluded by patterns: ${scanResults.excluded}`);
  
  if (scanResults.sample.length > 0) {
    console.log(`\n${BOLD}Sample files that would be processed:${RESET}`);
    for (const file of scanResults.sample) {
      console.log(`  ${file}`);
    }
  } else {
    console.log(warning('No markdown files found matching include patterns'));
  }
  
  section('Summary');
  
  if (hasErrors) {
    console.log(error('Some checks failed. Please fix the issues above.'));
    return 1;
  } else {
    console.log(success('All checks passed! Your wiki-harness is ready.'));
    return 0;
  }
}