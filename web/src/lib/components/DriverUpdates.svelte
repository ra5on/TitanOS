<script lang="ts">
	import SettingGroup from './settings/SettingGroup.svelte';
	import SettingRow from './settings/SettingRow.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { formatBytes } from '#lib/format.ts';
	export type DriverStatus = {
		state:
			| 'idle'
			| 'checking'
			| 'downloading'
			| 'ready'
			| 'current'
			| 'unavailable'
			| 'error'
			| 'environment'
			| 'sourceReady'
			| 'building';
		automatic: boolean;
		error?: string;
		checked?: string;
		pending?: { version: string };
		canRestore: boolean;
		/** All NVIDIA versions of the last check, newest first; empty with an older build environment. */
		drivers?: string[];
		/** The chosen version ('' the shipped one), the shipped and the running driver. */
		chosen?: string;
		bundled?: string;
		running?: string;
		runningLocal?: boolean;
		/** The version being built, or whose build failed last. */
		target?: string;
		source?: { driver: string; toolkit: string; downloaded: boolean };
		versions:
			| {
					version: string;
					sha256: string;
					size: number;
					downloaded: boolean;
					recommended?: boolean;
					revision: number;
					local?: boolean;
			  }[]
			| null;
	};
	let {
		status,
		busy,
		action,
	}: { status: DriverStatus; busy: boolean; action: (data: Record<string, unknown>) => void } =
		$props();
	let selected = $state('bundled');
	const versions = $derived(status.versions ?? []);
	const choice = $derived(versions.find((v) => v.sha256 === selected));
	const loading = $derived(
		['checking', 'downloading', 'environment', 'building'].includes(status.state),
	);
</script>

<!-- PLAN 78-5: a plain heading, each setting of the driver a flat row. -->
<div class="driver-updates">
	<SettingGroup title={t('plugins.driverTitle')} id="driver-updates" desc={t('plugins.driverHint')}>
		<SettingRow icon="refresh" title={t('plugins.driverAutomatic')} for="driver-automatic">
			{#snippet control()}
				<input
					id="driver-automatic"
					class="switch"
					type="checkbox"
					role="switch"
					checked={status.automatic}
					disabled={busy}
					onchange={(e) => action({ action: 'automatic', automatic: e.currentTarget.checked })}
				/>
			{/snippet}
		</SettingRow>
		<SettingRow icon="search" title={t('plugins.driverCheck')}>
			{#snippet description()}<span role="status">{t(`plugins.driverState.${status.state}`)}</span
				>{/snippet}
			{#snippet control()}
				<button
					class="btn"
					type="button"
					disabled={busy || loading}
					onclick={() => action({ action: 'check' })}
					>{t(loading ? 'plugins.driverWorking' : 'plugins.driverCheck')}</button
				>
			{/snippet}
		</SettingRow>
		{#if status.source}
			<SettingRow
				icon="tool"
				title={t('plugins.driverSource', {
					driver: status.source.driver,
					toolkit: status.source.toolkit,
				})}
				desc={t('plugins.driverBuildHint')}
			>
				{#snippet control()}
					<button
						class="btn"
						type="button"
						disabled={busy || loading || !!status.pending || !status.source?.downloaded}
						onclick={() => action({ action: 'prepare' })}>{t('plugins.driverPrepare')}</button
					>
				{/snippet}
			</SettingRow>
		{:else}
			<p class="muted">{t('plugins.driverBuildHint')}</p>
		{/if}
		<SettingRow icon="gpu" title={t('plugins.driverVersion')} for="driver-version">
			{#snippet control()}
				<select
					id="driver-version"
					class="field"
					bind:value={selected}
					disabled={busy || !!status.pending}
				>
					<option value="bundled">{t('plugins.driverBundled')}</option>
					{#each versions as version (version.sha256)}<option value={version.sha256}
							>{version.version}{version.local
								? ` · ${t('plugins.driverLocal')}`
								: ''}{version.recommended ? ` · ${t('plugins.driverRecommended')}` : ''} · {formatBytes(
								version.size,
							)}{version.downloaded ? ` · ${t('plugins.driverDownloaded')}` : ''}</option
						>{/each}
				</select>
				{#if status.pending}
					<button
						type="button"
						class="btn"
						disabled={busy}
						onclick={() => action({ action: 'select', sha256: 'cancel' })}
						>{t('plugins.driverCancel')}</button
					>
				{:else if choice && !choice.downloaded}
					<button
						type="button"
						class="btn"
						disabled={busy || loading}
						onclick={() => action({ action: 'check', sha256: selected })}
						>{t('plugins.driverDownload')}</button
					>
				{:else}
					<button
						type="button"
						class="btn btn-primary"
						disabled={busy || (selected === 'bundled' ? !status.canRestore : !choice?.downloaded)}
						onclick={() => action({ action: 'select', sha256: selected })}
						>{t('plugins.driverSelect')}</button
					>
				{/if}
			{/snippet}
		</SettingRow>
		{#if status.pending}
			<p role="status" class="notice-warn">
				{t('plugins.driverPending', { version: status.pending.version })}
			</p>
		{/if}
		{#if status.error}<details class="notice-warn">
				<summary>{t('plugins.errorDetails')}</summary>
				<pre>{status.error}</pre>
			</details>{/if}
	</SettingGroup>
</div>

<style>
	p {
		font-size: var(--text-base);
	}
	pre {
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
