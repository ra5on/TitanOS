const extensions = new Set(['txt', 'md', 'markdown', 'json', 'yaml', 'yml', 'ini', 'conf', 'cfg', 'toml', 'xml', 'csv', 'tsv', 'log', 'sh', 'bash', 'py', 'js', 'ts', 'html', 'css', 'sql', 'env']);
export const MAX_TEXT_BYTES = 512 * 1024;
export function isEditableText(name: string, size: number): boolean {
 return size <= MAX_TEXT_BYTES && (extensions.has(name.split('.').at(-1)?.toLowerCase() ?? '') || ['Dockerfile', 'Makefile', '.env'].includes(name));
}
