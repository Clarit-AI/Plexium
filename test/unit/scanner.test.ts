import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { resolve, join } from 'path';
import { scanFiles, FileInfo } from '../../src/core/scanner.js';
import { mkdir, writeFile, rm } from 'fs/promises';

describe('scanner', () => {
  const testDir = resolve(__dirname, '../fixtures/scanner');

  beforeEach(async () => {
    await mkdir(testDir, { recursive: true });
  });

  afterEach(async () => {
    await rm(testDir, { force: true, recursive: true });
  });

  describe('scanFiles', () => {
    it('discovers markdown files matching include patterns', async () => {
      await writeFile(join(testDir, 'doc1.md'), '# Doc 1');
      await writeFile(join(testDir, 'doc2.md'), '# Doc 2');
      await writeFile(join(testDir, 'readme.txt'), 'Not markdown');

      const result = await scanFiles({
        sourceDir: testDir,
        include: ['**/*.md'],
        exclude: []
      });

      expect(result.length).toBe(2);
      expect(result.map(f => f.relativePath).sort()).toEqual(['doc1.md', 'doc2.md']);
    });

    it('excludes files matching exclude patterns', async () => {
      await writeFile(join(testDir, 'doc1.md'), '# Doc 1');
      await writeFile(join(testDir, 'doc2.md'), '# Doc 2');
      await mkdir(join(testDir, 'node_modules'));
      await writeFile(join(testDir, 'node_modules', 'dependency.md'), '# Dependency');

      const result = await scanFiles({
        sourceDir: testDir,
        include: ['**/*.md'],
        exclude: ['node_modules/**']
      });

      expect(result.length).toBe(2);
      expect(result.every(f => !f.relativePath.includes('node_modules'))).toBe(true);
    });

    it('returns correct file metadata', async () => {
      await writeFile(join(testDir, 'doc1.md'), '# Doc 1');

      const result = await scanFiles({
        sourceDir: testDir,
        include: ['**/*.md'],
        exclude: []
      });

      expect(result).toHaveLength(1);
      expect(result[0].path).toBe(join(testDir, 'doc1.md'));
      expect(result[0].relativePath).toBe('doc1.md');
      expect(result[0].size).toBeGreaterThan(0);
      expect(result[0].modifiedTime).toBeInstanceOf(Date);
    });

    it('handles empty source directory', async () => {
      const emptyDir = resolve(__dirname, '../fixtures/empty-dir');
      await mkdir(emptyDir, { recursive: true });

      const result = await scanFiles({
        sourceDir: emptyDir,
        include: ['**/*.md'],
        exclude: []
      });

      expect(result).toHaveLength(0);
    });

    it('handles nested directory structure', async () => {
      await mkdir(join(testDir, 'level1', 'level2'), { recursive: true });
      await writeFile(join(testDir, 'root.md'), '# Root');
      await writeFile(join(testDir, 'level1', 'nested.md'), '# Nested');
      await writeFile(join(testDir, 'level1', 'level2', 'deep.md'), '# Deep');

      const result = await scanFiles({
        sourceDir: testDir,
        include: ['**/*.md'],
        exclude: []
      });

      expect(result.length).toBe(3);
      const paths = result.map(f => f.relativePath).sort();
      expect(paths).toContain('root.md');
      expect(paths).toContain('level1/nested.md');
      expect(paths).toContain('level1/level2/deep.md');
    });

    it('excludes files with specific patterns', async () => {
      await writeFile(join(testDir, 'README.md'), '# README');
      await writeFile(join(testDir, 'CHANGELOG.md'), '# Changelog');
      await writeFile(join(testDir, 'important.md'), '# Important');

      const result = await scanFiles({
        sourceDir: testDir,
        include: ['**/*.md'],
        exclude: ['**/README.md', '**/CHANGELOG.md']
      });

      expect(result.length).toBe(1);
      expect(result[0].relativePath).toBe('important.md');
    });
  });
});