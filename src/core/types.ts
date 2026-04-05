export interface WikiConfig {
  version: string;
  sourceDir: string;
  include: string[];
  exclude: string[];
  taxonomy: string[];
  output: string;
  wikiRepo: string;
  auth: WikiAuth;
}

export interface WikiAuth {
  mode: 'token' | 'ci' | 'none';
  token?: string;
}

export interface SourceFile {
  path: string;
  relativePath: string;
  size: number;
  lastModified: Date;
}