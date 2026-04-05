import type { WikiConfig } from './types.js';
export declare class ConfigValidationError extends Error {
    readonly errors: string[];
    constructor(message: string, errors: string[]);
}
export declare class ConfigLoader {
    private config;
    private configPath;
    constructor(configPath?: string);
    load(overrides?: Partial<WikiConfig>): Promise<WikiConfig>;
    private mergeWithDefaults;
    private validate;
    getConfig(): WikiConfig;
}
export declare function getDefaultConfig(): WikiConfig;
