import { describe, it, expect, beforeAll } from 'vitest';
import { readFile } from 'fs/promises';
import { resolve, join } from 'path';
import { normalize } from '../../src/core/normalizer.js';

const fixturesDir = resolve(__dirname, '../fixtures/markdown');

describe('normalizer', () => {
  describe('normalize', () => {
    it('parses valid markdown and extracts title from H1', async () => {
      const content = await readFile(join(fixturesDir, 'valid.md'), 'utf-8');
      const result = await normalize(content, 'valid.md');

      expect(result.title).toBe('Getting Started');
    });

    it('extracts heading tree correctly', async () => {
      const content = await readFile(join(fixturesDir, 'valid.md'), 'utf-8');
      const result = await normalize(content, 'valid.md');

      expect(result.headings).toHaveLength(1);
      expect(result.headings[0].text).toBe('Getting Started');
      expect(result.headings[0].depth).toBe(1);
      expect(result.headings[0].children).toHaveLength(2);
      expect(result.headings[0].children[0].text).toBe('Installation');
      expect(result.headings[0].children[1].text).toBe('Configuration');
    });

    it('counts words accurately', async () => {
      const content = await readFile(join(fixturesDir, 'valid.md'), 'utf-8');
      const result = await normalize(content, 'valid.md');

      expect(result.wordCount).toBeGreaterThan(10);
      expect(result.wordCount).toBeLessThan(50);
    });

    it('handles empty content', async () => {
      const result = await normalize('', 'empty.md');

      expect(result.title).toBe('');
      expect(result.headings).toHaveLength(0);
      expect(result.wordCount).toBe(0);
    });

    it('handles malformed markdown without crashing', async () => {
      const content = await readFile(join(fixturesDir, 'malformed.md'), 'utf-8');
      const result = await normalize(content, 'malformed.md');

      expect(result.title).toBe('Title');
      expect(result.headings).toHaveLength(1);
      expect(result.wordCount).toBeGreaterThan(0);
    });

    it('extracts frontmatter when present', async () => {
      const content = await readFile(join(fixturesDir, 'valid.md'), 'utf-8');
      const result = await normalize(content, 'valid.md');

      expect(result.frontmatter).toBeDefined();
      expect(result.frontmatter?.title).toBe('Sample Document');
      expect(result.frontmatter?.author).toBe('Test Author');
    });

    it('handles markdown without frontmatter', async () => {
      const content = await readFile(join(fixturesDir, 'no-frontmatter.md'), 'utf-8');
      const result = await normalize(content, 'no-frontmatter.md');

      expect(result.frontmatter).toBeUndefined();
      expect(result.title).toBe('Document Title');
    });
  });
});