<script lang="ts">
	// PLAN 75-20: what Einstellungen › Programme held for one app, now on the
	// app's own page – which accounts may use it and whether its pages ask
	// for the TitanOS login in the browser. Per account the same is set under
	// Benutzer (draft U2). Changes are stored right away.
	import { api } from '#lib/api.ts';
	import Toggle from '#lib/components/appform/Toggle.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	type Role = 'owner' | 'admin' | 'user';
	type AccessApp = { app: string; gate: boolean; users: string[] };
	type Access = {
		newApps: string;
		apps: AccessApp[];
		accounts: { name: string; role: Role; disabled: boolean }[];
	};

	let { app }: { app: string } = $props();

	let access = $state<Access | null>(null);
	let error = $state('');
	let busy = $state(false);
	const mine = $derived(access?.apps.find((a) => a.app === app) ?? null);
	const normal = $derived((access?.accounts ?? []).filter((a) => a.role === 'user'));

	$effect(() => {
		void app;
		api.get<Access>('/apps/access').then(
			(res) => (access = res),
			(err) => (error = apiErrorText(err)),
		);
	});

	async function store(change: (entry: AccessApp) => void) {
		if (!access || !mine || busy) return;
		busy = true;
		error = '';
		const apps = access.apps.map((a) => ({ app: a.app, gate: a.gate, users: [...a.users] }));
		const entry = apps.find((a) => a.app === app);
		if (entry) change(entry);
		try {
			access = await api.put<Access>('/apps/access', { newApps: access.newApps, apps });
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if mine}
	<div class="access">
		<div class="who">
			{#each normal as account (account.name)}
				<label class="user">
					<input
						type="checkbox"
						checked={mine.users.includes(account.name)}
						disabled={busy}
						aria-label={t('appaccess.allowFor', { app, user: account.name })}
						onchange={(event) => {
							const on = event.currentTarget.checked;
							void store((e) => {
								e.users = on
									? [...e.users, account.name]
									: e.users.filter((u) => u !== account.name);
							});
						}}
					/>
					<span class="mono">{account.name}</span>
				</label>
			{:else}
				<span class="muted">{t('appaccess.noUsers')}</span>
			{/each}
		</div>
		<p class="muted small">{t('appaccess.adminAlways')}</p>
		<div class="gate">
			<Toggle
				checked={mine.gate}
				label={t('appaccess.gateFor', { app })}
				onchange={(on: boolean) => void store((e) => (e.gate = on))}
			/>
			<span>{t('appaccess.gate')}</span>
		</div>
		<p class="muted small">{t('appaccess.gateHint')}</p>
	</div>
{:else if access}
	<p class="muted">{t('appaccess.empty')}</p>
{/if}

<style>
	.access {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.who {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-4);
	}
	.user,
	.gate {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
	}
	.small {
		margin: 0;
		font-size: var(--text-sm);
	}
</style>
