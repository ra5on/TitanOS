import type { FileRoot } from './api.ts';
export function preferredFileRoot(roots: FileRoot[], remembered: string): string {
	const data = roots.filter((root) => root.kind === 'volume' || root.kind === 'share');
	return (
		data.find((root) => root.id === remembered)?.id ??
		data.find((root) => root.kind === 'share')?.id ??
		data[0]?.id ??
		''
	);
}
