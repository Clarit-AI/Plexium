import { readFile } from 'fs/promises';
import { resolve } from 'path';
import type { WikiConfig } from './types.js';

const DEFAULT_CONFIG = {
  version: '1.0',
  sourceDir: '.',
  include: ['**/*.md'],
  exclude: ['node_modules/**', '.wiki-harness/**'],
  taxonomy: ['Overview', 'Architecture', 'Features', 'Guides', 'ADR'],
  output: '.wiki-harness/output',
  wikiRepo: '',
  auth: { mode: 'none' as const },
};

export class ConfigValidationError extends Error {
  constructor(message: string, public readonly errors: string[]) {
    super(message);
    this.name = 'ConfigValidationError';
  }
}

export class ConfigLoader {
  private config: WikiConfig | null = null;
  private configPath: string;

  constructor(configPath = '.wiki-harness/config.json') {
    this.configPath = configPath;
  }

  async load(overrides?: Partial<WikiConfig>): Promise<WikiConfig> {
    let rawConfig: Partial<WikiConfig> = {};

    try {
      const content = await readFile(this.configPath, 'utf-8');
      rawConfig = JSON.parse(content);
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== 'ENOENT') {
        throw new ConfigValidationError(`Failed to read config file: ${(error as Error).message}`, [
          `Could not read ${this.configPath}`,
        ]);
      }
    }

    const config = this.mergeWithDefaults(rawConfig);
    this.validate(config, overrides);
    this.config = { ...config, ...overrides };
    return this.config;
  }

  private mergeWithDefaults(rawConfig: Partial<WikiConfig>): WikiConfig {
    return {
      version: rawConfig.version ?? DEFAULT_CONFIG.version,
      sourceDir: rawConfig.sourceDir ?? DEFAULT_CONFIG.sourceDir,
      include: rawConfig.include ?? DEFAULT_CONFIG.include,
      exclude: rawConfig.exclude ?? DEFAULT_CONFIG.exclude,
      taxonomy: rawConfig.taxonomy ?? DEFAULT_CONFIG.taxonomy,
      output: rawConfig.output ?? DEFAULT_CONFIG.output,
      wikiRepo: rawConfig.wikiRepo ?? DEFAULT_CONFIG.wikiRepo,
      auth: rawConfig.auth ?? DEFAULT_CONFIG.auth,
    };
  }

  private validate(config: WikiConfig, overrides?: Partial<WikiConfig>): void {
    const errors: string[] = [];

    if (!config.version || typeof config.version !== 'string') {
      errors.push('version is required and must be a string');
    }

    if (!config.sourceDir || typeof config.sourceDir !== 'string') {
      errors.push('sourceDir is required and must be a string');
    }

    if (!Array.isArray(config.include)) {
      errors.push('include must be an array');
    }

    if (!Array.isArray(config.exclude)) {
      errors.push('exclude must be an array');
    }

    if (!Array.isArray(config.taxonomy)) {
      errors.push('taxonomy must be an array');
    }

    if (config.auth) {
      if (!['token', 'ci', 'none'].includes(config.auth.mode)) {
        errors.push('auth.mode must be "token", "ci", or "none"');
      }
    }

    if (errors.length > 0) {
      throw new ConfigValidationError('Invalid configuration', errors);
    }
  }

  getConfig(): WikiConfig {
    if (!this.config) {
      throw new Error('Config not loaded. Call load() first.');
    }
    return this.config;
  }
}

export function getDefaultConfig(): WikiConfig {
  return { ...DEFAULT_CONFIG };
}