import { Command } from 'commander';
import { initAction } from './commands/init.js';
export function createProgram() {
    const program = new Command();
    program
        .name('auto-wiki')
        .description('Automate wiki generation from markdown files')
        .version('1.0.0');
    program
        .command('init')
        .description('Initialize .wiki-harness directory with default config')
        .option('-f, --force', 'Overwrite existing config without prompting')
        .action(async (options) => {
        await initAction(options.force);
    });
    return program;
}
export { initAction };
//# sourceMappingURL=index.js.map