import fg from 'fast-glob';
import { stat } from 'fs/promises';
import { join } from 'path';
import type { WikiConfig, SourceFile } from './types.js';

export class ScannerError extends Error {
  constructor(message: string, public readonly warnings: string[]) {
    super(message);
    this.name = 'ScannerError';
  }
}

export class FileScanner {
  private warnings: string[] = [];

  async scan(config: WikiConfig): Promise<SourceFile[]> {
    this.warnings = [];
    const sourceDir = config.sourceDir || '.';
    const files: SourceFile[] = [];

    try {
      const entries = await fg(config.include, {
        cwd: sourceDir,
        absolute: false,
        followSymbolicLinks: false,
        onlyFiles: true,
        ignore: config.exclude,
        dot: false,
      });

      if (entries.length === 0) {
        this.warnings.push(`No markdown files found matching include patterns: ${config.include.join(', ')}`);
        return files;
      }

      for (const entry of entries) {
        try {
          const fullPath = join(sourceDir, entry);
          const stats = await stat(fullPath);

          if (stats.isSymbolicLink()) {
            this.warnings.push(`Skipping symlink: ${entry}`);
            continue;
          }

          files.push({
            path: fullPath,
            relativePath: entry,
            size: stats.size,
            lastModified: stats.mtime,
          });
        } catch (error) {
          const err = error as NodeJS.ErrnoException;
          if (err.code === 'EACCES' || err.code === 'EPERM') {
            this.warnings.push(`Permission denied accessing: ${entry}`);
            continue;
          }
          if (err.code === 'ENOENT') {
            this.warnings.push(`File no longer exists: ${entry}`);
            continue;
          }
          throw error;
        }
      }
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === 'ENOENT') {
        this.warnings.push(`Source directory does not exist: ${sourceDir}`);
        return files;
      }
      if ((error as NodeJS.ErrnoException).code === 'EACCES') {
        throw new ScannerError(`Permission denied accessing source directory: ${sourceDir}`, this.warnings);
      }
      throw new ScannerError(`Failed to scan directory: ${(error as Error).message}`, this.warnings);
    }

    return files;
  }

  getWarnings(): string[] {
    return [...this.warnings];
  }
}

export async function scan(config: WikiConfig): Promise<SourceFile[]> {
  const scanner = new FileScanner();
  return scanner.scan(config);
}