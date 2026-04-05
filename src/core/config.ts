import { readFile } from 'fs/promises';
import { existsSync } from 'fs';
import { resolve } from 'path';
import type { WikiConfig } from './types.js';

const REQUIRED_FIELDS = ['version', 'sourceDir', 'include', 'exclude', 'taxonomy', 'output', 'wikiRepo', 'auth'] as const;

const DEFAULTS = {
  version: '1.0',
  include: ['**/*.md'],
  exclude: ['node_modules/**', '.wiki-harness/**', '.git/**'],
  taxonomy: ['Overview', 'Architecture', 'Features', 'Guides', 'ADR'],
  output: '.wiki-harness/output',
  auth: { mode: 'none' as const },
};

export class ConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ConfigError';
  }
}

export async function loadConfig(configPath?: string): Promise<WikiConfig> {
  const path = configPath || resolve(process.cwd(), '.wiki-harness', 'config.json');
  
  if (!existsSync(path)) {
    throw new ConfigError(`Config file not found: ${path}`);
  }
  
  let content: string;
  try {
    content = await readFile(path, 'utf-8');
  } catch (error) {
    throw new ConfigError(`Failed to read config file: ${path}`);
  }
  
  let parsed: Record<string, unknown>;
  try {
    parsed = JSON.parse(content);
  } catch {
    throw new ConfigError('Invalid JSON in config file');
  }
  
  for (const field of REQUIRED_FIELDS) {
    if (!(field in parsed)) {
      throw new ConfigError(`Missing required field: ${field}`);
    }
  }
  
  if (typeof parsed.version !== 'string') {
    throw new ConfigError('version must be a string');
  }
  if (typeof parsed.sourceDir !== 'string') {
    throw new ConfigError('sourceDir must be a string');
  }
  if (!Array.isArray(parsed.include)) {
    throw new ConfigError('include must be an array');
  }
  if (!Array.isArray(parsed.exclude)) {
    throw new ConfigError('exclude must be an array');
  }
  if (!Array.isArray(parsed.taxonomy)) {
    throw new ConfigError('taxonomy must be an array');
  }
  if (typeof parsed.output !== 'string') {
    throw new ConfigError('output must be a string');
  }
  if (typeof parsed.wikiRepo !== 'string') {
    throw new ConfigError('wikiRepo must be a string');
  }
  
  if (typeof parsed.auth !== 'object' || parsed.auth === null) {
    throw new ConfigError('auth must be an object');
  }
  
  const auth = parsed.auth as Record<string, unknown>;
  if (typeof auth.mode !== 'string') {
    throw new ConfigError('auth.mode must be a string');
  }
  if (!['none', 'token', 'ci'].includes(auth.mode)) {
    throw new ConfigError('auth.mode must be one of: none, token, ci');
  }
  if (auth.mode === 'token' && !auth.token) {
    throw new ConfigError('auth.token is required when auth.mode is token');
  }
  
  const config: WikiConfig = {
    version: parsed.version as string,
    sourceDir: parsed.sourceDir as string,
    include: parsed.include as string[],
    exclude: parsed.exclude as string[],
    taxonomy: parsed.taxonomy as string[],
    output: parsed.output as string,
    wikiRepo: parsed.wikiRepo as string,
    auth: {
      mode: auth.mode as 'none' | 'token' | 'ci',
      token: auth.token as string | undefined,
    },
  };
  
  return applyDefaults(config);
}

function applyDefaults(config: WikiConfig): WikiConfig {
  return {
    ...config,
    include: config.include.length > 0 ? config.include : DEFAULTS.include,
    exclude: config.exclude.length > 0 ? config.exclude : DEFAULTS.exclude,
    taxonomy: config.taxonomy.length > 0 ? config.taxonomy : DEFAULTS.taxonomy,
    output: config.output || DEFAULTS.output,
    auth: config.auth || DEFAULTS.auth,
  };
}

export function validateConfig(config: WikiConfig): string[] {
  const errors: string[] = [];
  
  if (!config.version) errors.push('version is required');
  if (!config.sourceDir) errors.push('sourceDir is required');
  if (!Array.isArray(config.include) || config.include.length === 0) {
    errors.push('include must be a non-empty array');
  }
  if (!Array.isArray(config.exclude)) {
    errors.push('exclude must be an array');
  }
  if (!Array.isArray(config.taxonomy) || config.taxonomy.length === 0) {
    errors.push('taxonomy must be a non-empty array');
  }
  if (!config.output) errors.push('output is required');
  if (!config.wikiRepo) errors.push('wikiRepo is required');
  if (!config.auth || !config.auth.mode) errors.push('auth.mode is required');
  
  return errors;
}