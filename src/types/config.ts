export interface AuthConfig {
  mode: 'token' | 'ci' | 'none';
  token?: string;
}

export interface Config {
  version: string;
  sourceDir: string;
  include: string[];
  exclude: string[];
  taxonomy: string[];
  output: string;
  wikiRepo: string;
  auth: AuthConfig;
}

export const DEFAULT_CONFIG: Config = {
  version: '1.0',
  sourceDir: '.',
  include: ['**/*.md'],
  exclude: ['node_modules/**', '.wiki-harness/**', '.git/**'],
  taxonomy: ['Overview', 'Architecture', 'Features', 'Guides', 'ADR'],
  output: '.wiki-harness/output',
  wikiRepo: '',
  auth: { mode: 'none' },
};
