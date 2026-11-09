<script lang="ts">
	// PLAN 71 (owner's decision "Variante C", 7 Nov 2026): Systemsteuerung ›
	// Übersicht. The side bar of the window lists every area; this page only
	// shows how the NAS is doing, what is still open, which services run and
	// what was changed last. Only real answers of the NAS; what an account may
	// not read is not asked for.
	import { onMount } from 'svelte';
	import { api, type VolumeState } from '#lib/api.ts';
	import { activityLabel, type ActivityEntry, type ActivityPage } from '#lib/activity.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime, formatDuration } from '#lib/format.ts';
	import { settingsAreas } from '#lib/settings-search.ts';
	import { recentSettingsChanges } from '#lib/settings-recent.ts';
	import { settingsStates } from '#lib/settings-states.svelte.ts';
	import { checkSetup, type SetupTodo } from '#lib/setup-checklist.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	let { admin, prefix = 'overview' }: { admin: boolean; prefix?: string } = $props();

	const info = $derived(system.info);
	// Accounts with the role "user" get no volumes from the NAS: the row is left out.
	const volumes = $derived(storage.status?.volumes ?? null);
	const tone = (state: VolumeState) =>
		state === 'ok' ? 'ok' : state === 'failed' || state === 'missing' ? 'error' : 'warn';

	const areaLabel = (id: string) => {
		const area = settingsAreas.find((a) => a.id === id);
		return area ? t(area.label) : id;
	};

	// "Zu erledigen": the states of the settings (update, battery, certificate,
	// overdue backup; read once by the settings layout) and the open steps of
	// the setup list of /welcome.
	const states = $derived(Object.entries(settingsStates()));
	let setup = $state<SetupTodo[] | null>(null);
	let setupFailed = $state(false);
	const open = $derived((setup ?? []).filter((todo) => todo.done === false));

	// Services: Docker, VMs and LXC from Settings › Services; SSH and the
	// firewall from their own pages. Administrators only.
	type Service = { enabled: boolean; running: number; total: number; available: boolean };
	const NAMES = ['docker', 'vms', 'lxc'] as const;
	let services = $state<Partial<Record<(typeof NAMES)[number], Service>> | null>(null);
	let ssh = $state<{ enabled: boolean; running: boolean } | null>(null);
	let firewall = $state<{ config: { enabled: boolean }; active: boolean } | null>(null);
	// PLAN 75-4: the accounts, one row with a way into Systemsteuerung › Benutzer.
	let accounts = $state<{ name: string; disabled?: boolean }[] | null>(null);

	let recent = $state<(ActivityEntry & { area: string })[]>([]);

	onMount(() => {
		void checkSetup(admin).then((result) => {
			setup = result.todos;
			setupFailed = result.failed;
		});
		if (!admin) return;
		api
			.get<typeof services>('/services')
			.then((v) => (services = v && typeof v === 'object' ? v : {}))
			.catch(() => (services = {}));
		api
			.get<{ name: string; disabled?: boolean }[]>('/users')
			.then((v) => (accounts = Array.isArray(v) ? v : null))
			.catch(() => {});
		api
			.get<{ enabled: boolean; running: boolean }>('/system/ssh')
			.then((v) => (ssh = typeof v?.enabled === 'boolean' ? v : null))
			.catch(() => {});
		api
			.get<{ config: { enabled: boolean }; active: boolean }>('/firewall')
			.then((v) => (firewall = typeof v?.config?.enabled === 'boolean' ? v : null))
			.catch(() => {});
		api
			.get<ActivityPage>('/activity?days=30&limit=500')
			.then((page) => (recent = recentSettingsChanges(page.entries ?? [])))
			.catch(() => (recent = []));
	});

	function describe(entry: ActivityEntry): string {
		const label = activityLabel(entry.action);
		if (label.kind === 'named') return t(`activity.do.${label.key}` as 'activity.do.login');
		const verb = t(`activity.verb.${label.verb}` as 'activity.verb.run');
		return entry.target ? `${verb}: ${entry.target}` : verb;
	}
</script>

<div class="overview">
	<section class="tile ui-card health" aria-labelledby="{prefix}-health">
		<div class="card-head"><h2 id="{prefix}-health">{t('settingshome.health')}</h2></div>
		{#if info}
			<div class="set-row">
				<span class="set-label">{t('settingshome.health.host')}</span>
				<span class="set-control value">{info.hostname}</span>
			</div>
			<div class="set-row">
				<span class="set-label">{t('settingshome.health.version')}</span>
				<span class="set-control value">{info.version}</span>
			</div>
			<div class="set-row">
				<span class="set-label">{t('settingshome.health.uptime')}</span>
				<span class="set-control value">{formatDuration(info.uptimeSeconds)}</span>
			</div>
		{:else}
			<p class="card-text muted" role="status">{t('settingshome.health.loading')}</p>
		{/if}
		{#if admin && volumes}
			<div class="set-row">
				<span class="set-label"
					>{t('settingshome.health.volumes')}{#if volumes.length}<span class="set-hint"
							>{volumes.every((v) => v.state === 'ok')
								? t('settingshome.health.allOk')
								: t('settingshome.health.attention')}</span
						>{/if}</span
				>
				<span class="set-control">
					{#each volumes as volume (volume.name)}
						<a class="chip {tone(volume.state)}" href="/storage"
							>{volume.name}: {t(`state.${volume.state}` as MessageKey)}</a
						>
					{:else}
						<a class="chip warn" href="/storage">{t('settingshome.health.noVolume')}</a>
					{/each}
				</span>
			</div>
		{/if}
	</section>

	<section class="tile ui-card todo" aria-labelledby="{prefix}-todo">
		<div class="card-head"><h2 id="{prefix}-todo">{t('settingshome.todo')}</h2></div>
		{#each states as [area, state] (area)}
			<div class="set-row">
				<span class="set-label"
					><span><span class="chip {state.tone}">{t(state.text)}</span></span><span class="set-hint"
						>{areaLabel(area)}</span
					></span
				>
				<span class="set-control end"
					><a class="btn" href="/settings/{area}">{t('settingshome.open')}</a></span
				>
			</div>
		{/each}
		{#each open as todo (todo.key)}
			<div class="set-row">
				<span class="set-label"
					>{t(todo.key)}<span class="set-hint">{t(`${todo.key}Text` as MessageKey)}</span></span
				>
				<span class="set-control end"
					><a class="btn" href={todo.href}>{t('settingshome.open')}</a></span
				>
			</div>
		{/each}
		{#if setup === null}
			<p class="card-text muted" role="status">{t('settingshome.todo.checking')}</p>
		{:else if !states.length && !open.length}
			<p class="card-text done" role="status">
				<span class="mark" aria-hidden="true">✓</span>{t('settingshome.todo.none')}
			</p>
		{/if}
		{#if setupFailed}<p class="card-text muted">{t('settingshome.todo.unknown')}</p>{/if}
	</section>

	{#if admin}
		<section class="tile ui-card services" aria-labelledby="{prefix}-services">
			<div class="card-head">
				<h2 id="{prefix}-services">{t('settingshome.services')}</h2>
				<div class="card-tools">
					<a class="btn" href="/settings/services">{t('settingshome.services.manage')}</a>
				</div>
			</div>
			{#each NAMES as name (name)}
				{@const s = services?.[name]}
				{#if s && typeof s.enabled === 'boolean'}
					<div class="set-row">
						<span class="set-label">{t(`services.${name}`)}</span>
						<span class="set-control">
							{#if !s.available}
								<span class="state">{t('settingshome.services.notPossible')}</span>
							{:else}
								<span class="state" class:on={s.enabled}
									><span class="dot" aria-hidden="true"></span>{s.enabled
										? t('settingshome.services.on')
										: t('settingshome.services.off')}</span
								>
								{#if s.enabled && s.total}<span class="muted"
										>{t('services.counts', { running: s.running, total: s.total })}</span
									>{/if}
							{/if}
						</span>
					</div>
				{/if}
			{/each}
			{#if accounts}
				{@const locked = accounts.filter((a) => a.disabled).length}
				<div class="set-row">
					<span class="set-label">{t('desktop.app.users')}</span>
					<span class="set-control">
						<span class="state"
							>{t('settingshome.users.count', { n: accounts.length })}{#if locked}{' · '}{t(
									'settingshome.users.locked',
									{ n: locked },
								)}{/if}</span
						>
						<a class="more" href="/settings/users">{t('settingshome.open')}</a>
					</span>
				</div>
			{/if}
			{#if ssh}
				<div class="set-row">
					<span class="set-label">{t('access.ssh')}</span>
					<span class="set-control">
						<span class="state" class:on={ssh.enabled}
							><span class="dot" aria-hidden="true"></span>{ssh.enabled
								? t('settingshome.services.on')
								: t('settingshome.services.off')}</span
						>
						<a class="more" href="/settings/access#access-ssh">{t('settingshome.open')}</a>
					</span>
				</div>
			{/if}
			{#if firewall}
				<div class="set-row">
					<span class="set-label">{t('firewall.title')}</span>
					<span class="set-control">
						<span class="state" class:on={firewall.config.enabled}
							><span class="dot" aria-hidden="true"></span>{firewall.config.enabled
								? t('settingshome.services.on')
								: t('settingshome.services.off')}</span
						>
						<a class="more" href="/settings/firewall">{t('settingshome.open')}</a>
					</span>
				</div>
			{/if}
			{#if services === null}
				<p class="card-text muted" role="status">{t('settingshome.health.loading')}</p>
			{/if}
		</section>
	{/if}

	{#if admin && recent.length}
		<section class="tile ui-card recent-card" aria-labelledby="{prefix}-recent">
			<div class="card-head">
				<h2 id="{prefix}-recent">{t('settingshome.recent')}</h2>
				<div class="card-tools">
					<a class="btn" href="/tools/activity">{t('settingshome.recentAll')}</a>
				</div>
			</div>
			<ul class="recent">
				{#each recent as entry, index (index)}
					<li>
						<a class="area" href="/settings/{entry.area}">{areaLabel(entry.area)}</a>
						<span class="what">{describe(entry)}</span>
						<span class="who"
							>{entry.user ? t('settingshome.recentBy', { user: entry.user }) : ''}</span
						>
						<time datetime={entry.at}>{formatDateTime(new Date(entry.at))}</time>
					</li>
				{/each}
			</ul>
		</section>
	{/if}
</div>

<style>
	/* One column; from 52rem two: Zustand and Dienste on the left, Zu
	   erledigen on the right, the last changes below over the full width. */
	.overview {
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		align-items: start;
		gap: var(--gap, var(--space-4));
		min-width: 0;
	}
	@media (min-width: 52rem) {
		.overview {
			grid-template-columns: repeat(2, minmax(0, 1fr));
			grid-template-rows: auto 1fr auto;
			grid-template-areas:
				'health todo'
				'services todo'
				'recent recent';
		}
		.health {
			grid-area: health;
		}
		.todo {
			grid-area: todo;
		}
		.services {
			grid-area: services;
		}
		.recent-card {
			grid-area: recent;
		}
	}
	.recent-card {
		container-type: inline-size;
	}
	/* A to-do: its text takes the room, the button stays on the right. */
	.todo .set-row {
		grid-template-columns: minmax(0, 1fr) auto;
	}
	.value {
		overflow-wrap: anywhere;
		font-weight: 500;
	}
	.card-text {
		margin: 0;
		padding: var(--space-3) var(--space-4);
		font-size: var(--text-base);
	}
	.card-text + .card-text {
		padding-top: 0;
	}
	.muted {
		color: var(--muted);
	}
	.done {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--text);
	}
	.mark {
		color: var(--ok-text);
		font-weight: 700;
	}
	a.chip {
		text-decoration: none;
		white-space: normal;
		overflow-wrap: anywhere;
	}
	.set-label .chip {
		white-space: normal;
	}
	.state {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		font-weight: 600;
	}
	.dot {
		width: 9px;
		height: 9px;
		border-radius: 50%;
		border: 1.5px solid var(--muted);
	}
	.state.on .dot {
		border-color: var(--ok-text);
		background: var(--ok-text);
	}
	.more {
		margin-left: auto;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0 var(--space-4);
	}
	.recent li {
		display: grid;
		grid-template-columns: minmax(7rem, 12rem) minmax(0, 1fr) auto auto;
		align-items: baseline;
		gap: var(--space-1) var(--space-3);
		padding: var(--space-2) 0;
		font-size: var(--text-base);
	}
	.recent li + li {
		border-top: 1px solid var(--panel-border, var(--border));
	}
	.recent .area {
		font-weight: 500;
		overflow-wrap: anywhere;
	}
	.what {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.who,
	time {
		color: var(--muted);
		white-space: nowrap;
	}
	@container (max-width: 34rem) {
		.recent li {
			grid-template-columns: minmax(0, 1fr) auto;
		}
		.recent .what {
			grid-column: 1;
			grid-row: 2;
		}
		.recent .who {
			grid-column: 2;
			grid-row: 2;
		}
	}
</style>
