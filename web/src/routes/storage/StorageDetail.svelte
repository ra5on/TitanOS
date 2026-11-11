<script lang="ts">
	// Beside the list: the chosen volume with what lives on it, or the chosen
	// disk with its facts, sleep time and actions.
	import ForesightPlay from '#lib/components/agent/features/ForesightPlay.svelte';
	import Icon from '#lib/components/Icon.svelte';
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
		{#if sp.canAdminister}{@const used = sp.usageFor(detail.name)}
			<section class="tile usage" aria-labelledby="volume-usage">
				<h3 id="volume-usage">{t('storage.onVolume')}</h3>
				{#if !used}<p class="muted loading" role="status">{t('common.loading')}</p>
				{:else}
					<dl>
						<div>
							<dt>{t('storage.onVolumeShares')}</dt>
							<dd>
								{#each used.shares as share, i (share)}{i ? ', ' : ''}<a
										class="mono"
										href="/shares?share={encodeURIComponent(share)}">{share}</a
									>{:else}<span class="muted">{t('storage.onVolumeNone')}</span>{/each}
							</dd>
						</div>
						<div>
							<dt>{t('storage.onVolumeVms')}</dt>
							<dd>
								{used.vms.join(', ') || t('storage.onVolumeNone')}
							</dd>
						</div>
						<div>
							<dt>{t('storage.onVolumeDocker')}</dt>
							<dd>
								{used.docker
									? used.docker.length
										? t('storage.onVolumeDockerApps', { apps: used.docker.join(', ') })
										: t('storage.onVolumeDockerEngine')
									: t('storage.onVolumeNone')}
							</dd>
						</div>
					</dl>
					{#if used.failed}<div class="notice-warn usage-failed" role="alert">
							<span>{t('storage.onVolumeIncomplete')}</span><button
								class="btn"
								onclick={() => sp.loadUsage(detail.name)}>{t('common.retry')}</button
							>
						</div>{/if}
				{/if}
			</section>
		{/if}
	</aside>
{:else if sp.tab === 'disks' && diskDetail}
	<aside class="tile disk-details" use:revealTarget id="disk-{diskDetail.name}">
		<div class="detail-heading">
			<h2 class="mono">{diskDetail.name}</h2>
			<button class="btn" aria-label={t('common.close')} onclick={() => (sp.selectedDisk = '')}
				><Icon name="close" size={16} /></button
			>
		</div>
		<dl>
			<div>
				<dt>{t('disk.device')}</dt>
				<dd>{diskDetail.model}</dd>
			</div>
			<div>
				<dt>{t('disk.serial')}</dt>
				<dd class="mono">{diskDetail.serial || '–'}</dd>
			</div>
			<div>
				<dt>{t('disk.type')}</dt>
				<dd>{diskType(diskDetail)}</dd>
			</div>
			<div>
				<dt>{t('disk.hours')}</dt>
				<dd>{diskDetail.smart ? sp.hours.format(diskDetail.smart.powerOnHours) : '–'}</dd>
			</div>
			<div>
				<dt>{t('disk.errors')}</dt>
				<dd>{diskDetail.smart?.errors ?? '–'}</dd>
			</div>
			<div>
				<dt>{t('disk.wear')}</dt>
				<dd>
					{diskDetail.smart && diskDetail.smart.wearPercent >= 0
						? `${diskDetail.smart.wearPercent} %`
						: '–'}
				</dd>
			</div>
		</dl>
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
		padding: 20px;
	}
	.disk-actions,
	.usage-failed {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.disk-actions {
		margin-top: 18px;
	}
	.usage {
		margin-top: var(--space-4);
		padding: var(--space-4) 20px;
		display: grid;
		gap: var(--space-3);
		font-size: var(--text-md);
	}
	.usage h3 {
		margin: 0;
		font-size: var(--text-md);
	}
	.usage dl {
		display: grid;
		gap: var(--space-2-5);
		margin: 0;
	}
	.usage dt {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.usage dd {
		margin: 2px 0 0;
		overflow-wrap: anywhere;
	}
	.disk-details dl {
		display: grid;
		gap: var(--space-4);
		margin: 0;
	}
	.disk-details dl > div {
		display: grid;
		gap: var(--space-1);
	}
	.disk-details dt {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.disk-details dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
</style>
