// What the VMs page works out from the status, without the page itself.

import { ApiError, type VM, type VMStatus, type Volume } from '#lib/api.ts';
import { errorText, t, type Locale } from '#lib/i18n/index.svelte.ts';
import { vmLabel } from '#lib/vm-name.ts';
import type { ScaleMode } from './VncScreen.svelte';

export type Problem = { text: string; detail: string };
export type Failure = { problem: Problem; action: string };

export function problem(err: unknown): Problem {
	const code = err instanceof ApiError ? err.code : 'internal';
	return {
		text: errorText(code),
		detail: code === 'vm_failed' && err instanceof ApiError ? err.message : '',
	};
}

/** Mounted volumes at least 90 % full that hold a disk of the VM. */
export function lowSpaceVolumes(volumes: Volume[], vm: VM | undefined): string[] {
	return volumes
		.filter(
			(volume) =>
				volume.mounted &&
				volume.sizeBytes > 0 &&
				volume.usedBytes / volume.sizeBytes >= 0.9 &&
				vm?.disks.some(
					(disk) => disk.volume === volume.name || disk.path?.startsWith(`/mnt/${volume.name}/`),
				),
		)
		.map((volume) => volume.name);
}

export const pendingText = (action: string) =>
	t(action === 'start' ? 'ui.starting' : action === 'shutdown' ? 'ui.stopping' : 'ui.rebooting');

// One line of notices above the tabs; the most serious first, the rest
// behind "+N more".
export type Notice = {
	level: 'error' | 'warn' | 'info' | 'ok';
	text: string;
	detail?: string;
	retry?: boolean;
};

export function vmNotices(input: {
	current: VM | undefined;
	status: VMStatus | null;
	vms: VM[];
	failures: Record<string, Failure>;
	pending: Record<string, string>;
	lowSpace: string[];
	feedback: string;
}): Notice[] {
	const { current, status, vms, failures, pending, lowSpace, feedback } = input;
	if (!current) return [];
	const list: Notice[] = [];
	const failure = failures[current.name];
	if (failure)
		list.push({
			level: 'error',
			text: failure.problem.text,
			detail: failure.problem.detail,
			retry: !!failure.action,
		});
	if (current.job?.state === 'failed')
		list.push({ level: 'error', text: t('vms.jobFailed'), detail: current.job.text });
	if (status && !status.kvm) list.push({ level: 'warn', text: t('vms.noKvm') });
	if (!current.available) list.push({ level: 'warn', text: t('vms.unavailable') });
	if (current.job?.state === 'running')
		list.push({ level: 'warn', text: t('vms.jobRunning', { file: current.job.text }) });
	if (lowSpace.length)
		list.push({ level: 'warn', text: t('vms.lowSpace', { volumes: lowSpace.join(', ') }) });
	for (const [name, action] of Object.entries(pending))
		list.push({
			level: 'info',
			text: `${vmLabel(vms.find((vm) => vm.name === name) ?? current)}: ${pendingText(action)}`,
		});
	if (feedback) list.push({ level: 'ok', text: feedback });
	return list;
}

export const noticeClass = {
	error: 'notice-error',
	warn: 'notice-warn',
	info: 'notice-info',
	ok: 'notice-ok',
} as const;

// Key combinations the browser or the NAS's own desktop would keep for
// themselves; as X keysyms with the codes of the keys.
const CTRL: [number, string] = [0xffe3, 'ControlLeft'];
const ALT: [number, string] = [0xffe9, 'AltLeft'];
export const KEYS: { label: string; keys: [number, string][] }[] = [
	{ label: 'Strg+Alt+Entf', keys: [CTRL, ALT, [0xffff, 'Delete']] },
	{ label: 'Strg+Alt+⌫', keys: [CTRL, ALT, [0xff08, 'Backspace']] },
	...[1, 2, 3, 7].map((n) => ({
		label: `Strg+Alt+F${n}`,
		keys: [CTRL, ALT, [0xffbd + n, `F${n}`]] as [number, string][],
	})),
	{ label: 'Alt+Tab', keys: [ALT, [0xff09, 'Tab']] },
	{ label: 'Win', keys: [[0xffeb, 'MetaLeft']] },
	{ label: 'Druck', keys: [[0xff61, 'PrintScreen']] },
];
export const keyLabel = (label: string, locale: Locale) =>
	locale === 'en'
		? label.replace('Strg', 'Ctrl').replace('Entf', 'Del').replace('Druck', 'Print')
		: label;

export const MODES: ScaleMode[] = ['scale', 'remote', 'full'];
