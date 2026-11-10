// Private no-model test command: records synthetic answers, never invokes a tool.
import { writeFileSync } from 'node:fs';

export default function (pi) {
  pi.registerCommand('fixture', {
    description: 'Synthetic interaction only',
    handler: async (_args, ctx) => {
      const output = process.env.FIXTURE_RESULT;
      if (!output || !process.env.FIXTURE_PROVIDER) throw new Error('Explicit fixture environment required');
      writeFileSync(output + '.start', 'started');
      let result;
      if (process.env.FIXTURE_PROVIDER === 'pi') {
        result = await ctx.ui.select('Synthetic region', ['North', 'South']);
      } else if (process.env.FIXTURE_KIND === 'ask') {
        result = await ctx.ui.askDialog([{ id: 'region', question: 'Choose region', options: [{ label: 'North' }, { label: 'South' }] }]);
      } else {
        result = await ctx.ui.approve({ compact: 'Allow tool: bash\nCommand: echo synthetic', full: 'Allow tool: bash\nCommand: echo synthetic' });
      }
      writeFileSync(output, JSON.stringify(result));
    },
  });
}
