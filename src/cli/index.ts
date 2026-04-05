import { Command } from 'commander';
import { doctorCommand } from './commands/doctor.js';

export const program = new Command();

program
  .name('auto-wiki')
  .description('Automate wiki generation from markdown files')
  .version('1.0.0');

program
  .command('doctor')
  .description('Validate config and report status')
  .action(async () => {
    const exitCode = await doctorCommand();
    process.exit(exitCode);
  });

export default program;