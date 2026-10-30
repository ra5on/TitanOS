import type { Share } from './api.ts';

export type ShareView = {
	volume: string;
	protocol: 'all' | 'smb' | 'nfs';
	sort: 'name' | 'volume';
	descending: boolean;
	compact: boolean;
};
export function sanitizeShareView(value: unknown): ShareView {
	const result: ShareView = {
		volume: '',
		protocol: 'all',
		sort: 'name',
		descending: false,
		compact: true,
	};
	if (!value || typeof value !== 'object' || Array.isArray(value)) return result;
	const v = value as Record<string, unknown>;
	if (typeof v.volume === 'string' && v.volume.length <= 128 && !/[\x00-\x1f\x7f]/.test(v.volume))
		result.volume = v.volume;
	if (v.protocol === 'smb' || v.protocol === 'nfs') result.protocol = v.protocol;
	if (v.sort === 'volume') result.sort = v.sort;
	if (typeof v.descending === 'boolean') result.descending = v.descending;
	if (typeof v.compact === 'boolean') result.compact = v.compact;
	return result;
}
export function visibleShares(
	shares: Share[],
	view: ShareView,
	query: string,
	locale: string,
): Share[] {
	const words = query.toLocaleLowerCase(locale).trim().split(/\s+/);
	return shares
		.filter(
			(s) =>
				(!view.volume || s.volume === view.volume) &&
				(view.protocol === 'all' || (view.protocol === 'smb' ? s.smb : s.nfs.enabled)) &&
				words.every((w) => `${s.name} ${s.volume}`.toLocaleLowerCase(locale).includes(w)),
		)
		.sort(
			(a, b) =>
				((view.sort === 'volume'
					? a.volume.localeCompare(b.volume, locale, { numeric: true })
					: 0) || a.name.localeCompare(b.name, locale, { numeric: true })) *
				(view.descending ? -1 : 1),
		);
}
// Navigation state stays in RAM and is replaced when the signed-in account changes.
let navigation = { account: '', query: '', selected: '', scroll: 0 };
export function shareNavigation(account: string) {
	if (navigation.account !== account) navigation = { account, query: '', selected: '', scroll: 0 };
	return navigation;
}
export const shareFilesURL = (name: string) =>
	'/files?' + new URLSearchParams({ root: `share/${name}` });
