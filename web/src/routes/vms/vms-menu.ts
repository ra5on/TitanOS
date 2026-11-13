// PLAN 67-1: the menu bar of the desktop acts on the VM shown.
import { goto } from '$app/navigation';
import { i18n } from '#lib/i18n/index.svelte.ts';
import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
import { addVmConsole } from '#lib/terminal.svelte.ts';
import { vmLabel } from '#lib/vm-name.ts';
import { KEYS, keyLabel, MODES } from './vm-page.ts';
import { TABS, type VmsPage } from './vms-page.svelte.ts';

/** Registers the commands of the page's menu bar; call while the page's component starts. */
export function registerVmsMenu(p: VmsPage) {
	const vmOn = () => p.current?.state === 'running';
	const vmFree = () => !!p.current && !p.isBusy(p.current);
	const screenOn = () => p.tab === 'screen' && vmOn() && !!p.screen;
	registerMenuCommands({
		// PLAN 87: pulling the list down on the phone (PhoneGestures); not on the screen.
		'vms.refresh': { run: () => void p.load(), enabled: () => p.tab !== 'screen' },
		'vms.start': {
			run: () => p.current && p.act(p.current, 'start'),
			enabled: () => vmFree() && !vmOn() && !!p.current && p.canStart(p.current),
		},
		'vms.shutdown': {
			run: () => p.current && p.act(p.current, 'shutdown'),
			enabled: () => vmFree() && vmOn(),
		},
		'vms.reboot': {
			run: () => p.current && p.act(p.current, 'reboot'),
			enabled: () => vmFree() && vmOn(),
		},
		'vms.poweroff': { run: () => p.open('poweroff'), enabled: () => vmFree() && vmOn() },
		'vms.clone': { run: () => (p.cloning = p.current ?? null), enabled: () => vmFree() && !vmOn() },
		'vms.remove': { run: () => p.open('remove'), enabled: vmFree },
		'vms.edit': {
			run: () => p.current && goto(`/vms/edit?vm=${encodeURIComponent(p.current.name)}`),
			enabled: () => !!p.current,
		},
		...Object.fromEntries(
			TABS.map((next) => [
				'vms.tab.' + next,
				{
					run: () => p.current && goto(p.link(p.current, next)),
					enabled: () => !!p.current,
					checked: () => !!p.current && p.tab === next,
				},
			]),
		),
		'vms.cad': { run: () => p.screen?.ctrlAltDel(), enabled: screenOn },
		// "Tasten senden ›": the key combinations of the screen's own key menu.
		'vms.keys': {
			items: () =>
				screenOn()
					? KEYS.map((k, i) => ({ id: String(i), text: keyLabel(k.label, i18n.locale) }))
					: [],
			run: (i) => {
				const entry = KEYS[Number(i)];
				if (entry) p.screen?.sendKeys(entry.keys);
			},
		},
		'vms.isoInsert': { run: () => void p.withOverview((o) => o.insertMedium()), enabled: vmFree },
		'vms.isoEject': {
			run: () => void p.withOverview((o) => o.ejectMedium()),
			enabled: () => vmFree() && !!p.current?.iso && !!p.current?.disks.length,
		},
		...Object.fromEntries(
			MODES.map((entry) => [
				'vms.mode.' + entry,
				{
					run: () => (p.mode = entry),
					enabled: () => entry !== 'remote' || p.current?.template === 'linux',
					checked: () => p.shownMode === entry,
				},
			]),
		),
		'vms.fullscreen': { run: () => void p.size.frame?.requestFullscreen(), enabled: screenOn },
		'vms.screenshot': { run: p.saveScreenshot, enabled: screenOn },
		'vms.paste': { run: () => p.open('paste'), enabled: screenOn },
		'vms.serial': {
			run: () => p.current && addVmConsole(p.current.name, vmLabel(p.current)),
			enabled: vmOn,
		},
	});
}
