export interface WikiConfig {
  version: string;
  sourceDir: string;
  include: string[];
  exclude: string[];
  taxonomy: string[];
  output: string;
  wikiRepo: string;
  auth: {
    mode: 'none' | 'token' | 'ci';
    token?: string;
  };
}

export const DEFAULT_CONFIG: Partial<WikiConfig> = {
  version: '1.0',
  include: ['**/*.md'],
  exclude: ['node_modules/**', '.wiki-harness/**', '.git/**'],
  taxonomy: ['Overview', 'Architecture', 'Features', 'Guides', 'ADR'],
  output: '.wiki-harness/output',
  auth: { mode: 'none' },
};

export function mergeConfig(
  base: WikiConfig,
  overrides: Partial<WikiConfig>
): WikiConfig {
  return {
    ...base,
    ...overrides,
    auth: { ...base.auth, ...overrides.auth },
  };
}