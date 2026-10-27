<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
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
					dataError = errorText(err instanceof ApiError ? err.code : 'internal');
				},
			),
			api.get<Config>('/config-backup').then(
				(value) => {
					config = value;
					configError = '';
				},
				(err) => {
					config = null;
					configError = errorText(err instanceof ApiError ? err.code : 'internal');
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
</script>

<PageHead title={t('safeguard.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('safeguard.intro')}</p>
	{#snippet actions()}<button class="btn" disabled={loading} onclick={load}
			>{t(loading ? 'common.loading' : 'safeguard.refresh')}</button
		>{/snippet}
</PageHead>

<div class="tile summary" aria-busy={loading}>
	<div>
		<span>{t('safeguard.lastData')}</span><strong
			>{unknown.length
				? t('safeguard.unknown')
				: jobs
					? date(lastData)
					: t(loading ? 'common.loading' : 'safeguard.unknown')}</strong
		>
	</div>
	<div>
		<span>{t('safeguard.lastConfig')}</span><strong
			>{config ? date(lastConfig) : t(loading ? 'common.loading' : 'safeguard.unknown')}</strong
		>
	</div>
	<div>
		<span>{t('safeguard.next')}</span><strong
			>{!jobs || !config
				? t(loading ? 'common.loading' : 'safeguard.unknown')
				: next
					? formatDateTime(new Date(next))
					: t('safeguard.noSchedule')}</strong
		>
	</div>
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

<div class="actions">
	<a class="btn" href="/settings/backups/coverage">{t('coverage.title')}</a><a
		class="btn"
		href="/settings/backups/applications">{t('applications.title')}</a
	>
</div>
<div class="groups">
	<section class="tile panel">
		<h2 class="tile-title">{t('safeguard.data')}</h2>
		<p class="muted">{t('safeguard.dataHint')}</p>
		<nav aria-label={t('safeguard.data')}>
			<a href="/settings/backups/data">{t('safeguard.jobs')} <span aria-hidden="true">›</span></a><a
				href="/settings/backups/data#backup-targets"
				>{t('safeguard.targets')} <span aria-hidden="true">›</span></a
			><a href="/settings/backups/data#backup-history"
				>{t('safeguard.history')} <span aria-hidden="true">›</span></a
			>
		</nav>
		<a class="btn" href="/settings/backups/data">{t('safeguard.manage')}</a>
	</section>
	<section class="tile panel">
		<h2 class="tile-title">{t('safeguard.config')}</h2>
		<p class="muted">{t('safeguard.configHint')}</p>
		<nav aria-label={t('safeguard.config')}>
			<a href="/settings/backups/configuration#config-schedule"
				>{t('safeguard.automatic')} <span aria-hidden="true">›</span></a
			><a href="/settings/backups/configuration#config-downloads"
				>{t('safeguard.download')} <span aria-hidden="true">›</span></a
			><a href="/settings/backups/configuration#config-history"
				>{t('safeguard.history')} <span aria-hidden="true">›</span></a
			>
		</nav>
		<a class="btn" href="/settings/backups/configuration">{t('stick.title')}</a>
	</section>
	<section class="tile panel">
		<h2 class="tile-title">{t('safeguard.restore')}</h2>
		<p class="muted">{t('safeguard.recoveryHint')}</p>
		<nav aria-label={t('safeguard.restore')}>
			<a href="/settings/backups/restore#restore-files"
				>{t('safeguard.filesRestore')} <span aria-hidden="true">›</span></a
			><a href="/settings/backups/restore#restore-config"
				>{t('safeguard.configRestore')} <span aria-hidden="true">›</span></a
			><a href="/settings/backups/restore#stick-replace"
				>{t('stick.replace')} <span aria-hidden="true">›</span></a
			>
		</nav>
		<a class="btn" href="/settings/backups/restore">{t('safeguard.openRestore')}</a>
	</section>
</div>

<style>
	.summary,
	.groups {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 20px;
	}
	.summary {
		padding: calc(var(--tile-pad) + 2px);
	}
	.summary > div {
		display: grid;
		gap: 6px;
	}
	.summary span {
		color: var(--muted);
		font-size: 0.8125rem;
	}
	strong {
		font-weight: 500;
	}
	p {
		margin: 0;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
	}
	section > .btn {
		align-self: flex-start;
	}
	nav {
		display: grid;
		margin-top: auto;
	}
	nav a {
		display: flex;
		justify-content: space-between;
		gap: 12px;
		align-items: center;
		padding: 12px 0;
		border-top: 1px solid var(--panel-border);
		color: var(--text);
		text-decoration: none;
		min-height: 44px;
	}
	nav a:hover {
		color: var(--accent);
	}
	.notice-error a {
		margin-right: 12px;
	}
	@media (max-width: 900px) {
		.summary,
		.groups {
			grid-template-columns: 1fr;
		}
	}
</style>
