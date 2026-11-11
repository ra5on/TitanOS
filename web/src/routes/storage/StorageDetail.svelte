<script lang="ts">
	// Beside the list: the chosen volume with what lives on it, or the chosen
	// disk with its facts, sleep time and actions.
	import ForesightPlay from '#lib/components/agent/features/ForesightPlay.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { revealTarget } from '#lib/reveal-target.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import DiskSleepSection from './DiskSleepSection.svelte';
	import { diskType } from './labels.ts';
	import type { StoragePage } from './storage-page.svelte.ts';
	import VolumeCard from './VolumeCard.svelte';

	let { sp }: { sp: StoragePage } = $props();
	const detail = $derived(sp.detail);
	const diskDetail = $derived(sp.diskDetail);
</script>

{#if sp.tab === 'volumes' && detail}
	<aside>
		<div class="detail-heading">
			<h2>{t('storage.details')}</h2>
			<button class="btn" aria-label={t('common.close')} onclick={() => (sp.selected = '')}
				><Icon name="close" size={16} /></button
			>
		</div>
		<!-- A new instance per volume: an open editor must not carry over. -->
		{#key detail.name}
			<VolumeCard
				volume={detail}
				canAdminister={sp.canAdminister}
				busy={sp.busy}
				oncheck={(start) => sp.check(detail, start)}
				onadd={() => (sp.dialog = { kind: 'add', volume: detail.name })}
				onprofile={() => (sp.dialog = { kind: 'profile', volume: detail.name })}
				diskSleep={sp.diskSleep}
				onsleepsaved={(updated) => {
					if (storage.status) storage.status.diskSleep = updated;
				}}
				onsleepopen={() => {
					sp.sleepFocus = { kind: 'volume', key: detail.name };
					sp.sleepOpen = true;
				}}
				ondelete={() => (sp.dialog = { kind: 'delete', volume: detail.name })}
				onremove={(disk) => (
					(sp.error = null),
					(sp.dialog = { kind: 'remove', volume: detail.name, disk })
				)}
			/>
		{/key}
		<!-- PLAN 78-5: what lives on the volume, as flat rows under a plain heading. -->
		{#if sp.canAdminister}{@const used = sp.usageFor(detail.name)}
			<div class="usage sfacts">
				<SettingGroup title={t('storage.onVolume')} id="volume-usage">
					{#if !used}<p class="muted loading" role="status">{t('common.loading')}</p>
					{:else}
						<SettingRow icon="folder" title={t('storage.onVolumeShares')}>
							{#snippet control()}<span
									>{#each used.shares as share, i (share)}{i ? ', ' : ''}<a
											class="mono"
											href="/shares?share={encodeURIComponent(share)}">{share}</a
										>{:else}<span class="muted">{t('storage.onVolumeNone')}</span>{/each}</span
								>{/snippet}
						</SettingRow>
						<SettingRow icon="monitor" title={t('storage.onVolumeVms')}>
							{#snippet control()}<span>{used.vms.join(', ') || t('storage.onVolumeNone')}</span
								>{/snippet}
						</SettingRow>
						<SettingRow icon="box" title={t('storage.onVolumeDocker')}>
							{#snippet control()}<span
									>{used.docker
										? used.docker.length
											? t('storage.onVolumeDockerApps', { apps: used.docker.join(', ') })
											: t('storage.onVolumeDockerEngine')
										: t('storage.onVolumeNone')}</span
								>{/snippet}
						</SettingRow>
						{#if used.failed}<div class="notice-warn usage-failed" role="alert">
								<span>{t('storage.onVolumeIncomplete')}</span><button
									class="btn"
									onclick={() => sp.loadUsage(detail.name)}>{t('common.retry')}</button
								>
							</div>{/if}
					{/if}
				</SettingGroup>
			</div>
		{/if}
	</aside>
{:else if sp.tab === 'disks' && diskDetail}
	<!-- PLAN 78-5: the disk's detail is no tile: its name, the facts as flat
	     rows, the sleep time, then its actions. -->
	<aside class="disk-details" use:revealTarget id="disk-{diskDetail.name}">
		<div class="detail-heading">
			<h2 class="mono">{diskDetail.name}</h2>
			<button class="btn" aria-label={t('common.close')} onclick={() => (sp.selectedDisk = '')}
				><Icon name="close" size={16} /></button
			>
		</div>
		<div class="sfacts">
			<SettingGroup>
				<SettingRow title={t('disk.device')}>
					{#snippet control()}<span>{diskDetail.model}</span>{/snippet}
				</SettingRow>
				<SettingRow title={t('disk.serial')}>
					{#snippet control()}<span class="mono">{diskDetail.serial || '–'}</span>{/snippet}
				</SettingRow>
				<SettingRow title={t('disk.type')}>
					{#snippet control()}<span>{diskType(diskDetail)}</span>{/snippet}
				</SettingRow>
				<SettingRow title={t('disk.hours')}>
					{#snippet control()}<span
							>{diskDetail.smart ? sp.hours.format(diskDetail.smart.powerOnHours) : '–'}</span
						>{/snippet}
				</SettingRow>
				<SettingRow title={t('disk.errors')}>
					{#snippet control()}<span>{diskDetail.smart?.errors ?? '–'}</span>{/snippet}
				</SettingRow>
				<SettingRow title={t('disk.wear')}>
					{#snippet control()}<span
							>{diskDetail.smart && diskDetail.smart.wearPercent >= 0
								? `${diskDetail.smart.wearPercent} %`
								: '–'}</span
						>{/snippet}
				</SettingRow>
			</SettingGroup>
		</div>
		<DiskSleepSection
			name={diskDetail.name}
			status={sp.diskSleep}
			canAdminister={sp.canAdminister}
			onsaved={(updated) => {
				if (storage.status) storage.status.diskSleep = updated;
			}}
			onopen={() => {
				sp.sleepFocus = { kind: 'disk', key: diskDetail.name };
				sp.sleepOpen = true;
			}}
		/>
		{#if sp.canAdminister}{@const disk = diskDetail}
			<div class="disk-actions">
				<a class="btn" href="/storage/smart?disk={encodeURIComponent(disk.name)}"
					>{t('smart.details')}</a
				>
				{#if disk.volume}
					<button
						class="btn"
						onclick={() => {
							sp.tab = 'volumes';
							sp.query = '';
							sp.filter = 'all';
							sp.selected = disk.volume ?? '';
						}}>{t('storage.openVolume', { volume: disk.volume })}</button
					>
					<ForesightPlay volume={sp.volumes.find((v) => v.name === disk.volume)} disk={disk.name} />
				{:else if disk.role === 'free'}
					<a class="btn" href="/tools/disks">{t('storage.testDisk')}</a>
					{#each sp.addTargets as volume (volume.name)}
						<button
							class="btn"
							disabled={sp.busy || disk.testing}
							onclick={() => (sp.dialog = { kind: 'add', volume: volume.name, disk })}
							>{t('storage.addToVolume', { volume: volume.name })}</button
						>
					{/each}
				{/if}
			</div>
		{/if}
	</aside>
{/if}

<style>
	.detail-heading {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		flex-wrap: wrap;
	}
	aside {
		min-width: 0;
	}
	.detail-heading {
		justify-content: space-between;
		margin-bottom: var(--space-3);
	}
	.detail-heading h2 {
		font-size: var(--text-lg);
	}
	.detail-heading .btn {
		padding: var(--space-1-5);
		min-height: 30px;
	}
	.disk-details {
		display: grid;
		gap: var(--space-4);
	}
	.disk-details .detail-heading {
		margin-bottom: 0;
	}
	.disk-actions,
	.usage-failed {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.usage {
		margin-top: var(--space-4);
	}
</style>
