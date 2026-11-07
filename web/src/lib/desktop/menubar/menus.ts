// PLAN 67-1: the menu bar of every program (top bar variant B, like macOS).
// Each program describes its menus here; Fenster and Hilfe are the same for
// all. Pure data: labels are message keys, nothing is translated here, so
// tests and the frame side can read it.
//
// Kinds of entries:
// - go:   a page; of the same program it opens in the window, of another
//         program that program opens ("{app}" = the Docker app of the window)
// - win:  an operation on the window in front
// - desk: an operation on the desktop
// - sys:  the logo menu and the help (dialogs, lock, logout, documentation)
// - cmd:  a command of the page in the window; it registers its handlers with
//         lib/menu-commands.svelte.ts. Without a handler the entry is disabled.
import type { Role } from '../../api.ts';
import type { MessageKey } from '../../i18n/index.svelte.ts';
import { appAllowed, appForPath, SYSTEM_APPS } from '../apps.ts';

export type WinOp =
	| 'minimize'
	| 'maximize'
	| 'snapLeft'
	| 'snapRight'
	| 'allOfProgram'
	| 'showDesktop'
	| 'openTab'
	| 'close';
export type DeskOp =
	| 'newFolder'
	| 'wallpaper'
	| 'widgets'
	| 'dock'
	| 'autoArrange'
	| 'sort:name'
	| 'sort:kind'
	| 'sort:recent'
	| 'sort:free'
	| 'size:s'
	| 'size:m'
	| 'size:l';
export type SysOp =
	| 'about'
	| 'settings'
	| 'appcenter'
	| 'updates'
	| 'lock'
	| 'logout'
	| 'restart'
	| 'shutdown'
	| 'help'
	| 'shortcuts'
	| 'diagnose';

export type MenuAction =
	| { go: string }
	| { win: WinOp }
	| { desk: DeskOp }
	| { sys: SysOp }
	| { cmd: string; arg?: string };

export type MenuEntry = {
	/** Unique within the menus of a program. */
	id: string;
	label: MessageKey;
	do?: MenuAction;
	/** A submenu (opens to the right). */
	sub?: MenuItem[];
	/** Shortcut as shown, e.g. "Alt+M", "Ctrl+A" (Ctrl/Del/Space are translated). Only keys that work. */
	keys?: string;
	/** Only these roles see the entry. */
	roles?: Role[];
	/** A check mark (on/off) or a choice among several (radio). */
	mark?: 'check' | 'radio';
	/** A submenu whose entries the page supplies (shares, recent files, keys …). */
	list?: boolean;
	/** Text of an entry the page supplied (list submenus); shown as plain text. */
	text?: string;
	/** Whether a supplied list entry is the current one. */
	picked?: boolean;
};
export type MenuSeparator = { sep: true };
export type MenuItem = MenuEntry | MenuSeparator;
export type Menu = { id: string; label: MessageKey; items: MenuItem[] };

const ADMIN: Role[] = ['owner', 'admin'];
const sep: MenuSeparator = { sep: true };
const cmd = (id: string, label: MessageKey, more: Partial<MenuEntry> = {}): MenuEntry => ({
	id,
	label,
	do: { cmd: id },
	...more,
});
const go = (id: string, label: MessageKey, href: string, more: Partial<MenuEntry> = {}) =>
	({ id, label, do: { go: href }, ...more }) satisfies MenuEntry;
const sub = (id: string, label: MessageKey, items: MenuItem[]): MenuEntry => ({
	id,
	label,
	sub: items,
});
const radio = (id: string, label: MessageKey, more: Partial<MenuEntry> = {}) =>
	cmd(id, label, { mark: 'radio', ...more });
const check = (id: string, label: MessageKey, more: Partial<MenuEntry> = {}) =>
	cmd(id, label, { mark: 'check', ...more });
const menu = (id: string, label: MessageKey, items: MenuItem[]): Menu => ({ id, label, items });

// ---- The programs ----
// Only what the program's pages really offer (PLAN 67-1: "keep the menus honest").

const list = (id: string, label: MessageKey, more: Partial<MenuEntry> = {}) =>
	cmd(id, label, { list: true, ...more });
const radios = (prefix: string, pairs: [string, MessageKey][]) =>
	pairs.map(([id, label]) => radio(`${prefix}.${id}`, label));

const PROGRAMS: Record<string, Menu[]> = {
	files: [
		menu('file', 'menubar.menu.file', [
			cmd('files.mkdir', 'menubar.files.newFolder'),
			cmd('files.newFile', 'menubar.files.newFile'),
			sub('files.uploadMenu', 'menubar.files.upload', [
				cmd('files.upload', 'menubar.files.uploadFiles'),
				cmd('files.uploadFolder', 'menubar.files.uploadFolder'),
			]),
			cmd('files.download', 'menubar.files.download'),
			cmd('files.zip', 'menubar.files.zip'),
			list('files.openWith', 'menubar.files.openWith'),
			cmd('files.preview', 'menubar.files.preview'),
			cmd('files.properties', 'menubar.files.properties'),
			sep,
			cmd('files.rename', 'menubar.files.rename', { keys: 'F2' }),
			cmd('files.delete', 'menubar.files.delete', { keys: 'Del' }),
			cmd('files.openTrash', 'menubar.files.openTrash', { do: { cmd: 'files.trash' } }),
		]),
		menu('edit', 'menubar.menu.edit', [
			cmd('files.undo', 'menubar.edit.undo'),
			sep,
			cmd('files.cut', 'menubar.edit.cut', { keys: 'Ctrl+X' }),
			cmd('files.copy', 'menubar.edit.copy', { keys: 'Ctrl+C' }),
			cmd('files.paste', 'menubar.edit.paste', { keys: 'Ctrl+V' }),
			cmd('files.selectAll', 'menubar.edit.selectAll', { keys: 'Ctrl+A' }),
			sep,
			cmd('files.search', 'menubar.edit.find'),
		]),
		menu('view', 'menubar.menu.view', [
			radio('files.view.list', 'files.view.list'),
			radio('files.view.small', 'files.view.small'),
			radio('files.view.large', 'files.view.large'),
			sep,
			sub('files.sortMenu', 'menubar.files.sortBy', [
				radio('files.sort.name', 'menubar.files.sortName'),
				radio('files.sort.size', 'menubar.files.sortSize'),
				radio('files.sort.modified', 'menubar.files.sortModified'),
			]),
			check('files.hidden', 'menubar.files.hidden'),
			check('files.twoPanes', 'menubar.files.twoPanes'),
			check('files.folderView', 'menubar.files.folderView'),
		]),
		menu('go', 'menubar.menu.go', [
			cmd('files.back', 'menubar.files.back'),
			cmd('files.forward', 'menubar.files.forward'),
			cmd('files.up', 'menubar.files.up'),
			sep,
			cmd('files.goto', 'menubar.files.goto'),
			list('files.shares', 'menubar.files.shares'),
			list('files.recent', 'menubar.files.recent'),
			cmd('files.favorites', 'menubar.files.favorites'),
			cmd('files.trash', 'menubar.files.trash'),
		]),
	],
	shares: [
		menu('file', 'menubar.menu.file', [
			cmd('shares.new', 'menubar.shares.new'),
			cmd('shares.edit', 'menubar.shares.edit'),
			cmd('shares.rights', 'menubar.shares.rights'),
			cmd('shares.connect', 'menubar.shares.connect'),
			sep,
			cmd('shares.delete', 'menubar.shares.delete'),
		]),
		menu('view', 'menubar.menu.view', [
			radio('shares.sort.name', 'menubar.shares.byName'),
			radio('shares.sort.volume', 'menubar.shares.byVolume'),
			sep,
			...radios('shares.proto', [
				['all', 'menubar.shares.protoAll'],
				['smb', 'menubar.shares.smb'],
				['nfs', 'menubar.shares.nfs'],
			]),
		]),
		menu('services', 'menubar.menu.services', [
			go('shares.go.services', 'desktop.side.services', '/settings/services'),
		]),
	],
	storage: [
		menu('file', 'menubar.menu.file', [
			cmd('storage.newVolume', 'menubar.storage.newVolume'),
			cmd('storage.addDisk', 'menubar.storage.addDisk'),
			go('storage.go.replace', 'desktop.side.replace', '/storage/replace'),
			go('storage.go.import', 'desktop.side.import', '/storage/import'),
		]),
		menu('tools', 'menubar.menu.tools', [
			check('storage.scrub', 'menubar.storage.scrub'),
			go('storage.go.smart', 'menubar.storage.smart', '/storage/smart'),
			cmd('storage.sleep', 'menubar.storage.sleep'),
			cmd('storage.rescan', 'menubar.storage.rescan'),
			go('storage.go.diskTools', 'desktop.side.diskTools', '/tools/disks'),
		]),
		menu('view', 'menubar.menu.view', [
			radio('storage.tab.volumes', 'menubar.storage.volumes'),
			radio('storage.tab.disks', 'menubar.storage.disks'),
			sep,
			go('storage.go.manage', 'desktop.side.manage', '/storage/manage'),
			go('storage.go.memory', 'desktop.side.memory', '/settings/memory'),
		]),
	],
	docker: [
		menu('file', 'menubar.menu.file', [
			go('docker.go.install', 'menubar.docker.install', '/apps'),
			cmd('docker.import', 'menubar.docker.compose'),
			sep,
			cmd('docker.startAll', 'menubar.docker.startAll'),
			cmd('docker.stopAll', 'menubar.docker.stopAll'),
			sep,
			cmd('docker.checkUpdates', 'menubar.docker.checkUpdates'),
			cmd('docker.updateAll', 'menubar.docker.updateAll'),
		]),
		menu('view', 'menubar.menu.view', [
			radio('docker.filter.all', 'docker.filterAll'),
			radio('docker.filter.running', 'menubar.docker.filterRunning'),
			radio('docker.filter.stopped', 'menubar.docker.filterStopped'),
			radio('docker.filter.updates', 'menubar.docker.filterUpdates'),
		]),
		menu('tools', 'menubar.menu.tools', [
			go('docker.go.containers', 'desktop.side.containers', '/docker'),
			go('docker.go.images', 'desktop.side.images', '/docker/images'),
			go('docker.go.volumes', 'desktop.side.volumes', '/docker/volumes'),
			go('docker.go.networks', 'desktop.side.networks', '/docker/networks'),
			go('docker.go.cleanup', 'desktop.side.dockerCleanup', '/docker/cleanup'),
			sep,
			go('docker.go.https', 'desktop.side.httpsNames', '/settings/proxy'),
		]),
	],
	vms: [
		menu('machine', 'menubar.menu.machine', [
			go('vms.go.new', 'menubar.vms.new', '/vms/new'),
			go('vms.go.adopt', 'desktop.side.adoptVm', '/vms?adopt=1'),
			sep,
			cmd('vms.start', 'menubar.vms.start'),
			cmd('vms.shutdown', 'menubar.vms.shutdown'),
			cmd('vms.reboot', 'menubar.vms.reboot'),
			cmd('vms.poweroff', 'menubar.vms.poweroff'),
			sep,
			cmd('vms.edit', 'menubar.vms.edit'),
			cmd('vms.clone', 'menubar.vms.clone'),
			cmd('vms.remove', 'menubar.vms.remove'),
		]),
		menu('view', 'menubar.menu.view', [
			radio('vms.tab.overview', 'menubar.vms.overview'),
			radio('vms.tab.screen', 'menubar.vms.screen'),
			radio('vms.tab.hardware', 'menubar.vms.hardware'),
			radio('vms.tab.snapshots', 'menubar.vms.snapshots'),
			radio('vms.tab.schedule', 'menubar.vms.schedule'),
		]),
		menu('screen', 'menubar.menu.screen', [
			cmd('vms.cad', 'menubar.vms.cad'),
			list('vms.keys', 'menubar.vms.keys'),
			sub('vms.modeMenu', 'menubar.vms.resolution', [
				radio('vms.mode.scale', 'vms.mode.scale'),
				radio('vms.mode.remote', 'vms.mode.remote'),
				radio('vms.mode.full', 'vms.mode.full'),
			]),
			cmd('vms.fullscreen', 'menubar.vms.fullscreen'),
			cmd('vms.screenshot', 'menubar.vms.screenshot'),
			cmd('vms.paste', 'menubar.vms.paste'),
			sep,
			cmd('vms.serial', 'menubar.vms.serial'),
		]),
		menu('media', 'menubar.menu.media', [
			cmd('vms.isoInsert', 'menubar.vms.isoInsert'),
			cmd('vms.isoEject', 'menubar.vms.isoEject'),
			sep,
			go('vms.go.media', 'desktop.side.media', '/vms/media'),
		]),
	],
	lxc: [
		menu('container', 'menubar.menu.container', [
			cmd('lxc.new', 'menubar.lxc.new'),
			sep,
			cmd('lxc.start', 'menubar.vms.start'),
			cmd('lxc.stop', 'menubar.lxc.stop'),
			cmd('lxc.restart', 'menubar.vms.reboot'),
			cmd('lxc.kill', 'menubar.lxc.kill'),
			sep,
			cmd('lxc.shell', 'menubar.lxc.shell'),
			cmd('lxc.console', 'menubar.lxc.console'),
			cmd('lxc.edit', 'menubar.lxc.edit'),
			cmd('lxc.snapshot', 'menubar.lxc.snapshot'),
			sep,
			cmd('lxc.remove', 'menubar.vms.remove'),
		]),
		menu('view', 'menubar.menu.view', [
			list('lxc.show', 'menubar.lxc.show'),
			cmd('lxc.refresh', 'menubar.refresh'),
		]),
	],
	appcenter: [
		menu('file', 'menubar.menu.file', [
			go('appcenter.go.install', 'menubar.docker.install', '/apps'),
			go('appcenter.go.own', 'desktop.side.ownApp', '/apps/new'),
			cmd('appcenter.fromLink', 'menubar.appcenter.fromLink'),
			cmd('appcenter.sources', 'menubar.appcenter.sources'),
			cmd('appcenter.refresh', 'menubar.appcenter.refresh'),
		]),
		menu('view', 'menubar.menu.view', [
			radio('appcenter.filter.all', 'menubar.appcenter.all'),
			radio('appcenter.filter.installed', 'menubar.appcenter.installed'),
			cmd('appcenter.webapps', 'menubar.appcenter.webapps'),
			sep,
			list('appcenter.categories', 'menubar.appcenter.categories'),
			list('appcenter.source', 'menubar.appcenter.source'),
		]),
		menu('tools', 'menubar.menu.tools', [
			go('appcenter.go.plugins', 'desktop.side.plugins', '/plugins'),
			go('appcenter.go.cleanup', 'desktop.side.cleanup', '/plugins/cleanup'),
		]),
	],
	backup: [
		menu('file', 'menubar.menu.file', [
			cmd('backup.new', 'menubar.backup.new'),
			list('backup.now', 'menubar.backup.now'),
			cmd('backup.stickNow', 'menubar.backup.stickNow'),
			go('backup.go.restore', 'menubar.backup.restore', '/settings/backups/restore'),
		]),
		menu('view', 'menubar.menu.view', [
			go('backup.go.overview', 'desktop.side.overview', '/settings/backups'),
			go('backup.go.data', 'desktop.side.data', '/settings/backups/data'),
			go('backup.go.apps', 'desktop.side.applications', '/settings/backups/applications'),
			go('backup.go.config', 'menubar.backup.stick', '/settings/backups/configuration'),
			go('backup.go.coverage', 'desktop.side.coverage', '/settings/backups/coverage'),
			sep,
			cmd('backup.reload', 'menubar.refresh'),
		]),
	],
	agent: [
		menu('file', 'menubar.menu.file', [
			cmd('agent.newChat', 'menubar.agent.newChat'),
			cmd('agent.project', 'menubar.agent.project'),
			list('agent.projects', 'menubar.agent.inProject'),
		]),
		menu('view', 'menubar.menu.view', [
			cmd('agent.notes', 'menubar.agent.notes'),
			cmd('agent.knowledge', 'menubar.agent.knowledge'),
			cmd('agent.inbox', 'menubar.agent.inbox'),
		]),
		menu('agent', 'menubar.menu.agent', [
			list('agent.model', 'menubar.agent.model'),
			cmd('agent.settings', 'menubar.agent.settings'),
		]),
	],
	monitor: [
		menu('view', 'menubar.menu.view', [
			...radios('monitor.tab', [
				['overview', 'taskmanager.tab.overview'],
				['processes', 'taskmanager.tab.processes'],
				['containers', 'taskmanager.tab.containers'],
				['guests', 'taskmanager.tab.guests'],
				['network', 'taskmanager.tab.network'],
				['disks', 'taskmanager.tab.disks'],
			]),
			sep,
			go('monitor.go.history', 'desktop.side.history', '/tools/monitoring'),
			go('monitor.go.tasks', 'desktop.side.tasks', '/tools/tasks'),
			go('monitor.go.check', 'desktop.side.hardwareCheck', '/tools/hardware'),
			go('monitor.go.hardware', 'desktop.side.hardware', '/settings/hardware'),
			go('monitor.go.temps', 'desktop.side.temperatures', '/settings/temperatures'),
		]),
		menu('process', 'menubar.menu.sort', [
			...radios('monitor.sort', [
				['cpu', 'menubar.monitor.byCpu'],
				['memory', 'menubar.monitor.byMemory'],
				['name', 'menubar.monitor.byName'],
			]),
		]),
		menu('tools', 'menubar.menu.tools', [
			cmd('monitor.refresh', 'menubar.monitor.refresh'),
			cmd('monitor.check', 'menubar.monitor.check'),
			sep,
			cmd('monitor.export', 'menubar.monitor.export'),
			cmd('monitor.exportJson', 'menubar.exportJson'),
		]),
	],
	logs: [
		menu('file', 'menubar.menu.file', [
			cmd('logs.export', 'menubar.logs.export'),
			cmd('logs.exportJson', 'menubar.exportJson'),
			cmd('logs.exportPrevious', 'menubar.logs.exportPrevious'),
			sep,
			cmd('logs.pack', 'menubar.logs.pack'),
			go('logs.go.bundle', 'desktop.side.bundle', '/tools/bundle'),
		]),
		menu('edit', 'menubar.menu.edit', [
			cmd('logs.find', 'menubar.edit.find'),
			cmd('logs.copy', 'menubar.edit.copy'),
		]),
		menu('view', 'menubar.menu.view', [
			go('logs.go.system', 'desktop.side.logs', '/tools/logs'),
			go('logs.go.activity', 'desktop.side.activity', '/tools/activity'),
			sep,
			list('logs.source', 'menubar.logs.source'),
			list('logs.level', 'menubar.logs.level'),
			list('logs.range', 'menubar.logs.range'),
			check('logs.follow', 'menubar.logs.follow'),
		]),
	],
	planner: [
		menu('file', 'menubar.menu.file', [
			cmd('planner.newScript', 'menubar.planner.newScript'),
			cmd('planner.newTask', 'menubar.planner.newTask'),
			list('planner.run', 'menubar.planner.run'),
		]),
		menu('view', 'menubar.menu.view', [
			go('planner.go.scripts', 'desktop.side.scripts', '/tools/scripts'),
			go('planner.go.maintenance', 'desktop.side.maintenance', '/tools/maintenance'),
			go('planner.go.energy', 'desktop.side.energySchedule', '/settings/energy'),
			sep,
			list('planner.history', 'desktop.side.history'),
			cmd('planner.refresh', 'menubar.refresh'),
		]),
	],
	terminal: [
		menu('shell', 'menubar.menu.shell', [
			cmd('terminal.newTab', 'menubar.terminal.newTab'),
			cmd('terminal.consoleOf', 'menubar.terminal.consoleOf'),
			cmd('terminal.closeTab', 'menubar.terminal.closeTab'),
		]),
		menu('edit', 'menubar.menu.edit', [
			cmd('terminal.copy', 'menubar.edit.copy'),
			cmd('terminal.paste', 'menubar.edit.paste'),
			cmd('terminal.selectAll', 'menubar.edit.selectAll'),
			cmd('terminal.find', 'menubar.edit.find'),
			sep,
			cmd('terminal.clear', 'menubar.terminal.clear'),
		]),
		menu('view', 'menubar.menu.view', [
			cmd('terminal.fontUp', 'terminal.fontLarger'),
			cmd('terminal.fontDown', 'terminal.fontSmaller'),
			cmd('terminal.fullscreen', 'menubar.vms.fullscreen'),
		]),
	],
	users: [
		menu('file', 'menubar.menu.file', [
			cmd('users.new', 'menubar.users.new'),
			list('users.password', 'menubar.users.password'),
			list('users.lock', 'menubar.users.lock'),
			list('users.reset2fa', 'menubar.users.reset2fa'),
			sep,
			list('users.delete', 'menubar.users.delete'),
		]),
		menu('view', 'menubar.menu.view', [
			...radios('users.filter', [
				['all', 'menubar.appcenter.all'],
				['active', 'menubar.users.active'],
				['locked', 'menubar.users.locked'],
			]),
			cmd('users.refresh', 'menubar.refresh'),
			sep,
			go('users.go.accounts', 'desktop.side.accounts', '/users'),
			go('users.go.programs', 'desktop.side.programs', '/settings/programs'),
			go('users.go.access', 'desktop.side.access', '/settings/access'),
		]),
	],
	security: [
		menu('file', 'menubar.menu.file', [
			cmd('security.addZone', 'menubar.security.addZone'),
			check('security.firewall', 'menubar.security.firewall'),
			sep,
			cmd('security.apply', 'menubar.security.apply'),
			cmd('security.discard', 'menubar.security.discard'),
		]),
		menu('view', 'menubar.menu.view', [
			...radios('security.tab', [
				['zones', 'firewall.tab.zones'],
				['networks', 'firewall.tab.networks'],
				['policies', 'firewall.tab.policies'],
				['autoblock', 'firewall.tab.autoblock'],
				['recent', 'firewall.tab.recent'],
			]),
		]),
	],
	account: [
		menu('account', 'menubar.menu.account', [
			cmd('account.password', 'menubar.account.password'),
			cmd('account.twoFactorOn', 'menubar.account.twoFactorOn'),
			cmd('account.twoFactorOff', 'menubar.account.twoFactorOff'),
			cmd('account.recoveryCodes', 'menubar.account.recoveryCodes'),
			sep,
			cmd('account.passkeys', 'menubar.account.passkeys'),
			cmd('account.ssh', 'menubar.account.ssh'),
			cmd('account.webdav', 'menubar.account.webdav'),
			cmd('account.devices', 'menubar.account.devices'),
			sep,
			cmd('account.logoutAll', 'menubar.account.logoutAll'),
		]),
	],
	settings: [
		menu('file', 'menubar.menu.file', [
			cmd('settings.search', 'menubar.settings.search'),
			go('settings.go.config', 'menubar.settings.config', '/settings/backups/configuration', {
				roles: ADMIN,
			}),
		]),
		menu('view', 'menubar.menu.view', [
			go('settings.go.all', 'desktop.side.allSettings', '/settings'),
			sub('settings.groups', 'menubar.settings.groups', [
				go('settings.go.system', 'desktop.side.system', '/settings/about'),
				go('settings.go.network', 'desktop.side.network', '/settings/network', { roles: ADMIN }),
				go('settings.go.accounts', 'desktop.side.accountsServices', '/settings/access', {
					roles: ADMIN,
				}),
				go('settings.go.data', 'desktop.side.dataGroup', '/settings/files'),
			]),
		]),
	],
	// PLAN 66: web apps running in the browser (WordCraft …), handled by the window itself.
	webapp: [
		menu('file', 'menubar.menu.file', [
			cmd('webapp.new', 'menubar.webapp.new'),
			cmd('webapp.open', 'webapps.menu.open'),
			sep,
			cmd('webapp.save', 'menubar.webapp.save', { keys: 'Ctrl+S' }),
			cmd('webapp.saveAs', 'webapps.menu.saveAs'),
		]),
		menu('edit', 'menubar.menu.edit', [
			cmd('webapp.undo', 'menubar.edit.undo', { keys: 'Ctrl+Z' }),
			cmd('webapp.redo', 'menubar.edit.redo', { keys: 'Ctrl+Y' }),
			sep,
			cmd('webapp.selectAll', 'menubar.edit.selectAll', { keys: 'Ctrl+A' }),
			cmd('webapp.find', 'menubar.edit.find', { keys: 'Ctrl+F' }),
		]),
		menu('app', 'menubar.menu.app', [
			cmd('webapp.reload', 'menubar.webapp.reload'),
			go('webapp.go.default', 'menubar.webapp.defaultApp', '/settings/files'),
		]),
	],
	// PLAN 63/66: a Docker app in the remote view, handled by the window itself.
	remote: [
		menu('file', 'menubar.menu.file', [
			cmd('remote.upload', 'menubar.remote.upload'),
			list('remote.downloads', 'viewer.downloads'),
		]),
		menu('edit', 'menubar.menu.edit', [
			cmd('remote.clipSend', 'menubar.remote.clipSend'),
			cmd('remote.clipGet', 'menubar.remote.clipGet'),
		]),
		menu('view', 'menubar.menu.view', [
			sub('remote.qualityMenu', 'viewer.quality', [
				radio('remote.quality.auto', 'viewer.q.auto'),
				radio('remote.quality.high', 'viewer.q.high'),
				radio('remote.quality.balanced', 'viewer.q.balanced'),
				radio('remote.quality.low', 'viewer.q.low'),
			]),
			check('remote.mute', 'menubar.remote.mute'),
			cmd('remote.fullscreen', 'menubar.vms.fullscreen'),
			check('remote.stats', 'menubar.remote.stats'),
		]),
		menu('app', 'menubar.menu.app', [
			cmd('remote.reconnect', 'viewer.reconnect'),
			cmd('remote.restart', 'menubar.remote.restart', { roles: ADMIN }),
			go('remote.go.docker', 'desktop.ctx.showInDocker', '/docker/apps/{app}', { roles: ADMIN }),
		]),
	],
	// The screen of one VM in a window of its own.
	vmscreen: [
		menu('screen', 'menubar.menu.screen', [
			cmd('vms.cad', 'menubar.vms.cad'),
			list('vms.keys', 'menubar.vms.keys'),
			sub('vms.modeMenu', 'menubar.vms.resolution', [
				radio('vms.mode.scale', 'vms.mode.scale'),
				radio('vms.mode.remote', 'vms.mode.remote'),
				radio('vms.mode.full', 'vms.mode.full'),
			]),
			cmd('vms.screenshot', 'menubar.vms.screenshot'),
		]),
	],
};

/** Documentation shown by "Hilfe zu <Programm>" (files in docs/ of the repository). */
const DOCS: Record<string, string> = {
	files: 'FILE-COPY.md',
	shares: 'SHARE-PROTECTION.md',
	storage: 'BTRFS.md',
	docker: 'APP-LINKS.md',
	vms: 'VM-MEDIA.md',
	vmscreen: 'VM-MEDIA.md',
	lxc: 'LXC.md',
	appcenter: 'PLUGINS.md',
	backup: 'REPLICATION.md',
	agent: 'AGENT.md',
	monitor: 'MONITORING.md',
	logs: 'ACTIVITY.md',
	planner: 'SCRIPTS.md',
	security: 'FIREWALL.md',
	account: 'PASSKEYS.md',
	webapp: 'WEBAPPS.md',
	remote: 'FERNANSICHT.md',
};
export const DOCS_BASE = 'https://github.com/ra5on/TitanOS/blob/main/docs/';

/** The program of a window ("app:files", "docker:immich", "webapp:wordcraft", "vm:win11"). */
export function programOf(key: string): string {
	if (key.startsWith('app:')) return key.slice(4);
	if (key.startsWith('docker:')) return 'remote';
	if (key.startsWith('webapp:')) return 'webapp';
	if (key.startsWith('vm:')) return 'vmscreen';
	return '';
}

export function helpUrl(key: string): string {
	return DOCS_BASE + (DOCS[programOf(key)] ?? 'DESKTOP.md');
}

/** The menu "Fenster", the same for every program. */
export const WINDOW_MENU: Menu = menu('window', 'menubar.menu.window', [
	{ id: 'win.minimize', label: 'desktop.window.minimize', do: { win: 'minimize' }, keys: 'Alt+M' },
	{
		id: 'win.maximize',
		label: 'desktop.window.maximize',
		do: { win: 'maximize' },
		keys: 'Alt+↑',
		mark: 'check',
	},
	{ id: 'win.left', label: 'menubar.window.left', do: { win: 'snapLeft' }, keys: 'Alt+←' },
	{ id: 'win.right', label: 'menubar.window.right', do: { win: 'snapRight' }, keys: 'Alt+→' },
	sep,
	{ id: 'win.all', label: 'menubar.window.all', do: { win: 'allOfProgram' } },
	{
		id: 'win.desktop',
		label: 'desktop.bar.showDesktop',
		do: { win: 'showDesktop' },
		keys: 'Alt+K',
	},
	sep,
	{ id: 'win.tab', label: 'desktop.window.openTab', do: { win: 'openTab' } },
	sep,
	{ id: 'win.close', label: 'desktop.window.close', do: { win: 'close' }, keys: 'Alt+W' },
]);

/** The menu "Hilfe", the same for every program. */
export const HELP_MENU: Menu = menu('help', 'menubar.menu.help', [
	{ id: 'help.docs', label: 'menubar.help.program', do: { sys: 'help' } },
	{ id: 'help.shortcuts', label: 'menubar.help.shortcuts', do: { sys: 'shortcuts' } },
	{ id: 'help.diagnose', label: 'menubar.help.diagnose', do: { sys: 'diagnose' }, roles: ADMIN },
]);

/** The TitanOS logo menu (always there). */
export const LOGO_MENU: Menu = menu('logo', 'menubar.logo', [
	{ id: 'sys.about', label: 'desktop.side.about', do: { sys: 'about' } },
	sep,
	{ id: 'sys.settings', label: 'desktop.app.settings', do: { sys: 'settings' } },
	{ id: 'sys.appcenter', label: 'desktop.app.appcenter', do: { sys: 'appcenter' }, roles: ADMIN },
	{ id: 'sys.updates', label: 'desktop.side.updates', do: { sys: 'updates' }, roles: ADMIN },
	sep,
	{
		id: 'sys.desktop',
		label: 'desktop.bar.showDesktop',
		do: { win: 'showDesktop' },
		keys: 'Alt+K',
	},
	sep,
	{ id: 'sys.lock', label: 'desktop.menu.lock', do: { sys: 'lock' } },
	{ id: 'sys.logout', label: 'desktop.menu.logout', do: { sys: 'logout' } },
	sep,
	{ id: 'sys.restart', label: 'menubar.sys.restart', do: { sys: 'restart' }, roles: ADMIN },
	{ id: 'sys.shutdown', label: 'menubar.sys.shutdown', do: { sys: 'shutdown' }, roles: ADMIN },
]);

/** The desktop's own menus when no window is in front. */
export const DESKTOP_MENUS: Menu[] = [
	menu('file', 'menubar.menu.file', [
		{ id: 'desk.newFolder', label: 'desktop.ctx.newFolder', do: { desk: 'newFolder' } },
		{ id: 'desk.wallpaper', label: 'desktop.ctx.wallpaper', do: { desk: 'wallpaper' } },
	]),
	menu('view', 'menubar.menu.view', [
		sub('desk.sort', 'desktop.ctx.sort', [
			{
				id: 'desk.sort.name',
				label: 'desktop.sort.name',
				do: { desk: 'sort:name' },
				mark: 'radio',
			},
			{
				id: 'desk.sort.kind',
				label: 'desktop.sort.kind',
				do: { desk: 'sort:kind' },
				mark: 'radio',
			},
			{
				id: 'desk.sort.recent',
				label: 'desktop.sort.recent',
				do: { desk: 'sort:recent' },
				mark: 'radio',
			},
			{
				id: 'desk.sort.free',
				label: 'desktop.sort.free',
				do: { desk: 'sort:free' },
				mark: 'radio',
			},
			sep,
			{ id: 'desk.arrange', label: 'desktop.ctx.autoArrange', do: { desk: 'autoArrange' } },
		]),
		sub('desk.size', 'desktop.ctx.iconSize', [
			{ id: 'desk.size.s', label: 'desktop.size.s', do: { desk: 'size:s' }, mark: 'radio' },
			{ id: 'desk.size.m', label: 'desktop.size.m', do: { desk: 'size:m' }, mark: 'radio' },
			{ id: 'desk.size.l', label: 'desktop.size.l', do: { desk: 'size:l' }, mark: 'radio' },
		]),
		sep,
		{ id: 'desk.widgets', label: 'desktop.widgets.show', do: { desk: 'widgets' }, mark: 'check' },
		{ id: 'desk.dock', label: 'desktop.dock.show', do: { desk: 'dock' }, mark: 'check' },
	]),
];

function allowed(items: MenuItem[], role: Role): MenuItem[] {
	const out: MenuItem[] = [];
	for (const item of items) {
		if ('sep' in item) {
			// No separator first, last or twice.
			if (out.length && !('sep' in out[out.length - 1])) out.push(item);
			continue;
		}
		if (item.roles && !item.roles.includes(role)) continue;
		if (item.sub) {
			const inner = allowed(item.sub, role);
			if (!inner.length) continue;
			out.push({ ...item, sub: inner });
		} else out.push(item);
	}
	while (out.length && 'sep' in out[out.length - 1]) out.pop();
	return out;
}
function forRole(menus: Menu[], role: Role): Menu[] {
	return menus
		.map((m) => ({ ...m, items: allowed(m.items, role) }))
		.filter((m) => m.items.length > 0);
}

/** The menus of a window's program for an account: the program's, then Fenster and Hilfe. */
export function menusFor(key: string, role: Role): Menu[] {
	return forRole([...(PROGRAMS[programOf(key)] ?? []), WINDOW_MENU, HELP_MENU], role);
}

/** The menus with no window in front. */
export function desktopMenus(role: Role): Menu[] {
	return forRole([...DESKTOP_MENUS, WINDOW_MENU, HELP_MENU], role);
}

export function logoMenu(role: Role): Menu {
	return forRole([LOGO_MENU], role)[0];
}

/** Every entry of some menus, submenus included. */
export function entries(menus: Menu[]): MenuEntry[] {
	const out: MenuEntry[] = [];
	const walk = (items: MenuItem[]) => {
		for (const item of items) {
			if ('sep' in item) continue;
			out.push(item);
			if (item.sub) walk(item.sub);
		}
	};
	for (const m of menus) walk(m.items);
	return out;
}

/** The page commands a program's menus can call (anything else a frame announces is ignored). */
export function commandIds(key: string): Set<string> {
	const ids = new Set<string>();
	for (const e of entries([...(PROGRAMS[programOf(key)] ?? [])]))
		if (e.do && 'cmd' in e.do) ids.add(e.do.cmd);
	return ids;
}

/** The program keys with menus of their own (for tests). */
export const PROGRAM_KEYS = Object.keys(PROGRAMS);

/**
 * Where a "go" entry leads: a page of the window's own program opens in the
 * window; a page of another program opens that program (if the account may).
 */
export function goTarget(
	href: string,
	key: string,
	role: Role,
	services: Record<string, boolean>,
): { kind: 'here' | 'program'; app: string; href: string } | null {
	const app = key.startsWith('docker:') ? key.slice(7) : '';
	const path = href.replace('{app}', encodeURIComponent(app));
	const target = appForPath(path.split('?')[0]);
	if (!target || !appAllowed(target, role, services)) return null;
	if (key === 'app:' + target.id) return { kind: 'here', app: key, href: path };
	// A page the window's own side bar lists opens there, as from the side bar
	// (Systemsteuerung › Zugang, though Zugang belongs to Benutzer).
	const own = SYSTEM_APPS.find((a) => 'app:' + a.id === key);
	const bare = path.split('?')[0];
	if (own?.side.some((i) => 'href' in i && i.href.split('?')[0] === bare))
		return { kind: 'here', app: key, href: path };
	return { kind: 'program', app: 'app:' + target.id, href: path };
}
