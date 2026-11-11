<script lang="ts">
	// Plugins › NVIDIA: any version of NVIDIA's archive. The NAS builds it,
	// uses it after the next restart and rebuilds it after a kernel update
	// (plugins/driver_choice.go); until then the shipped driver runs.
	import SettingGroup from './settings/SettingGroup.svelte';
	import SettingRow from './settings/SettingRow.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { DriverStatus } from './DriverUpdates.svelte';
	let {
		status,
		busy,
		action,
	}: {
		status: DriverStatus;
		busy: boolean;
		action: (data: Record<string, unknown>) => void;
	} = $props();
	// svelte-ignore state_referenced_locally
	let picked = $state(status.chosen ?? '');
	const working = $derived(
		['checking', 'downloading', 'environment', 'building'].includes(status.state),
	);
	const failed = $derived(status.state === 'error' && !!status.target);
	const major = (version: string) => Number(version.split('.')[0]);
	// Older series than the shipped one often no longer build with new kernels.
	const old = (version: string) => !!status.bundled && major(version) < major(status.bundled);
	const ready = $derived(!!picked && picked === status.running && !!status.runningLocal);
</script>

<!-- PLAN 78-5: a plain heading, the version and its state as flat rows. -->
<div class="driver-choice">
	<SettingGroup title={t('plugins.choice.title')} id="driver-choice">
		{#if failed}
			<div class="notice-warn" role="alert">
				<p>
					{t('plugins.choice.failed', {
						version: status.target ?? '',
						bundled: status.bundled ?? '',
					})}
				</p>
				{#if status.error}<details>
						<summary>{t('plugins.errorDetails')}</summary>
						<pre>{status.error}</pre>
					</details>{/if}
			</div>
		{/if}
		<SettingRow
			icon="gpu"
			title={t('plugins.choice.version')}
			desc={status.running
				? t(status.runningLocal ? 'plugins.choice.runningLocal' : 'plugins.choice.runningBundled', {
						version: status.running,
					})
				: undefined}
			for="driver-version"
		>
			{#snippet control()}
				<select
					id="driver-version"
					class="field"
					bind:value={picked}
					disabled={busy || working || !!status.pending}
				>
					<option value="">{t('plugins.choice.bundled', { version: status.bundled ?? '' })}</option>
					{#each status.drivers ?? [] as version (version)}<option value={version}
							>{version}{version === status.running && status.runningLocal
								? ` · ${t('plugins.choice.built')}`
								: ''}{old(version) ? ` · ${t('plugins.choice.old')}` : ''}</option
						>{/each}
				</select>
				{#if status.pending}<button
						type="button"
						class="btn"
						disabled={busy}
						onclick={() => action({ action: 'select', sha256: 'cancel' })}
						>{t('plugins.driverCancel')}</button
					>{:else}<button
						type="button"
						class="btn btn-primary"
						disabled={busy || working || (picked === (status.chosen ?? '') && !failed)}
						onclick={() => action({ action: 'choose', version: picked })}
						>{t(picked ? 'plugins.choice.buildUse' : 'plugins.choice.use')}</button
					>{/if}
			{/snippet}
		</SettingRow>
		{#if working}
			<p role="status">
				{t(status.state === 'building' ? 'plugins.choice.building' : 'plugins.choice.downloading', {
					version: status.target || picked,
				})}
			</p>
			<progress aria-label={t(`plugins.driverState.${status.state}`)}></progress>
		{:else if status.pending}
			<p role="status" class="notice-warn">
				{t('plugins.driverPending', { version: status.pending.version })}
			</p>
		{:else if picked && !ready}
			<p class="muted">{t('plugins.choice.buildHint')}</p>
			{#if old(picked)}<p class="notice-warn">{t('plugins.choice.oldHint')}</p>{/if}
		{/if}
		<p class="muted">{t('plugins.choice.kernelHint')}</p>
	</SettingGroup>
</div>

<style>
	p {
		font-size: var(--text-base);
	}
	.notice-warn p {
		margin: 0;
	}
	progress {
		width: 100%;
		min-width: 0;
	}
	pre {
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
