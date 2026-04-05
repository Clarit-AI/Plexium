import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { resolve, join } from 'path';
import { readFile, writeFile, mkdir, access, unlink } from 'fs/promises';
import { constants } from 'fs';
import type { WikiConfig } from '../../src/core/types.js';

const testConfigDir = resolve(__dirname, '../fixtures/doctor');

async function createTestConfig(config: Partial<WikiConfig> & { version: string; sourceDir: string }) {
  const testConfig = {
    version: '1.0',
    sourceDir: './docs',
    include: ['**/*.md'],
    exclude: ['node_modules/**', '.wiki-harness/**'],
    taxonomy: ['Overview', 'Architecture', 'Features'],
    output: '.wiki-harness/output',
    wikiRepo: 'https://github.com/example/wiki',
    auth: { mode: 'none' as const },
    ...config,
  };
  const configPath = join(testConfigDir, 'config.json');
  await writeFile(configPath, JSON.stringify(testConfig, null, 2));
  return configPath;
}

describe('doctor command checks', () => {
  const originalCwd = process.cwd;

  beforeEach(async () => {
    vi.spyOn(process, 'cwd').mockReturnValue(resolve(__dirname, '../fixtures'));
    await mkdir(testConfigDir, { recursive: true });
    const docsDir = resolve(__dirname, '../fixtures/doctor/docs');
    await mkdir(docsDir, { recursive: true });
    await writeFile(resolve(docsDir, 'test.md'), '# Test\n\nContent');
    await writeFile(resolve(docsDir, 'nested.md'), '---\ntitle: Nested\n---\n# Nested');
  });

  afterEach(async () => {
    vi.restoreAllMocks();
    const files = ['valid.json', 'invalid-json.json', 'missing-fields.json', 'missing-token.json', 'invalid-auth.json'];
    for (const file of files) {
      await unlink(join(testConfigDir, file)).catch(() => {});
    }
    const docsDir = resolve(__dirname, '../fixtures/doctor/docs');
    await unlink(join(docsDir, 'test.md')).catch(() => {});
    await unlink(join(docsDir, 'nested.md')).catch(() => {});
  });

  describe('checkConfigFileExists', async () => {
    const { checkConfigFileExists } = await import('../../src/cli/commands/doctor.js');

    it('returns passed when config file exists', async () => {
      const configPath = join(testConfigDir, 'valid.json');
      await writeFile(configPath, '{}');
      const result = await checkConfigFileExists(configPath);
      expect(result.passed).toBe(true);
      expect(result.message).toContain('found');
    });

    it('returns failed when config file does not exist', async () => {
      const result = await checkConfigFileExists(join(testConfigDir, 'nonexistent.json'));
      expect(result.passed).toBe(false);
      expect(result.message).toContain('not found');
    });
  });

  describe('checkConfigValidJson', async () => {
    const { checkConfigValidJson } = await import('../../src/cli/commands/doctor.js');

    it('returns passed for valid JSON', async () => {
      const configPath = await createTestConfig({ version: '1.0', sourceDir: './docs' });
      const result = await checkConfigValidJson(configPath);
      expect(result.passed).toBe(true);
      expect(result.message).toContain('valid JSON');
    });

    it('returns failed for invalid JSON', async () => {
      const configPath = join(testConfigDir, 'invalid-json.json');
      await writeFile(configPath, '{ invalid json }');
      const result = await checkConfigValidJson(configPath);
      expect(result.passed).toBe(false);
      expect(result.message).toContain('Invalid JSON');
    });
  });

  describe('checkRequiredFields', async () => {
    const { checkRequiredFields } = await import('../../src/cli/commands/doctor.js');

    it('returns passed when required fields present', () => {
      const config = { version: '1.0', sourceDir: './docs' };
      const result = checkRequiredFields(config);
      expect(result.passed).toBe(true);
      expect(result.message).toContain('present');
    });

    it('returns failed when version missing', () => {
      const config = { sourceDir: './docs' } as Partial<WikiConfig>;
      const result = checkRequiredFields(config);
      expect(result.passed).toBe(false);
      expect(result.message).toContain('version');
    });

    it('returns failed when sourceDir missing', () => {
      const config = { version: '1.0' } as Partial<WikiConfig>;
      const result = checkRequiredFields(config);
      expect(result.passed).toBe(false);
      expect(result.message).toContain('sourceDir');
    });
  });

  describe('checkSourceDir', async () => {
    const { checkSourceDir } = await import('../../src/cli/commands/doctor.js');

    it('returns passed when sourceDir is readable', async () => {
      const config = { version: '1.0', sourceDir: resolve(__dirname, '../fixtures/doctor/docs') };
      const result = await checkSourceDir(config);
      expect(result.passed).toBe(true);
    });

    it('returns failed when sourceDir is not accessible', async () => {
      const config = { version: '1.0', sourceDir: resolve(__dirname, '../fixtures/doctor/nonexistent') };
      const result = await checkSourceDir(config);
      expect(result.passed).toBe(false);
    });
  });

  describe('checkIncludePatterns', async () => {
    const { checkIncludePatterns } = await import('../../src/cli/commands/doctor.js');

    it('returns passed for valid include patterns', () => {
      const config = { version: '1.0', sourceDir: './docs', include: ['**/*.md'] } as WikiConfig;
      const result = checkIncludePatterns(config);
      expect(result.passed).toBe(true);
    });

    it('returns failed for empty include array', () => {
      const config = { version: '1.0', sourceDir: './docs', include: [] } as WikiConfig;
      const result = checkIncludePatterns(config);
      expect(result.passed).toBe(false);
      expect(result.message).toContain('non-empty');
    });

    it('returns failed for invalid pattern type', () => {
      const config = { version: '1.0', sourceDir: './docs', include: [''] } as WikiConfig;
      const result = checkIncludePatterns(config);
      expect(result.passed).toBe(false);
    });
  });

  describe('checkExcludePatterns', async () => {
    const { checkExcludePatterns } = await import('../../src/cli/commands/doctor.js');

    it('returns passed for valid exclude patterns', () => {
      const config = { version: '1.0', sourceDir: './docs', exclude: ['node_modules/**'] } as WikiConfig;
      const result = checkExcludePatterns(config);
      expect(result.passed).toBe(true);
    });

    it('returns failed for non-array exclude', () => {
      const config = { version: '1.0', sourceDir: './docs', exclude: 'string' } as unknown as WikiConfig;
      const result = checkExcludePatterns(config);
      expect(result.passed).toBe(false);
      expect(result.message).toContain('array');
    });

    it('returns failed for invalid pattern in array', () => {
      const config = { version: '1.0', sourceDir: './docs', exclude: [null as unknown as string] } as unknown as WikiConfig;
      const result = checkExcludePatterns(config);
      expect(result.passed).toBe(false);
    });
  });

  describe('checkTaxonomy', async () => {
    const { checkTaxonomy } = await import('../../src/cli/commands/doctor.js');

    it('returns passed for non-empty taxonomy', () => {
      const config = { version: '1.0', sourceDir: './docs', taxonomy: ['Overview'] } as WikiConfig;
      const result = checkTaxonomy(config);
      expect(result.passed).toBe(true);
      expect(result.message).toContain('1');
    });

    it('returns failed for empty taxonomy', () => {
      const config = { version: '1.0', sourceDir: './docs', taxonomy: [] } as WikiConfig;
      const result = checkTaxonomy(config);
      expect(result.passed).toBe(false);
    });
  });

  describe('checkAuthMode', async () => {
    const { checkAuthMode } = await import('../../src/cli/commands/doctor.js');

    it('returns passed for valid auth mode "none"', () => {
      const config = { version: '1.0', sourceDir: './docs', auth: { mode: 'none' } } as WikiConfig;
      const result = checkAuthMode(config);
      expect(result.passed).toBe(true);
    });

    it('returns passed for valid auth mode "ci"', () => {
      const config = { version: '1.0', sourceDir: './docs', auth: { mode: 'ci' } } as WikiConfig;
      const result = checkAuthMode(config);
      expect(result.passed).toBe(true);
    });

    it('returns passed for token mode with token configured', () => {
      const config = { version: '1.0', sourceDir: './docs', auth: { mode: 'token', token: 'secret' } } as WikiConfig;
      const result = checkAuthMode(config);
      expect(result.passed).toBe(true);
      expect(result.message).toContain('token configured');
    });

    it('returns failed for token mode without token', () => {
      const config = { version: '1.0', sourceDir: './docs', auth: { mode: 'token' } } as WikiConfig;
      const result = checkAuthMode(config);
      expect(result.passed).toBe(false);
      expect(result.message).toContain('no token');
    });

    it('returns failed for invalid auth mode', () => {
      const config = { version: '1.0', sourceDir: './docs', auth: { mode: 'invalid' } } as WikiConfig;
      const result = checkAuthMode(config);
      expect(result.passed).toBe(false);
    });
  });

  describe('scanFiles', async () => {
    const { scanFiles } = await import('../../src/cli/commands/doctor.js');

    it('returns correct counts for matching files', async () => {
      const config = { version: '1.0', sourceDir: './docs', include: ['**/*.md'], exclude: [] } as WikiConfig;
      const docsDir = resolve(__dirname, '../fixtures/doctor/docs');
      config.sourceDir = docsDir;
      const result = await scanFiles(config);
      expect(result.found).toBe(2);
      expect(result.sample.length).toBe(2);
    });

    it('excludes files matching exclude patterns', async () => {
      const config = { version: '1.0', sourceDir: './docs', include: ['**/*.md'], exclude: ['**/nested.md'] } as WikiConfig;
      const docsDir = resolve(__dirname, '../fixtures/doctor/docs');
      config.sourceDir = docsDir;
      const result = await scanFiles(config);
      expect(result.found).toBe(1);
      expect(result.sample[0]).toContain('test.md');
    });
  });
});