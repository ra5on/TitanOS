/** Only local, known destinations; never treat notification text as a URL. */
export function noticeLink(kind: string, subject: string): string | null {
	if (kind.startsWith('maintenance_')) return '/tools/maintenance';
	if (kind.startsWith('script_')) return '/tools/scripts';
	if (kind.startsWith('volume_')) return `/storage#volume-${encodeURIComponent(subject)}`;
	if (kind.startsWith('disk_test_')) return '/tools/disks';
	if (kind.startsWith('snapshot_restore_')) return '/storage/manage?mode=snapshots';
	if (kind.startsWith('disk_')) return `/storage#disk-${encodeURIComponent(subject)}`;
	if (kind.startsWith('config_backup_')) return '/settings/backups/configuration';
	if (kind.startsWith('backup_'))
		return `/settings/backups/data#backup-${encodeURIComponent(subject)}`;
	if (kind.startsWith('update_')) return '/settings/updates';
	if (kind.startsWith('ups_')) return '/settings/ups';
	if (kind === 'certificate_expiring') return '/settings/access';
	if (kind.startsWith('letsencrypt_')) return '/settings/letsencrypt';
	if (kind.startsWith('ddns_')) return '/settings/ddns';
	// Notifications of the removed IPv64.net account area (PLAN 79-2) that
	// are still in the list lead to dynamic DNS.
	if (kind.startsWith('ipv64_')) return '/settings/ddns';
	if (kind === 'usb_plugged') return '/files';
	if (kind === 'plugin_recommended') return '/plugins';
	return null;
}
