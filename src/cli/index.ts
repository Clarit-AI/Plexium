import { Command } from 'commander';

const program = new Command();

program
  .name('auto-wiki')
  .description('Automate wiki generation from markdown files')
  .version('1.0.0');

program
  .command('init')
  .description('Scaffold .wiki-harness/ directory')
  .action(() => {
    console.log('not yet implemented');
  });

program
  .command('doctor')
  .description('Validate config and report status')
  .action(() => {
    console.log('not yet implemented');
  });

program
  .command('bootstrap')
  .description('Bootstrap wiki repository')
  .action(() => {
    console.log('not yet implemented');
  });

program
  .command('sync')
  .description('Sync markdown to wiki')
  .action(() => {
    console.log('not yet implemented');
  });

program
  .command('lint')
  .description('Lint markdown files')
  .action(() => {
    console.log('not yet implemented');
  });

program.parse();