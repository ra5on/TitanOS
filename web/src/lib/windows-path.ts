import type { FileRoot, Share } from './api.ts';

/** Only existing, accessible SMB shares can be represented as Windows paths. */
export function windowsFolder(
	root: FileRoot,
	dir: string,
	shares: Share[],
	host: string,
): string | null {
	if (!/^[a-zA-Z0-9][a-zA-Z0-9.-]*$/.test(host)) return null;
	const share = shares.find(
		(s) =>
			s.smb &&
			s.available &&
			(root.kind === 'share'
				? root.name === s.name
				: root.kind === 'volume' &&
					root.name === s.volume &&
					(dir === s.name || dir.startsWith(s.name + '/'))),
	);
	if (!share) return null;
	const relative = root.kind === 'share' ? dir : dir.slice(share.name.length).replace(/^\//, '');
	const segments = [share.name, ...relative.split('/').filter(Boolean)];
	if (
		segments.some(
			(s) =>
				/[<>:"\\|?*\x00-\x1f]/.test(s) ||
				/[. ]$/.test(s) ||
				/^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)/i.test(s),
		)
	)
		return null;
	return '\\\\' + host + '\\' + segments.join('\\');
}
const quote = (value: string) => "'" + value.replaceAll("'", "''") + "'";
export function windowsMapping(target: string, drive: string): string {
	if (!/^[D-Z]$/i.test(drive) || /[\x00-\x1f]/.test(target)) return '';
	return `New-SmbMapping -LocalPath '${drive.toUpperCase()}:' -RemotePath ${quote(target)} -Persistent $false`;
}
export function windowsCopy(source: string, target: string): string {
	if (
		!/^(?:[a-zA-Z]:\\|\\\\[^\\]+\\[^\\]+(?:\\|$))/.test(source) ||
		/[\x00-\x1f]/.test(source + target)
	)
		return '';
	return `& robocopy ${quote(source)} ${quote(target)} /E /Z /R:2 /W:2 /XJ`;
}
