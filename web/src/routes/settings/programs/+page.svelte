<script lang="ts">
	// Programme (PLAN 61): which account may use which Docker app. Owners and
	// administrators always may; a normal account needs a tick. Without one
	// the app is also locked at its HTTPS name in the browser (app gate).
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	type Role = 'owner' | 'admin' | 'user';
	type AccessApp = {
		app: string;
		icon: string;
		url: string;
		webui: string;
		running: boolean;
		gate: boolean;
		users: string[];
	};
	type Access = {
		newApps: 'admins' | 'everyone';
		apps: AccessApp[];
		accounts: { name: string; role: Role; disabled: boolean }[];
	};

	let access = $state<Access | null>(null);
	let loadError = $state('');
	let busy = $state(false);
	let error = $state('');
	let saved = $state(false);
	let dirty = $state(false);

	const admin = (role: Role) => role === 'owner' || role === 'admin';
	// Owners first, then administrators, then the others, each by name.
	const accounts = $derived(
		[...(access?.accounts ?? [])].sort(
			(a, b) =>
				['owner', 'admin', 'user'].indexOf(a.role) - ['owner', 'admin', 'user'].indexOf(b.role) ||
				a.name.localeCompare(b.name),
		),
	);
	const normal = $derived(accounts.filter((account) => !admin(account.role)));
	const unnamed = $derived((access?.apps ?? []).filter((app) => !app.url).length);

	async function load() {
		loadError = '';
		try {
			access = await api.get<Access>('/apps/access');
			dirty = false;
		} catch (err) {
			loadError = apiErrorText(err);
		}
	}
	onMount(load);

	function changed() {
		dirty = true;
		saved = false;
	}
	function tick(app: AccessApp, user: string, on: boolean) {
		app.users = on ? [...app.users, user] : app.users.filter((name) => name !== user);
		changed();
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!access || busy) return;
		busy = true;
		error = '';
		saved = false;
		try {
			access = await api.put<Access>('/apps/access', {
				newApps: access.newApps,
				apps: access.apps.map((app) => ({ app: app.app, gate: app.gate, users: app.users })),
			});
			dirty = false;
			saved = true;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<PageHead title={t('appaccess.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('appaccess.intro')}</p>
</PageHead>

{#if loadError}
	<div class="notice-error retry" role="alert">
		<span>{loadError}</span><button class="btn" type="button" onclick={load}
			>{t('common.retry')}</button
		>
	</div>
{:else if !access}
	<p class="muted" role="status">{t('common.loading')}</p>
{:else}
	<form class="tile panel" aria-labelledby="programs-apps" onsubmit={save}>
		<h2 class="tile-title" id="programs-apps">{t('appaccess.groupDocker')}</h2>
		{#if !access.apps.length}
			<p class="muted">{t('appaccess.empty')}</p>
			<div class="row-actions"><a class="btn" href="/docker">{t('appaccess.openDocker')}</a></div>
		{:else}
			<div class="table-wrap">
				<table class="data-table matrix">
					<thead>
						<tr>
							<th scope="col">{t('appaccess.colApp')}</th>
							{#each accounts as account (account.name)}
								<th scope="col" class="who">
									<span class="name">{account.name}</span>
									<span class="role"
										>{t(`appaccess.role.${account.role}`)}{#if account.disabled}
											· {t('appaccess.locked')}{/if}</span
									>
								</th>
							{/each}
							<th scope="col" class="who">{t('appaccess.gate')}</th>
						</tr>
					</thead>
					<tbody>
						{#each access.apps as app (app.app)}
							<tr>
								<th scope="row">
									<span class="app">
										<span class="icon"><AppIcon src={app.icon} name={app.app} /></span>
										<span class="app-text">
											<span>{app.app}</span>
											{#if !app.url}<span class="muted small">{t('appaccess.noName')}</span>{/if}
										</span>
									</span>
								</th>
								{#each accounts as account (account.name)}
									<td class="cell">
										{#if admin(account.role)}
											<input
												type="checkbox"
												checked
												disabled
												aria-label={t('appaccess.allowFor', { app: app.app, user: account.name })}
											/>
										{:else}
											<input
												type="checkbox"
												checked={app.users.includes(account.name)}
												disabled={busy}
												aria-label={t('appaccess.allowFor', { app: app.app, user: account.name })}
												onchange={(event) => tick(app, account.name, event.currentTarget.checked)}
											/>
										{/if}
									</td>
								{/each}
								<td class="cell">
									<input
										type="checkbox"
										bind:checked={app.gate}
										disabled={busy}
										aria-label={t('appaccess.gateFor', { app: app.app })}
										onchange={changed}
									/>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
			<p class="muted hint">{t('appaccess.adminAlways')}</p>
			{#if !normal.length}
				<p class="muted hint">{t('appaccess.noUsers')}</p>
				<div class="row-actions"><a class="btn" href="/users">{t('appaccess.openUsers')}</a></div>
			{/if}
			{#if unnamed}
				<p class="notice-warn">{t('appaccess.noNameHint')}</p>
				<div class="row-actions">
					<a class="btn" href="/settings/proxy">{t('appaccess.openProxy')}</a>
				</div>
			{/if}
		{/if}

		<div class="divided fields">
			<label class="field-row">
				<span class="label">{t('appaccess.newApps')}</span>
				<span class="value">
					<select class="field" bind:value={access.newApps} disabled={busy} onchange={changed}>
						<option value="admins">{t('appaccess.newAppsAdmins')}</option>
						<option value="everyone">{t('appaccess.newAppsEveryone')}</option>
					</select>
					<span class="muted small">{t('appaccess.newAppsHint')}</span>
				</span>
			</label>
		</div>

		<details class="explain">
			<summary>{t('appaccess.gate')}</summary>
			<p class="hint">{t('appaccess.gateHint')}</p>
			<p class="hint">{t('appaccess.gateLimit')}</p>
		</details>

		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="row-actions">
			<button class="btn btn-primary" disabled={busy || !dirty}>{t('common.save')}</button>
			{#if saved}<span class="muted" role="status">{t('appaccess.saved')}</span>{/if}
		</div>
	</form>
{/if}

<style>
	.retry {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
	}
	.hint {
		font-size: var(--text-md);
	}
	.small {
		font-size: var(--text-base);
	}
	.matrix th.who,
	.matrix td.cell {
		text-align: center;
	}
	.matrix th.who {
		min-width: 6.5rem;
	}
	.who .name {
		display: block;
		color: var(--text);
		font-weight: 600;
	}
	.who .role {
		display: block;
	}
	.matrix tbody th {
		font-weight: 500;
	}
	.matrix input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.app {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 10rem;
	}
	.icon {
		width: 28px;
		height: 28px;
		flex: none;
		border-radius: 6px;
		font-size: var(--text-sm);
		font-weight: 600;
		color: var(--text);
		overflow: hidden;
	}
	.app-text {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	/* One column of label and field, as on the other settings pages. */
	.field-row {
		display: grid;
		grid-template-columns: minmax(8rem, 12rem) minmax(0, 1fr);
		align-items: start;
		gap: var(--space-2) var(--space-3);
		font-size: var(--text-md);
	}
	.field-row .label {
		padding-top: 8px;
		color: var(--muted);
	}
	.field-row .value {
		display: flex;
		flex-direction: column;
		gap: 6px;
		max-width: 28rem;
	}
	@media (max-width: 640px) {
		.field-row {
			grid-template-columns: minmax(0, 1fr);
		}
		.field-row .label {
			padding-top: 0;
		}
	}
	.explain summary {
		cursor: pointer;
		color: var(--accent);
		font-size: var(--text-md);
	}
	.explain[open] summary {
		margin-bottom: var(--space-1);
	}
</style>
