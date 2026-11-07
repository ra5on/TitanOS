// The list "Nächste Schritte" of /welcome (step 3) with what is already done.
// Shared since PLAN 71: Systemsteuerung › Übersicht shows the open entries
// under "Zu erledigen". Read once when asked, never on a timer.
import { api } from './api.ts';
import { session } from './session.svelte.ts';

export type SetupTodoKey =
	| 'welcome.todoVolume'
	| 'welcome.todoShare'
	| 'welcome.todoTwoFactor'
	| 'welcome.todoNotify'
	| 'welcome.todoStick'
	| 'welcome.todoApps'
	| 'welcome.todoHardware'
	| 'welcome.todoDrivers'
	| 'welcome.todoBackup'
	| 'welcome.todoRemote'
	| 'welcome.todoPush';

/** One step; done is null while unknown (not loaded or not readable). */
export type SetupTodo = { href: string; key: SetupTodoKey; done: boolean | null };

/**
 * Asks the NAS what is done. Without `admin` only the account's own steps are
 * asked (two-factor login): the other answers need an administrator and
 * would only be refused.
 */
export async function checkSetup(admin: boolean): Promise<{ todos: SetupTodo[]; failed: boolean }> {
	const twoFactor: SetupTodo = {
		href: '/account',
		key: 'welcome.todoTwoFactor',
		done: session.data?.user ? session.data.user.twoFactor : null,
	};
	if (!admin) return { todos: [twoFactor], failed: false };
	const [storage, shares, notify, stick, plugins, backups, tailnet, wireguard, push] =
		await Promise.allSettled([
			api.get<{ volumes: unknown[] }>('/storage'),
			api.get<unknown[]>('/shares'),
			api.get<{ settings: Record<string, unknown> }>('/notifications/settings'),
			api.get<{ backup: { lastError: string; backups: unknown[] } }>('/config-backup'),
			api.get<{ recommendations?: unknown[] | null }>('/plugins'),
			api.get<{ jobs: { job: { enabled: boolean } }[] }>('/backups'),
			api.get<{ state: string }>('/plugins/tailscale'),
			api.get<{ enabled: boolean }>('/network/wireguard'),
			api.get<{ devices: unknown[] }>('/notifications/push'),
		]);
	// An answer of an unexpected shape counts as "unknown", not as an error of the page.
	const value = <T>(result: PromiseSettledResult<T>, test: (value: T) => boolean) => {
		if (result.status !== 'fulfilled') return null;
		try {
			return test(result.value);
		} catch {
			return null;
		}
	};
	const channel = (settings: Record<string, unknown>) =>
		['email', 'ntfy', 'telegram', 'discord', 'homeAssistant'].some(
			(name) => (settings[name] as { enabled?: boolean } | undefined)?.enabled,
		);
	const todos: SetupTodo[] = [
		{
			href: '/storage',
			key: 'welcome.todoVolume',
			done: value(storage, (s) => s.volumes.length > 0),
		},
		{
			href: '/shares',
			key: 'welcome.todoShare',
			done: value(shares, (list) => Array.isArray(list) && list.length > 0),
		},
		twoFactor,
		{
			href: '/settings/notifications',
			key: 'welcome.todoNotify',
			done: value(notify, (n) => channel(n.settings)),
		},
		{
			href: '/settings/backups/configuration',
			key: 'welcome.todoStick',
			done: value(stick, (c) => c.backup.backups.length > 0 && !c.backup.lastError),
		},
		{
			href: '/settings/backups/data',
			key: 'welcome.todoBackup',
			done: value(backups, (b) => b.jobs.some((j) => j.job.enabled)),
		},
		{
			href: '/settings/network?tab=remote',
			key: 'welcome.todoRemote',
			done:
				(tailnet.status === 'fulfilled' && tailnet.value.state === 'Running') ||
				(wireguard.status === 'fulfilled' && wireguard.value.enabled)
					? true
					: tailnet.status === 'rejected' && wireguard.status === 'rejected'
						? null
						: false,
		},
		{
			href: '/settings/notifications#push',
			key: 'welcome.todoPush',
			done: value(push, (p) => p.devices.length > 0),
		},
		{ href: '/docker', key: 'welcome.todoApps', done: null },
		{ href: '/tools/hardware', key: 'welcome.todoHardware', done: null },
	];
	// Plugins for detected hardware (drivers, tools): only when there are any.
	if (plugins.status === 'fulfilled' && (plugins.value.recommendations?.length ?? 0) > 0)
		todos.splice(1, 0, { href: '/plugins', key: 'welcome.todoDrivers', done: false });
	const failed = [storage, shares, notify, stick].some((result) => result.status === 'rejected');
	return { todos, failed };
}
