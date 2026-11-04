import { beforeEach, expect, test, vi } from 'vitest';

const answers = vi.hoisted(() => ({ value: {} as Record<string, unknown>, asked: [] as string[] }));

vi.mock('#lib/api.ts', () => ({
	api: {
		get: async (path: string) => {
			answers.asked.push(path);
			if (!(path in answers.value)) throw new Error('unreachable');
			return answers.value[path];
		},
	},
}));
vi.mock('#lib/notifications.svelte.ts', () => ({ notifications: { list: [] as unknown[] } }));

const { loadSettingsStates, settingsStates } = await import('#lib/settings-states.svelte.ts');
const { notifications } = await import('#lib/notifications.svelte.ts');
const inDays = (days: number) => new Date(Date.now() + days * 86_400_000).toISOString();

beforeEach(() => {
	answers.value = {};
	answers.asked = [];
	(notifications as { list: unknown[] }).list = [];
});

test('non-admins ask nothing and see no states', () => {
	loadSettingsStates(false);
	expect(answers.asked).toEqual([]);
	expect(settingsStates()).toEqual({});
});

test('update, battery and certificate become area states', async () => {
	answers.value = {
		'/update': { available: { version: '0.48.0' } },
		'/ups': { status: { onBattery: true, lowBattery: true } },
		'/system/certificate': { notAfter: inDays(3) },
	};
	loadSettingsStates(true);
	await vi.waitFor(() => expect(Object.keys(settingsStates())).toHaveLength(3));
	expect(settingsStates()).toEqual({
		updates: { text: 'settings.state.update', tone: 'info' },
		ups: { text: 'settings.state.lowBattery', tone: 'error' },
		access: { text: 'settings.state.certExpiring', tone: 'warn' },
	});
});

test('a failing answer leaves the other states', async () => {
	answers.value = { '/system/certificate': { notAfter: inDays(-1) } };
	loadSettingsStates(true);
	await vi.waitFor(() =>
		expect(settingsStates().access).toEqual({ text: 'settings.state.certExpired', tone: 'error' }),
	);
	expect(settingsStates().updates).toBeUndefined();
});

test('a backup is overdue only if its newest message says so', () => {
	(notifications as { list: unknown[] }).list = [
		{ kind: 'backup_ok', subject: 'nightly' },
		{ kind: 'backup_overdue', subject: 'nightly' },
	];
	expect(settingsStates().backups).toBeUndefined();
	(notifications as { list: unknown[] }).list = [
		{ kind: 'backup_overdue', subject: 'weekly' },
		{ kind: 'backup_ok', subject: 'nightly' },
	];
	expect(settingsStates().backups).toEqual({ text: 'settings.state.backupOverdue', tone: 'warn' });
});
