import { source } from '@/lib/source';
import { getLLMText } from '@/lib/get-llm-text';

export const revalidate = false;

export async function GET() {
  const scan = source.getPages().map(getLLMText);
  const results = await Promise.all(scan);
  const scanned = results.filter((r): r is string => r !== null);

  return new Response(scanned.join('\n\n---\n\n'));
}
