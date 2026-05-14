import { source } from '@/lib/source';

export async function getLLMText(page: (typeof source)['$inferPage']) {
  if (!('getText' in page.data)) return null;
  const processed = await page.data.getText('processed');

  return `# ${page.data.title} (${page.url})

${processed}`;
}
