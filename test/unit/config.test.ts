import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { resolve, join } from 'path';
import { loadConfig, ConfigError, validateConfig } from '../../src/core/config.js';
import { writeFile, unlink, mkdir } from 'fs/promises';
import { existsSync } from 'fs';

const testConfigDir = resolve(__dirname, '../fixtures/config');

describe('config loader', () => {
  const originalCwd = process.cwd;

  beforeEach(() => {
    vi.spyOn(process, 'cwd').mockReturnValue(resolve(__dirname, '../fixtures'));
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('loadConfig', () => {
    it('loads valid config successfully', async () => {
      const configPath = join(testConfigDir, 'valid.json');
      const config = await loadConfig(configPath);

      expect(config.version).toBe('1.0');
      expect(config.sourceDir).toBe('./docs');
      expect(config.include).toEqual(['**/*.md']);
      expect(config.exclude).toEqual(['node_modules/**', '.wiki-harness/**']);
      expect(config.taxonomy).toEqual(['Overview', 'Architecture', 'Features']);
      expect(config.output).toBe('.wiki-harness/output');
      expect(config.wikiRepo).toBe('https://github.com/example/wiki');
      expect(config.auth.mode).toBe('token');
      expect(config.auth.token).toBe('test-token-123');
    });

    it('applies defaults when fields missing', async () => {
      const configPath = join(testConfigDir, 'partial.json');
      const config = await loadConfig(configPath);

      expect(config.version).toBe('1.0');
      expect(config.include).toEqual(['**/*.md']);
      expect(config.exclude).toEqual(['node_modules/**']);
      expect(config.taxonomy).toEqual(['Overview', 'Architecture', 'Features', 'Guides', 'ADR']);
      expect(config.output).toBe('.wiki-harness/output');
      expect(config.auth.mode).toBe('none');
    });

    it('throws on missing required fields', async () => {
      const configPath = join(testConfigDir, 'missing-fields.json');
      
      await expect(loadConfig(configPath)).rejects.toThrow(ConfigError);
      await expect(loadConfig(configPath)).rejects.toThrow('Missing required field');
    });

    it('throws on invalid field types', async () => {
      const configPath = join(testConfigDir, 'invalid-types.json');
      
      await expect(loadConfig(configPath)).rejects.toThrow(ConfigError);
      await expect(loadConfig(configPath)).rejects.toThrow('Missing required field');
    });

    it('throws on invalid auth mode', async () => {
      const tempPath = join(testConfigDir, 'invalid-auth-mode.json');
      await writeFile(tempPath, JSON.stringify({
        version: '1.0',
        sourceDir: './docs',
        include: ['**/*.md'],
        exclude: ['**/node_modules/**'],
        taxonomy: ['Overview'],
        output: './output',
        wikiRepo: 'https://example.com',
        auth: { mode: 'invalid' }
      }));

      await expect(loadConfig(tempPath)).rejects.toThrow('auth.mode must be one of');
      
      await unlink(tempPath).catch(() => {});
    });

    it('throws on token mode without token', async () => {
      const tempPath = join(testConfigDir, 'missing-token.json');
      await writeFile(tempPath, JSON.stringify({
        version: '1.0',
        sourceDir: './docs',
        include: ['**/*.md'],
        exclude: ['**/node_modules/**'],
        taxonomy: ['Overview'],
        output: './output',
        wikiRepo: 'https://example.com',
        auth: { mode: 'token' }
      }));

      await expect(loadConfig(tempPath)).rejects.toThrow('auth.token is required');
      
      await unlink(tempPath).catch(() => {});
    });

    it('handles missing config file gracefully', async () => {
      const nonExistentPath = join(testConfigDir, 'nonexistent.json');
      
      await expect(loadConfig(nonExistentPath)).rejects.toThrow(ConfigError);
      await expect(loadConfig(nonExistentPath)).rejects.toThrow('Config file not found');
    });

    it('throws on invalid JSON', async () => {
      const tempPath = join(testConfigDir, 'invalid-json.json');
      await writeFile(tempPath, '{ invalid json }');

      await expect(loadConfig(tempPath)).rejects.toThrow(ConfigError);
      await expect(loadConfig(tempPath)).rejects.toThrow('Invalid JSON');
      
      await unlink(tempPath).catch(() => {});
    });
  });

  describe('validateConfig', () => {
    it('returns empty array for valid config', () => {
      const config = {
        version: '1.0',
        sourceDir: './docs',
        include: ['**/*.md'],
        exclude: ['node_modules/**'],
        taxonomy: ['Overview'],
        output: './output',
        wikiRepo: 'https://example.com',
        auth: { mode: 'none' as const }
      };
      
      const errors = validateConfig(config);
      expect(errors).toHaveLength(0);
    });

    it('returns errors for invalid config', () => {
      const config = {
        version: '',
        sourceDir: '',
        include: [],
        exclude: [],
        taxonomy: [],
        output: '',
        wikiRepo: '',
        auth: { mode: 'none' as const }
      };
      
      const errors = validateConfig(config);
      expect(errors.length).toBeGreaterThan(0);
      expect(errors).toContain('version is required');
      expect(errors).toContain('sourceDir is required');
      expect(errors).toContain('include must be a non-empty array');
    });
  });
});