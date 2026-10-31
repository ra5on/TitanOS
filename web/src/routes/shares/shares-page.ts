// What the share page shows about a share, without the page itself.

import type { Share } from '#lib/api.ts';
import { t } from '#lib/i18n/index.svelte.ts';

/** How a share is reached from Windows, macOS, Linux and over NFS, through `host`. */
export function shareAddresses(share: Share, host: string) {
	const list: { label: string; value: string }[] = [];
	if (share.smb)
		list.push(
			{ label: t('shares.windows'), value: `\\\\${host}\\${share.name}` },
			// Finder: Go › Connect to Server.
			{ label: t('shares.mac'), value: `smb://${host}/${encodeURIComponent(share.name)}` },
			// File managers (GNOME, KDE) take the URL, mount.cifs the UNC form.
			{ label: t('shares.linux'), value: `smb://${host}/${encodeURIComponent(share.name)}` },
			{ label: t('shares.linuxMount'), value: `//${host}/${share.name}` },
		);
	if (share.nfs.enabled) list.push({ label: t('shares.nfs'), value: `${host}:/${share.name}` });
	return list;
}

/** "1 account" / "3 accounts" with access to the share. */
export function accessSummary(share: Share) {
	const count = Object.keys(share.access).length;
	return count === 1 ? t('shares.accountOne') : t('shares.accountCount', { count });
}
