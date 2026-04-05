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

export interface NormalizedDoc {
  path: string;
  title: string;
  headings: Heading[];
  wordCount: number;
  frontmatter?: Record<string, unknown>;
  ast: unknown;
}

export interface Heading {
  level: number;
  text: string;
  slug: string;
}

export interface SourceFile {
  path: string;
  relativePath: string;
  size: number;
  lastModified: Date;
}