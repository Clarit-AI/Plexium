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
export declare const DEFAULT_CONFIG: Config;
//# sourceMappingURL=config.d.ts.map