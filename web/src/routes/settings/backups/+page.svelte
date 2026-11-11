<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import Rule321 from './Rule321.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime } from '#lib/format.ts';

	type Jobs = {
		jobs: {
			job: { id: string; enabled: boolean };
			stateKnown?: boolean;
			state: { lastOK: string | null; lastError: string; warning: string; statusError?: string };
			next: string | null;
		}[];
	};
	type Config = {
		backup: {
			settings: { enabled: boolean };
			target: string;
			lastError: string;
			backups: { time: string }[];
			next: string | null;
		};
	};
	let jobs = $state<Jobs | null>(null);
	let config = $state<Config | null>(null);
	let dataError = $state('');
	let configError = $state('');
	let loading = $state(false);
	async function load() {
		if (loading) return;
		loading = true;
		await Promise.all([
			api.get<Jobs>('/backups').then(
				(value) => {
					jobs = value;
					dataError = '';
				},
				(err) => {
					jobs = null;
					dataError = apiErrorText(err);
				},
			),
			api.get<Config>('/config-backup').then(
				(value) => {
					config = value;
					configError = '';
				},
				(err) => {
					config = null;
					configError = apiErrorText(err);
				},
			),
		]);
		loading = false;
	}
	// Opening/refreshing this page may list backup archives on a volume. Never poll it.
	onMount(() => {
		if (location.hash.startsWith('#backup-')) {
			void goto('/settings/backups/data' + location.hash, { replaceState: true });
			return;
		}
		void load();
	});
	const lastData = $derived(
		jobs?.jobs
			.map((v) => v.state.lastOK)
			.filter((v): v is string => !!v)
			.sort((a, b) => Date.parse(a) - Date.parse(b))
			.at(-1),
	);
	const lastConfig = $derived(
		config?.backup.backups
			.map((v) => v.time)
			.sort((a, b) => Date.parse(a) - Date.parse(b))
			.at(-1),
	);
	const next = $derived(
		[
			...(jobs?.jobs.filter((v) => v.job.enabled).map((v) => v.next) ?? []),
			...(config?.backup.settings.enabled ? [config.backup.next] : []),
		]
			.filter((v): v is string => !!v)
			.sort((a, b) => Date.parse(a) - Date.parse(b))[0],
	);
	const failures = $derived(
		jobs?.jobs.filter((v) => v.state.lastError && v.state.lastError !== 'cancelled') ?? [],
	);
	const unknown = $derived(
		jobs?.jobs.filter((v) => v.stateKnown === false || v.state.statusError) ?? [],
	);
	const warnings = $derived(jobs?.jobs.filter((v) => v.state.warning) ?? []);
	const date = (value: string | undefined) =>
		value ? formatDateTime(new Date(value)) : t('safeguard.none');

	// PLAN 67-1: the menu bar of the desktop; a new job is made on the jobs page.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	registerMenuCommands({
		'backup.new': () => void goto('/settings/backups/data?new=1'),
		'backup.reload': { run: load, enabled: () => !loading },
	});
</script>

<PageHead title={t('safeguard.title')}>
	<p class="muted">{t('safeguard.intro')}</p>
	{#snippet actions()}<button class="btn" disabled={loading} onclick={load}
			>{t(loading ? 'common.loading' : 'safeguard.refresh')}</button
		>{/snippet}
</PageHead>

<!-- PLAN 78-1: the state as rows; the value stands where a row has its control. -->
<div class="state" aria-busy={loading}>
	<SettingGroup title={t('safeguard.state')}>
		<SettingRow id="backup-last-data" icon="archive" title={t('safeguard.lastData')}>
			{#snippet control()}<strong
					>{unknown.length
						? t('safeguard.unknown')
						: jobs
							? date(lastData)
							: t(loading ? 'common.loading' : 'safeguard.unknown')}</strong
				>{/snippet}
		</SettingRow>
		<SettingRow id="backup-last-config" icon="gear" title={t('safeguard.lastConfig')}>
			{#snippet control()}<strong
					>{config ? date(lastConfig) : t(loading ? 'common.loading' : 'safeguard.unknown')}</strong
				>{/snippet}
		</SettingRow>
		<SettingRow id="backup-next" icon="clock" title={t('safeguard.next')}>
			{#snippet control()}<strong
					>{!jobs || !config
						? t(loading ? 'common.loading' : 'safeguard.unknown')
						: next
							? formatDateTime(new Date(next))
							: t('safeguard.noSchedule')}</strong
				>{/snippet}
		</SettingRow>
	</SettingGroup>
</div>
{#if dataError || configError}<div class="notice-error" role="alert">
		{#if dataError}<p>{t('safeguard.data')}: {dataError}</p>{/if}
		{#if configError}<p>{t('safeguard.config')}: {configError}</p>{/if}
		<button class="btn" disabled={loading} onclick={load}>{t('ui.retry')}</button>
	</div>{/if}
{#if failures.length}<div class="notice-error" role="alert">
		<p>{t('safeguard.dataErrors', { count: failures.length })}</p>
		{#each failures as view}<a
				href="/settings/backups/data#backup-{encodeURIComponent(view.job.id)}">{view.job.id}</a
			>{/each}
	</div>{/if}
{#each unknown as view}<p class="notice-warn">
		<a href="/settings/backups/data#backup-{encodeURIComponent(view.job.id)}">{view.job.id}</a>: {t(
			'coverage.stateUnknown',
		)}
	</p>{/each}
{#each warnings as view}<p class="notice-warn">
		<a href="/settings/backups/data#backup-{encodeURIComponent(view.job.id)}">{view.job.id}</a>: {view
			.state.warning}
	</p>{/each}
{#if config?.backup.lastError}<p class="notice-error" role="alert">
		<a href="/settings/backups/configuration">{t('safeguard.config')}</a>: {config.backup.lastError}
	</p>{/if}
{#if config && !config.backup.settings.enabled}<p class="notice-warn">{t('safeguard.configOff')}</p>
{:else if config && !config.backup.target}<p class="notice-warn">{t('stick.noVolume')}</p>{/if}

<Rule321 jobs={jobs?.jobs} />
<!-- Every part of the program as a row that leads there. -->
<SettingGroup id="backup-data" title={t('safeguard.data')} desc={t('safeguard.dataHint')}>
	{#snippet tools()}<a class="btn" href="/settings/backups/data">{t('safeguard.manage')}</a
		>{/snippet}
	<SettingRow icon="list" title={t('safeguard.jobs')} href="/settings/backups/data" />
	<SettingRow
		icon="disk"
		title={t('safeguard.targets')}
		href="/settings/backups/data#backup-targets"
	/>
	<SettingRow
		icon="activity"
		title={t('safeguard.history')}
		href="/settings/backups/data#backup-history"
	/>
	<SettingRow icon="shield" title={t('coverage.title')} href="/settings/backups/coverage" />
	<SettingRow icon="box" title={t('applications.title')} href="/settings/backups/applications" />
</SettingGroup>
<SettingGroup id="backup-config" title={t('safeguard.config')} desc={t('safeguard.configHint')}>
	{#snippet tools()}<a class="btn" href="/settings/backups/configuration">{t('stick.title')}</a
		>{/snippet}
	<SettingRow
		icon="clock"
		title={t('safeguard.automatic')}
		href="/settings/backups/configuration#config-schedule"
	/>
	<SettingRow
		icon="download"
		title={t('safeguard.download')}
		href="/settings/backups/configuration#config-downloads"
	/>
	<SettingRow
		icon="activity"
		title={t('safeguard.history')}
		href="/settings/backups/configuration#config-history"
	/>
</SettingGroup>
<SettingGroup id="backup-restore" title={t('safeguard.restore')} desc={t('safeguard.recoveryHint')}>
	{#snippet tools()}<a class="btn" href="/settings/backups/restore">{t('safeguard.openRestore')}</a
		>{/snippet}
	<SettingRow
		icon="folder"
		title={t('safeguard.filesRestore')}
		href="/settings/backups/restore#restore-files"
	/>
	<SettingRow
		icon="undo"
		title={t('safeguard.configRestore')}
		href="/settings/backups/restore#restore-config"
	/>
	<SettingRow
		icon="key"
		title={t('stick.replace')}
		href="/settings/backups/restore#stick-replace"
	/>
</SettingGroup>

<style>
	.state {
		display: contents;
	}
	strong {
		color: var(--text);
		font-weight: 500;
	}
	p {
		margin: 0;
	}
	.notice-error a {
		margin-right: var(--space-3);
	}
</style>
