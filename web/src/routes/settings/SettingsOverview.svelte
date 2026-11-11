<script lang="ts">
	// PLAN 71 (owner's decision "Variante C", 7 Nov 2026): Systemsteuerung ›
	// Übersicht. The side bar of the window lists every area; this page only
	// shows how the NAS is doing, what is still open, which services run and
	// what was changed last. Only real answers of the NAS; what an account may
	// not read is not asked for.
	// PLAN 78-1: no cards any more – every block is a plain heading above its
	// rows (value at the right); a row that leads somewhere is a link row.
	import { onMount } from 'svelte';
	import { api, type VolumeState } from '#lib/api.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { activityLabel, type ActivityEntry, type ActivityPage } from '#lib/activity.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime, formatDuration, osVersion } from '#lib/format.ts';
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

{#snippet onOff(on: boolean)}
	<span class="state" class:on
		><span class="dot" aria-hidden="true"></span>{on
			? t('settingshome.services.on')
			: t('settingshome.services.off')}</span
	>
{/snippet}

<div class="overview">
	<div class="health">
		<SettingGroup title={t('settingshome.health')} id="{prefix}-health">
			{#if info}
				<SettingRow icon="home" title={t('settingshome.health.host')}>
					{#snippet control()}<span class="value">{info.hostname}</span>{/snippet}
				</SettingRow>
				<SettingRow icon="info" title={t('settingshome.health.version')}>
					{#snippet control()}<span class="value">{osVersion(info.version)}</span>{/snippet}
				</SettingRow>
				<SettingRow icon="clock" title={t('settingshome.health.uptime')}>
					{#snippet control()}<span class="value">{formatDuration(info.uptimeSeconds)}</span
						>{/snippet}
				</SettingRow>
			{:else}
				<p class="note muted" role="status">{t('settingshome.health.loading')}</p>
			{/if}
			{#if admin && volumes}
				<SettingRow
					icon="disk"
					title={t('settingshome.health.volumes')}
					desc={volumes.length
						? volumes.every((v) => v.state === 'ok')
							? t('settingshome.health.allOk')
							: t('settingshome.health.attention')
						: undefined}
				>
					{#snippet control()}
						{#each volumes as volume (volume.name)}
							<a class="chip {tone(volume.state)}" href="/storage"
								>{volume.name}: {t(`state.${volume.state}` as MessageKey)}</a
							>
						{:else}
							<a class="chip warn" href="/storage">{t('settingshome.health.noVolume')}</a>
						{/each}
					{/snippet}
				</SettingRow>
			{/if}
		</SettingGroup>
	</div>

	<div class="todo">
		<SettingGroup title={t('settingshome.todo')} id="{prefix}-todo">
			{#each states as [area, state] (area)}
				<SettingRow href="/settings/{area}" icon="bell" title={areaLabel(area)}>
					{#snippet control()}<span class="chip {state.tone}">{t(state.text)}</span>{/snippet}
				</SettingRow>
			{/each}
			{#each open as todo (todo.key)}
				<SettingRow
					href={todo.href}
					icon="list"
					title={t(todo.key)}
					desc={t(`${todo.key}Text` as MessageKey)}
				/>
			{/each}
			{#if setup === null}
				<p class="note muted" role="status">{t('settingshome.todo.checking')}</p>
			{:else if !states.length && !open.length}
				<div role="status"><SettingRow icon="check" title={t('settingshome.todo.none')} /></div>
			{/if}
			{#if setupFailed}<p class="note muted">{t('settingshome.todo.unknown')}</p>{/if}
		</SettingGroup>
	</div>

	{#if admin}
		<div class="services">
			<SettingGroup title={t('settingshome.services')} id="{prefix}-services">
				{#snippet tools()}
					<a class="btn" href="/settings/services">{t('settingshome.services.manage')}</a>
				{/snippet}
				{#each NAMES as name (name)}
					{@const s = services?.[name]}
					{#if s && typeof s.enabled === 'boolean'}
						<SettingRow icon={name === 'vms' ? 'monitor' : 'box'} title={t(`services.${name}`)}>
							{#snippet control()}
								{#if !s.available}
									<span class="state">{t('settingshome.services.notPossible')}</span>
								{:else}
									{@render onOff(s.enabled)}
									{#if s.enabled && s.total}<span class="muted"
											>{t('services.counts', { running: s.running, total: s.total })}</span
										>{/if}
								{/if}
							{/snippet}
						</SettingRow>
					{/if}
				{/each}
				{#if accounts}
					{@const locked = accounts.filter((a) => a.disabled).length}
					<SettingRow
						href="/settings/users"
						icon="users"
						title={t('desktop.app.users')}
						summary={t('settingshome.users.count', { n: accounts.length }) +
							(locked ? ' · ' + t('settingshome.users.locked', { n: locked }) : '')}
					/>
				{/if}
				{#if ssh}
					<SettingRow href="/settings/access#access-ssh" icon="terminal" title={t('access.ssh')}>
						{#snippet control()}{@render onOff(ssh?.enabled ?? false)}{/snippet}
					</SettingRow>
				{/if}
				{#if firewall}
					<SettingRow href="/settings/firewall" icon="shield" title={t('firewall.title')}>
						{#snippet control()}{@render onOff(firewall?.config.enabled ?? false)}{/snippet}
					</SettingRow>
				{/if}
				{#if services === null}
					<p class="note muted" role="status">{t('settingshome.health.loading')}</p>
				{/if}
			</SettingGroup>
		</div>
	{/if}

	{#if admin && recent.length}
		<div class="recent-card">
			<SettingGroup title={t('settingshome.recent')} id="{prefix}-recent">
				{#snippet tools()}
					<a class="btn" href="/tools/activity">{t('settingshome.recentAll')}</a>
				{/snippet}
				<ul class="recent">
					{#each recent as entry, index (index)}
						<li>
							<SettingRow
								href="/settings/{entry.area}"
								icon="edit"
								title={areaLabel(entry.area)}
								desc={describe(entry)}
							>
								{#snippet control()}
									{#if entry.user}<span class="who"
											>{t('settingshome.recentBy', { user: entry.user })}</span
										>{/if}
									<time datetime={entry.at}>{formatDateTime(new Date(entry.at))}</time>
								{/snippet}
							</SettingRow>
						</li>
					{/each}
				</ul>
			</SettingGroup>
		</div>
	{/if}
</div>

<style>
	/* One column of groups, as on every page of the control panel. */
	.overview {
		display: flex;
		flex-direction: column;
		gap: var(--gap, var(--space-4));
		min-width: 0;
	}
	.overview > div {
		min-width: 0;
	}
	.value {
		color: var(--text);
		overflow-wrap: anywhere;
		font-weight: 500;
	}
	.note {
		margin: 0;
		font-size: var(--text-base);
	}
	.muted {
		color: var(--muted);
	}
	a.chip {
		text-decoration: none;
		white-space: normal;
		overflow-wrap: anywhere;
	}
	.chip {
		white-space: normal;
	}
	.state {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		color: var(--text);
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
	ul {
		display: flex;
		flex-direction: column;
		gap: 3px;
		list-style: none;
		margin: 0;
		padding: 0;
	}
	/* A long account name wraps instead of pushing the row out. */
	.who {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	time {
		white-space: nowrap;
	}
</style>
