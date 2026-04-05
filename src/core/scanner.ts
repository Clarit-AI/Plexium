import { readdir, stat } from 'fs/promises';
import { join, relative } from 'path';
import { minimatch } from 'minimatch';

export interface FileInfo {
  path: string;
  relativePath: string;
  size: number;
  modifiedTime: Date;
}

export interface ScannerOptions {
  sourceDir: string;
  include: string[];
  exclude: string[];
}

export class ScannerError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ScannerError';
  }
}

function matchesPatterns(filePath: string, patterns: string[]): boolean {
  return patterns.some(pattern => minimatch(filePath, pattern, { dot: true }));
}

async function scanDirectory(
  dirPath: string,
  basePath: string,
  include: string[],
  exclude: string[],
  results: FileInfo[]
): Promise<void> {
  let entries: string[];
  
  try {
    entries = await readdir(dirPath);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'EACCES') {
      console.warn(`Warning: Permission denied accessing ${dirPath}`);
      return;
    }
    throw error;
  }
  
  for (const entry of entries) {
    const fullPath = join(dirPath, entry);
    const relativePath = relative(basePath, fullPath);
    
    if (matchesPatterns(relativePath, exclude)) {
      continue;
    }
    
    try {
      const stats = await stat(fullPath);
      
      if (stats.isDirectory()) {
        await scanDirectory(fullPath, basePath, include, exclude, results);
      } else if (stats.isFile()) {
        if (matchesPatterns(relativePath, include)) {
          results.push({
            path: fullPath,
            relativePath,
            size: stats.size,
            modifiedTime: stats.mtime,
          });
        }
      }
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === 'EACCES') {
        console.warn(`Warning: Permission denied accessing ${fullPath}`);
        continue;
      }
      throw error;
    }
  }
}

export async function scanFiles(options: ScannerOptions): Promise<FileInfo[]> {
  const { sourceDir, include, exclude } = options;
  
  const results: FileInfo[] = [];
  
  await scanDirectory(sourceDir, sourceDir, include, exclude, results);
  
  results.sort((a, b) => a.relativePath.localeCompare(b.relativePath));
  
  return results;
}