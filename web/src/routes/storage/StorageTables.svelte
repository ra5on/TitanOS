<script lang="ts">
	// The list of the storage page: the volumes or the disks as a table, with
	// state, use and the sleep time of each row.
	import EmptyState from '#lib/components/EmptyState.svelte';
	import { formatDiskBytes, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { diskStatus, diskType, smartText } from './labels.ts';
	import {
		diskSleep as diskSleepOf,
		sleepDuration,
		sleepOrigin,
		sleepSince,
		volumeSleep,
	} from './sleep-view.ts';
	import type { StoragePage } from './storage-page.svelte.ts';

	let { sp }: { sp: StoragePage } = $props();
</script>

<div class="tile table">
	{#if !storage.status}
		{#if sp.statusError}<div class="notice-error table-state" role="alert">
				<span>{sp.statusError}</span><button
					class="btn"
					disabled={sp.statusLoading}
					onclick={sp.loadStatus}>{t('common.retry')}</button
				>
			</div>{:else}<p class="muted table-state" role="status">{t('common.loading')}</p>{/if}
	{:else if sp.tab === 'volumes'}
		<table>
			<thead
				><tr
					><th>{t('storage.volumes')}</th><th>{t('disk.status')}</th><th
						>RAID / {t('storage.filesystem')}</th
					><th>{t('storage.used')}</th><th class="num">{t('storage.disks')}</th><th
						>{t('storage.sleepColumn')}</th
					></tr
				></thead
			><tbody>
				{#each sp.visibleVolumes as volume (volume.name)}{@const sleep = volumeSleep(
						sp.diskSleep,
						volume.name,
					)}
					<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
					<tr
						class="clickable"
						class:selected={sp.selected === volume.name}
						onclick={() => (sp.selected = sp.selected === volume.name ? '' : volume.name)}
					>
						<td
							><button class="row-name mono" aria-expanded={sp.selected === volume.name}
								>{volume.name}<span class="chev" aria-hidden="true">›</span></button
							></td
						>
						<td
							><span class="chip" class:warn={!['ok'].includes(volume.state)}
								>{t(`state.${volume.state}`)}</span
							>{#if volume.sync}<div class="sync-inline">
									<progress max="100" value={volume.sync.percent}></progress><span class="mono"
										>{formatPercent(volume.sync.percent)}</span
									>
								</div>{/if}</td
						>
						<td
							><span>{t(`level.${volume.level}`)}</span><span class="sub"
								>{t(`fs.${volume.filesystem}`)}</span
							></td
						>
						<td
							>{#if volume.mounted && volume.sizeBytes > 0}<span class="mono"
									>{formatDiskBytes(volume.usedBytes)} / {formatDiskBytes(volume.sizeBytes)}</span
								>
								<div class="bar">
									<span style:width="{Math.min(100, (volume.usedBytes / volume.sizeBytes) * 100)}%"
									></span>
								</div>{:else}<span class="muted">{t('storage.notMounted')}</span>{/if}</td
						>
						<td class="num mono">{volume.members.length}</td>
						<td>
							{#if sleep}
								<button
									type="button"
									class="sleep-cell"
									title={sp.canAdminister ? t('storage.sleepOpenHint') : undefined}
									disabled={!sp.canAdminister}
									onclick={(event) => {
										event.stopPropagation();
										sp.sleepFocus = { kind: 'volume', key: volume.name };
										sp.sleepOpen = true;
									}}
								>
									{#if sleep.total > 0}
										<span>{sleepDuration(sleep.minutes)}</span>
										<span class="chip" class:ok={sleep.asleep === sleep.total}
											>{sleep.asleep === sleep.total
												? t('storage.sleepAsleep')
												: sleep.asleep > 0
													? t('storage.sleepPartly')
													: t('storage.sleepAwake')}</span
										>
										<span class="sub"
											>{t('storage.sleepCount', {
												asleep: sleep.asleep,
												total: sleep.total,
											})}</span
										>
									{:else}
										<span class="muted">–</span>
										<span class="sub">{t('storage.sleepNoSleeper')}</span>
									{/if}
								</button>
							{:else}<span class="muted">–</span>{/if}
						</td>
					</tr>
				{:else}<tr
						><td colspan="6"
							><EmptyState
								inline
								title={t(sp.volumes.length ? 'ui.noResults' : 'storage.noVolumes')}
							/></td
						></tr
					>{/each}
			</tbody>
		</table>
	{:else}
		<table>
			<thead
				><tr
					><th>{t('disk.device')}</th><th>{t('disk.status')}</th><th class="num"
						>{t('disk.size')}</th
					><th class="num">{t('disk.temp')}</th><th>{t('disk.smart')}</th><th>{t('disk.volume')}</th
					><th>{t('storage.sleepColumn')}</th></tr
				></thead
			><tbody>
				{#each sp.visibleDisks as disk (disk.identity || disk.serial || disk.name)}{@const status =
						diskStatus(disk)}{@const sleep = diskSleepOf(sp.diskSleep, disk.name)}
					<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
					<tr
						class="clickable"
						class:selected={sp.selectedDisk === (disk.identity || disk.serial || disk.name)}
						onclick={() =>
							(sp.selectedDisk =
								sp.selectedDisk === (disk.identity || disk.serial || disk.name)
									? ''
									: disk.identity || disk.serial || disk.name)}
					>
						<td
							><button
								class="row-name mono"
								aria-expanded={sp.selectedDisk === (disk.identity || disk.serial || disk.name)}
								>{disk.name}<span class="chev" aria-hidden="true">›</span></button
							><span class="sub">{disk.model || diskType(disk)}</span></td
						>
						<td><span class="status"><span class="dot {status.dot}"></span>{status.text}</span></td
						><td class="mono num">{formatDiskBytes(disk.sizeBytes)}</td><td class="mono num"
							>{disk.smart?.temperatureC ? `${disk.smart.temperatureC} °C` : '–'}</td
						><td class:bad={disk.smart?.healthy === false}>{smartText(disk)}</td><td class="mono"
							>{disk.volume ||
								(disk.role === 'free'
									? t('storage.unassigned')
									: disk.contents?.join(', ') || '–')}</td
						>
						<td>
							{#if sleep?.disk}
								<button
									type="button"
									class="sleep-cell"
									title={sp.canAdminister ? t('storage.sleepOpenHint') : undefined}
									disabled={!sp.canAdminister}
									onclick={(event) => {
										event.stopPropagation();
										sp.sleepFocus = { kind: 'disk', key: disk.name };
										sp.sleepOpen = true;
									}}
								>
									<span>{sleepDuration(sleep.disk.effective)}</span>
									<span class="chip" class:ok={sleep.disk.state === 'asleep'}
										>{sleep.disk.state === 'asleep'
											? t('storage.sleepAsleep')
											: t('storage.sleepAwake')}</span
									>
									<span class="sub">{sleepOrigin(sleep.disk.origin)}</span>
									{#if sleepSince(sleep.disk)}<span class="sub">{sleepSince(sleep.disk)}</span>{/if}
								</button>
							{:else if sleep?.reason}
								<span class="muted">–</span>
								<span class="sub">{t(`storage.sleepReason.${sleep.reason}`)}</span>
							{:else}<span class="muted">–</span>{/if}
						</td>
					</tr>
				{:else}<tr
						><td colspan="7"
							><EmptyState
								inline
								title={t(sp.disks.length ? 'ui.noResults' : 'storage.noDisks')}
							/></td
						></tr
					>{/each}
			</tbody>
		</table>
	{/if}
</div>

<style>
	.sync-inline {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		flex-wrap: wrap;
	}
	.table {
		min-width: 0;
		overflow-x: auto;
	}
	.table table {
		width: 100%;
	}
	td {
		padding: 14px var(--space-4);
	}
	tr.selected {
		background: color-mix(in srgb, var(--accent) 8%, transparent);
	}
	.row-name {
		border: 0;
		background: transparent;
		padding: 0;
		cursor: pointer;
		color: var(--accent);
		font-weight: 500;
		text-align: left;
		white-space: nowrap;
		text-decoration: underline;
		text-underline-offset: 3px;
		text-decoration-color: color-mix(in srgb, var(--accent) 45%, transparent);
	}
	.row-name:hover {
		text-decoration-color: currentColor;
	}
	.chev {
		margin-left: var(--space-1-5);
		text-decoration: none;
		display: inline-block;
	}
	tr.clickable {
		cursor: pointer;
	}
	tr.clickable:hover {
		background: color-mix(in srgb, var(--accent) 6%, transparent);
	}
	.sleep-cell {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1) var(--space-2);
		border: 0;
		background: transparent;
		padding: 0;
		color: var(--text);
		cursor: pointer;
		text-align: left;
		font: inherit;
	}
	.sleep-cell:hover:not(:disabled) > span:first-child {
		text-decoration: underline;
	}
	.sleep-cell:disabled {
		cursor: default;
		opacity: 1;
	}
	.sleep-cell .sub {
		flex-basis: 100%;
		margin-top: 0;
	}
	.sub {
		display: block;
		font-size: var(--text-sm);
		color: var(--muted);
		margin-top: var(--space-1);
	}
	.num {
		text-align: right;
		white-space: nowrap;
	}
	.bar {
		height: 5px;
		margin-top: 7px;
		min-width: 100px;
	}
	.sync-inline {
		font-size: var(--text-sm);
		margin-top: 7px;
		gap: var(--space-1-5);
	}
	.sync-inline progress {
		width: 70px;
		height: 6px;
		accent-color: var(--accent);
	}
	.table-state {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.table-state {
		justify-content: space-between;
		margin: var(--space-4);
	}
	.status {
		display: flex;
		align-items: center;
		gap: 7px;
	}
	.bad {
		color: var(--error-text);
	}
	@media (max-width: 760px) {
		td {
			padding: var(--space-3);
		}
	}
</style>
