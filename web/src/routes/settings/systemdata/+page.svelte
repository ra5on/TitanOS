<script lang="ts">
	// PLAN 79-8 (owner's draft Entwuerfe/79-systemdaten): where TitanOS keeps
	// its own data. One volume for everything, a hint about sleeping disks,
	// the backup of the settings, and one row per log and history with its
	// own volume. A choice is stored at once; the data follow in the background.
	import { onDestroy } from 'svelte';
	import { api } from '#lib/api.ts';
	import type { IconName } from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		offeredVolumes,
		sleepingChoices,
		sysDataHint,
		type SysDataFunction,
		type SysDataFunctionId,
		type SysDataState,
		type SysDataVolume,
	} from '#lib/sysdata.ts';

	type ConfigSettings = { enabled: boolean; encrypt?: boolean; pace?: string } & Record<
		string,
		unknown
	>;
	type ConfigBackup = {
		backup: {
			settings: ConfigSettings;
			target: string;
			lastError: string;
			backups: { time: string; sizeBytes: number }[];
		};
		passwordSet?: boolean;
	};

	let data = $state<SysDataState | null>(null);
	let config = $state<ConfigBackup | null>(null);
	let error = $state('');
	let busy = $state(false);
	let saved = $state(false);

	const ICONS: Record<SysDataFunctionId, IconName> = {
		config: 'refresh',
		logs: 'fileText',
		activity: 'activity',
		notify: 'bell',
		smart: 'shield',
		monitoring: 'monitor',
	};
	const LOGS: SysDataFunctionId[] = ['logs', 'activity', 'notify', 'smart', 'monitoring'];

	// While a folder is being moved the page asks again, so that sizes and
	// "wird verschoben" follow; never longer than two minutes.
	let timer: ReturnType<typeof setTimeout> | undefined;
	let polls = 0;
	function take(state: SysDataState) {
		data = state;
		clearTimeout(timer);
		const waiting = state.functions.some(
			(f) => f.moving || (f.volume !== '' && f.sizeBytes === null),
		);
		if (waiting && polls < 40) {
			polls++;
			timer = setTimeout(() => void refresh(), 3000);
		}
	}
	async function refresh() {
		try {
			take(await api.get<SysDataState>('/system/data'));
		} catch {
			/* the next visit asks again */
		}
	}
	onDestroy(() => clearTimeout(timer));

	async function load() {
		error = '';
		try {
			const [state, backup] = await Promise.all([
				api.get<SysDataState>('/system/data'),
				api.get<ConfigBackup>('/config-backup').catch(() => null),
			]);
			config = backup;
			take(state);
		} catch (err) {
			error = apiErrorText(err);
		}
	}
	$effect(() => {
		void load();
	});
	$effect(() => {
		if (saved) {
			const done = setTimeout(() => (saved = false), 5000);
			return () => clearTimeout(done);
		}
	});

	async function store(body: { volume?: string; functions?: Record<string, string> }) {
		busy = true;
		saved = false;
		error = '';
		try {
			polls = 0;
			take(await api.put<SysDataState>('/system/data', body));
			saved = true;
			// The copies of /config name their target themselves.
			if (body.volume !== undefined || body.functions?.config !== undefined)
				config = await api.get<ConfigBackup>('/config-backup').catch(() => config);
		} catch (err) {
			error = apiErrorText(err);
			await refresh();
		}
		busy = false;
	}

	async function setPace(pace: string) {
		if (!config) return;
		busy = true;
		saved = false;
		error = '';
		try {
			config = await api.put<ConfigBackup>('/config-backup', {
				...config.backup.settings,
				pace,
			});
			saved = true;
		} catch (err) {
			error = apiErrorText(err);
		}
		busy = false;
	}

	const hint = $derived(data ? sysDataHint(data) : { tone: 'none' as const });
	const asleep = $derived(data ? sleepingChoices(data) : []);
	const names = (list: string[]) => list.map((name) => `„${name}“`).join(', ');

	function volumeNote(volume: SysDataVolume): string {
		if (!volume.mounted) return t('sysdata.volume.unmounted');
		if (!data?.sleepKnown) return '';
		return t(volume.sleeps ? 'sysdata.volume.sleeps' : 'sysdata.volume.awake');
	}
	const volumeLabel = (volume: SysDataVolume) => {
		const note = volumeNote(volume);
		return note ? `${volume.name} · ${note}` : volume.name;
	};

	function describe(f: SysDataFunction): string {
		const parts = [t(`sysdata.fn.${f.id}.desc` as MessageKey)];
		if (!f.enabled) parts.push(t('sysdata.off'));
		else if (f.moving) parts.push(t('sysdata.moving'));
		else if (f.sizeBytes !== null) parts.push(formatBytes(f.sizeBytes));
		return parts.join(' · ');
	}

	const configFn = $derived(data?.functions.find((f) => f.id === 'config'));
	const configSummary = $derived.by(() => {
		if (!config) return t('sysdata.fn.config.desc');
		const { settings, backups, lastError } = config.backup;
		if (!settings.enabled) return t('sysdata.off');
		if (lastError) return t('sysdata.config.failed');
		if (!backups.length) return t('sysdata.config.none');
		const total = backups.reduce((sum, backup) => sum + backup.sizeBytes, 0);
		return [
			t('sysdata.config.last', { time: formatDateTime(new Date(backups[0].time)) }),
			t('sysdata.config.count', { count: backups.length }),
			formatBytes(total),
		].join(' · ');
	});
	const pace = $derived(config?.backup.settings.pace === 'daily' ? 'daily' : 'change');
</script>

<PageHead title={t('sysdata.title')}>
	<p class="muted">{t('sysdata.intro')}</p>
</PageHead>

{#if error}<div class="notice-error" role="alert">
		{error}{#if !data}<button class="btn" onclick={load}>{t('ui.retry')}</button>{/if}
	</div>{/if}
{#if !data && !error}<p class="loading" role="status">{t('common.loading')}</p>{/if}

{#snippet choice(f: SysDataFunction, state: SysDataState)}
	<select
		id="sysdata-fn-{f.id}"
		class="field"
		value={f.choice}
		disabled={busy}
		onchange={(e) => store({ functions: { [f.id]: e.currentTarget.value } })}
	>
		<option value=""
			>{state.resolved
				? t('sysdata.inherit', { volume: state.resolved })
				: t('sysdata.inheritNone')}</option
		>
		{#each offeredVolumes(state, f.choice) as volume (volume.name)}<option value={volume.name}
				>{volumeLabel(volume)}</option
			>{/each}
	</select>
{/snippet}

{#if data}
	{@const state = data}
	<div class="page">
		<SettingGroup id="sysdata-store" title={t('sysdata.group.store')}>
			{#snippet tools()}{#if saved}<span class="muted" role="status">{t('ui.saved')}</span
					>{/if}{/snippet}
			<SettingRow
				icon="disk"
				title={t('sysdata.common')}
				desc={t('sysdata.common.desc')}
				for="sysdata-common"
			>
				{#snippet control()}
					<select
						id="sysdata-common"
						class="field"
						value={state.volume}
						disabled={busy}
						onchange={(e) => store({ volume: e.currentTarget.value })}
					>
						<option value=""
							>{state.resolved && state.volume === ''
								? t('sysdata.automaticNow', { volume: state.resolved })
								: t('sysdata.automatic')}</option
						>
						{#each offeredVolumes(state, state.volume) as volume (volume.name)}<option
								value={volume.name}>{volumeLabel(volume)}</option
							>{/each}
					</select>
				{/snippet}
			</SettingRow>
			{#if state.volumes.length === 0 || (!state.resolved && state.volume === '')}
				<p class="notice-warn" role="status">{t('sysdata.noVolume')}</p>
			{:else if !state.resolved}
				<p class="notice-warn" role="status">
					{t('sysdata.unmounted', { volume: state.volume })}
				</p>
			{:else if hint.tone === 'ok'}
				<p class="notice-ok" role="status">
					{t('sysdata.hint.ok')}
					{#if hint.spared.length === 1}{t('sysdata.hint.spared', {
							volume: hint.spared[0],
						})}{:else if hint.spared.length > 1}{t('sysdata.hint.sparedMany', {
							volumes: names(hint.spared),
						})}{/if}
				</p>
			{:else if hint.tone === 'warn'}
				<p class="notice-warn" role="status">{t('sysdata.hint.warn', { volume: hint.volume })}</p>
			{/if}
			{#if asleep.length}
				<p class="notice-warn" role="status">
					{t('sysdata.hint.ownSleeps', {
						functions: asleep.map((f) => t(`sysdata.fn.${f.id}` as MessageKey)).join(', '),
					})}
				</p>
			{/if}
		</SettingGroup>

		{#if configFn}
			<SettingGroup id="sysdata-config" title={t('sysdata.group.config')}>
				<SettingRow
					id="config"
					icon={ICONS.config}
					title={t('sysdata.fn.config')}
					desc={configSummary}
					for="sysdata-fn-config"
				>
					{#snippet control()}{@render choice(configFn, state)}{/snippet}
					<SettingSub
						title={t('sysdata.config.when')}
						desc={t(pace === 'daily' ? 'sysdata.config.when.daily' : 'sysdata.config.when.change')}
						for="sysdata-pace"
					>
						{#snippet control()}
							<select
								id="sysdata-pace"
								class="field"
								value={pace}
								disabled={busy || !config}
								onchange={(e) => setPace(e.currentTarget.value)}
							>
								<option value="daily">{t('sysdata.config.pace.daily')}</option>
								<option value="change">{t('sysdata.config.pace.change')}</option>
							</select>
						{/snippet}
					</SettingSub>
					<SettingSub
						title={t('sysdata.config.password')}
						desc={t(
							config?.backup.settings.encrypt && config.passwordSet
								? 'sysdata.config.password.set'
								: 'sysdata.config.password.none',
						)}
					>
						{#snippet control()}<a
								class="btn"
								href="/settings/backups/configuration#config-schedule"
								>{t(
									config?.backup.settings.encrypt && config.passwordSet
										? 'sysdata.config.password.change'
										: 'sysdata.config.password.setUp',
								)}</a
							>{/snippet}
					</SettingSub>
					<SettingSub title={t('sysdata.config.copies')} desc={t('sysdata.config.copies.desc')}>
						{#snippet control()}<a
								class="btn"
								href="/settings/backups/configuration#config-downloads"
								>{t('sysdata.config.copies.show')}</a
							>{/snippet}
					</SettingSub>
					<SettingSub title={t('sysdata.config.more')} desc={t('sysdata.config.more.desc')}>
						{#snippet control()}<a class="btn" href="/settings/backups/configuration"
								>{t('sysdata.config.more.open')}</a
							>{/snippet}
					</SettingSub>
				</SettingRow>
			</SettingGroup>
		{/if}

		<SettingGroup id="sysdata-logs" title={t('sysdata.group.logs')}>
			{#each LOGS as id (id)}
				{@const f = state.functions.find((entry) => entry.id === id)}
				{#if f}
					<SettingRow
						icon={ICONS[id]}
						title={t(`sysdata.fn.${id}` as MessageKey)}
						desc={describe(f)}
						for="sysdata-fn-{id}"
					>
						{#snippet control()}{@render choice(f, state)}{/snippet}
					</SettingRow>
				{/if}
			{/each}
		</SettingGroup>
	</div>
{/if}

<style>
	.page {
		min-width: 0;
		font-size: 0.875rem;
	}
	/* A hint below the row keeps its distance from the next group. */
	.page :global(.sgroup > p) {
		margin: var(--space-1) 0 0;
	}
</style>
