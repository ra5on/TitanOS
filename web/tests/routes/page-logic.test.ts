// The logic taken out of the VMs, plugin and share pages.
import { afterEach, beforeEach, expect, test } from 'vitest';
import { ApiError, type Share, type VM, type Volume } from '#lib/api.ts';
import { i18n } from '#lib/i18n/index.svelte.ts';
import {
	categoryOf,
	hue,
	isLink,
	localized,
	matches,
	monogram,
	pluginProblem,
	type Plugin,
} from '../../src/routes/plugins/plugins.ts';
import { accessSummary, shareAddresses } from '../../src/routes/shares/shares-page.ts';
import { keyLabel, lowSpaceVolumes, problem, vmNotices } from '../../src/routes/vms/vm-page.ts';

let before: typeof i18n.locale;
beforeEach(() => {
	before = i18n.locale;
	i18n.locale = 'de';
});
afterEach(() => {
	i18n.locale = before;
});

const vm = (over: Partial<VM> = {}) =>
	({
		name: 'web',
		displayName: 'Web',
		disks: [{ file: 'a.qcow2', format: 'qcow2', sizeBytes: 1, volume: 'Data' }],
		available: true,
		job: null,
		...over,
	}) as VM;
const volume = (name: string, used: number, mounted = true) =>
	({ name, mounted, sizeBytes: 100, usedBytes: used }) as Volume;

test('vms: volumes at 90 % that hold a disk of the VM', () => {
	const volumes = [volume('Data', 95), volume('Fast', 99), volume('Old', 99, false)];
	expect(lowSpaceVolumes(volumes, vm())).toEqual(['Data']);
	expect(lowSpaceVolumes([volume('Data', 89)], vm())).toEqual([]);
	const onPath = vm({ disks: [{ file: 'x', path: '/mnt/Fast/vm/x.img' } as VM['disks'][0]] });
	expect(lowSpaceVolumes(volumes, onPath)).toEqual(['Fast']);
	expect(lowSpaceVolumes(volumes, undefined)).toEqual([]);
});

test('vms: notices, the most serious first', () => {
	const current = vm({ available: false, job: { state: 'running', text: 'disk.qcow2' } });
	const notices = vmNotices({
		current,
		status: { kvm: false } as never,
		vms: [current],
		failures: { web: { problem: { text: 'Fehler', detail: 'd' }, action: 'start' } },
		pending: { web: 'shutdown' },
		lowSpace: ['Data'],
		feedback: 'ok',
	});
	expect(notices.map((n) => n.level)).toEqual([
		'error',
		'warn',
		'warn',
		'warn',
		'warn',
		'info',
		'ok',
	]);
	expect(notices[0]).toEqual({ level: 'error', text: 'Fehler', detail: 'd', retry: true });
	expect(notices[5].text.startsWith('Web: ')).toBe(true);
	expect(vmNotices({ current: undefined } as never)).toEqual([]);
});

test('vms: the NAS message only for vm_failed, keys in English', () => {
	expect(problem(new ApiError(500, 'vm_failed', 'qemu said no')).detail).toBe('qemu said no');
	expect(problem(new ApiError(500, 'internal', 'secret')).detail).toBe('');
	expect(problem(new Error('x')).detail).toBe('');
	expect(keyLabel('Strg+Alt+Entf', 'en')).toBe('Ctrl+Alt+Del');
	expect(keyLabel('Druck', 'en')).toBe('Print');
	expect(keyLabel('Strg+Alt+Entf', 'de')).toBe('Strg+Alt+Entf');
});

const plugin = (over: Partial<Plugin> = {}) =>
	({
		name: 'nvidia',
		title: 'NVIDIA-Treiber',
		description: { de: 'Grafik', en: 'Graphics' },
		...over,
	}) as Plugin;

test('plugins: texts, categories and search', () => {
	expect(localized({ de: 'a', en: 'b' }, 'de')).toBe('a');
	expect(localized({ en: 'b' }, 'de')).toBe('b');
	expect(localized({ fr: 'c' }, 'de')).toBe('c');
	expect(localized(null, 'de')).toBe('');
	expect(categoryOf({ category: 'graphics' })).toBe('graphics');
	expect(categoryOf({ category: 'toys' })).toBe('other');
	expect(categoryOf({})).toBe('other');
	const p = plugin({
		details: { de: { hardware: ['RTX 4060'] } },
		devices: [{ key: 'k', bus: 'pci', name: 'GeForce' }],
	});
	expect(matches(p, '', 'de')).toBe(true);
	expect(matches(p, 'grafik rtx', 'de')).toBe(true);
	expect(matches(p, 'geforce', 'de')).toBe(true);
	expect(matches(p, 'graphics', 'de')).toBe(false);
	expect(matches(p, 'graphics', 'en')).toBe(true);
});

test('plugins: monogram, hue, links and error details', () => {
	expect(monogram(plugin())).toBe('NV');
	expect(monogram(plugin({ title: 'zfs' }))).toBe('Zf');
	expect(monogram(plugin({ title: '–' }))).toBe('?');
	expect(hue(plugin())).toBe(hue(plugin({ title: 'other' })));
	expect(hue(plugin())).toBeGreaterThanOrEqual(0);
	expect(hue(plugin())).toBeLessThan(360);
	expect(isLink('https://example.org/x')).toBe(true);
	expect(isLink('http://example.org/x')).toBe(false);
	expect(isLink('https://a b')).toBe(false);
	expect(pluginProblem(new ApiError(502, 'plugin_download_failed', '404')).detail).toBe('404');
	expect(pluginProblem(new ApiError(500, 'internal', 'x')).detail).toBe('');
});

const share = (over: Partial<Share> = {}) =>
	({
		name: 'Fotos Alt',
		smb: true,
		nfs: { enabled: false, clients: null },
		access: { alex: 'rw' },
		...over,
	}) as Share;

test('shares: addresses for each system', () => {
	expect(shareAddresses(share(), 'nas').map((a) => a.value)).toEqual([
		'\\\\nas\\Fotos Alt',
		'smb://nas/Fotos%20Alt',
		'smb://nas/Fotos%20Alt',
		'//nas/Fotos Alt',
	]);
	const nfsOnly = share({ smb: false, nfs: { enabled: true, clients: null } });
	expect(shareAddresses(nfsOnly, '10.0.0.2')).toEqual([
		{ label: expect.any(String), value: '10.0.0.2:/Fotos Alt' },
	]);
	expect(shareAddresses(share({ smb: false }), 'nas')).toEqual([]);
});

test('shares: one account or several', () => {
	expect(accessSummary(share())).toMatch(/1/);
	expect(accessSummary(share({ access: { a: 'ro', b: 'rw', c: 'rw' } }))).toMatch(/3/);
});
