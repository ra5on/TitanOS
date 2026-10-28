// Entries of the activity log and how they are named on the page.
export type ActivitySettings = { enabled: boolean; volume: string; days: number; maxMiB: number };
export type ActivityStatus = {
	settings: ActivitySettings;
	volume: string;
	pending: number;
	dropped: number;
	error: string;
};
export type ActivityEntry = {
	at: string;
	user: string;
	via: 'web' | 'token' | 'agent' | 'system' | 'ssh';
	ip?: string;
	action: string;
	target?: string;
	status?: number;
	ok: boolean;
};
export type ActivityPage = {
	entries: ActivityEntry[];
	more: boolean;
	stored: boolean;
	status: ActivityStatus;
};

// Actions with a name of their own; everything else is named by area and method.
const named: Record<string, string> = {
	login: 'login',
	logout: 'logout',
	login_failed: 'loginFailed',
	login_blocked: 'loginBlocked',
	ssh_login: 'sshLogin',
	ssh_login_blocked: 'sshLoginBlocked',
	firewall_block: 'firewallBlock',
	firewall_reverted: 'firewallReverted',
	'PUT /api/v1/firewall': 'firewallChange',
	'POST /api/v1/firewall/confirm': 'firewallConfirm',
	'POST /api/v1/firewall/revert': 'firewallRevert',
	'PUT /api/v1/firewall/autoblock': 'firewallAutoBlock',
	'POST /api/v1/firewall/unblock': 'firewallUnblock',
	login_disabled: 'loginDisabled',
	setup: 'setup',
	'POST /api/v1/files/delete': 'fileDelete',
	'POST /api/v1/files/rename': 'fileRename',
	'POST /api/v1/files/rename-batch': 'fileRename',
	'POST /api/v1/files/copy': 'fileCopy',
	'POST /api/v1/files/mkdir': 'fileMkdir',
	'POST /api/v1/files/create': 'fileCreate',
	'PUT /api/v1/files/text': 'fileEdit',
	'POST /api/v1/files/uploads': 'fileUpload',
	'POST /api/v1/files/url-download': 'fileDownloadLink',
	'POST /api/v1/files/extract': 'fileExtract',
	'POST /api/v1/files/trash/{action}': 'fileTrash',
	'POST /api/v1/files/undo/{id}': 'fileUndo',
	'POST /api/v1/users': 'userCreate',
	'PUT /api/v1/users/{name}': 'userChange',
	'DELETE /api/v1/users/{name}': 'userDelete',
	'POST /api/v1/users/{name}/lock': 'userLock',
	'POST /api/v1/users/{name}/unlock': 'userUnlock',
	'POST /api/v1/users/{name}/password': 'userPassword',
	'POST /api/v1/shares': 'shareCreate',
	'PUT /api/v1/shares/{name}': 'shareChange',
	'DELETE /api/v1/shares/{name}': 'shareDelete',
	'POST /api/v1/shares/{name}/delete-data': 'shareDeleteData',
	share_data_deleted: 'shareDataDeleted',
	share_data_delete_failed: 'shareDataDeleteFailed',
	'POST /api/v1/storage/volumes': 'volumeCreate',
	'DELETE /api/v1/storage/volumes/{name}': 'volumeDelete',
	'POST /api/v1/storage/volumes/{name}/snapshots/{snapshot}/folders/restore': 'snapshotFolderRestore',
	'GET /api/v1/terminal/shell': 'consoleShell',
	'GET /api/v1/docker/containers/{id}/console': 'consoleContainer',
	'GET /api/v1/vms/{name}/serial': 'consoleSerial',
	'GET /api/v1/vms/{name}/screen': 'consoleScreen',
	'GET /api/v1/lxc/{name}/console': 'consoleLxc',
	'GET /api/v1/config-backup/download': 'configDownload',
	'GET /api/v1/diagnostics/bundle': 'diagnostics',
	'POST /api/v1/system/power': 'power',
	'POST /api/v1/update/{action}': 'update',
	'POST /api/v1/vms/{name}/{action}': 'vmAction',
	'POST /api/v1/docker/containers/{id}/{action}': 'containerAction',
	'POST /api/v1/account/2fa/enable': 'twoFactorOn',
	'POST /api/v1/account/2fa/disable': 'twoFactorOff',
	'POST /api/v1/account/tokens': 'tokenCreate',
	'DELETE /api/v1/account/tokens/{id}': 'tokenDelete',
	'PUT /api/v1/activity': 'activitySettings',
	vm_schedule_start: 'vmScheduleStart',
	vm_schedule_stop: 'vmScheduleStop',
	vm_schedule_poweroff: 'vmSchedulePoweroff',
	letsencrypt_issued: 'letsencryptIssued',
	letsencrypt_failed: 'letsencryptFailed',
	ddns_updated: 'ddnsUpdated',
	ddns_failed: 'ddnsFailed',
};

const areas: Record<string, string> = {
	account: 'account',
	session: 'account',
	users: 'users',
	shares: 'shares',
	files: 'files',
	storage: 'storage',
	docker: 'apps',
	apps: 'apps',
	vms: 'vms',
	'vm-clones': 'vms',
	lxc: 'lxc',
	plugins: 'plugins',
	network: 'network',
	firewall: 'network',
	system: 'system',
	backups: 'backups',
	'config-backup': 'backups',
	update: 'updates',
	assistant: 'assistant',
	homeassistant: 'homeassistant',
	terminal: 'console',
	activity: 'activity',
	notifications: 'notifications',
	maintenance: 'maintenance',
	monitoring: 'monitoring',
	ups: 'ups',
	usb: 'usb',
	hardware: 'hardware',
	diagnostics: 'system',
};

export type ActivityLabel =
	| { kind: 'named'; key: string }
	| { kind: 'generic'; area: string; verb: 'run' | 'change' | 'remove' | 'open' };

export function activityLabel(action: string): ActivityLabel {
	const key = named[action];
	if (key) return { kind: 'named', key };
	const [method = '', path = ''] = action.split(' ', 2);
	const segment = /^\/api\/v1\/([^/]+)/.exec(path)?.[1] ?? '';
	const area = areas[segment] ?? 'other';
	const verb =
		method === 'DELETE'
			? 'remove'
			: method === 'PUT' || method === 'PATCH'
				? 'change'
				: method === 'GET'
					? 'open'
					: 'run';
	return { kind: 'generic', area, verb };
}

// activityPeople lists the accounts in the entries for the filter.
export function activityPeople(entries: ActivityEntry[]): string[] {
	return [...new Set(entries.map((e) => e.user).filter(Boolean))].sort((a, b) =>
		a.localeCompare(b),
	);
}
