<script lang="ts">
	import { revealTarget } from '#lib/reveal-target.ts';
	import type { Volume } from '#lib/api.ts';
	import Snapshots from './Snapshots.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import HelpTip from '#lib/components/HelpTip.svelte';
	import { formatDiskBytes, formatDuration, formatPercent } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { stateTone } from './labels.ts';
	import { api } from '#lib/api.ts';
	import { canShrink, takesDisk } from './btrfs.ts';
	import CachePanel from './CachePanel.svelte';
	import EncryptionPanel from './EncryptionPanel.svelte';
	import { isLocked, lockedBlocks } from './crypt.ts';
	import { refreshStorage } from '#lib/storage.svelte.ts';

	type Props = {
		volume: Volume;
		canAdminister: boolean;
		busy: boolean;
		oncheck: (start: boolean) => void;
		onadd: () => void;
		ondelete: () => void;
		onremove: (disk: string) => void;
		onprofile: () => void;
	};
	let { volume, canAdminister, busy, oncheck, onadd, ondelete, onremove, onprofile }: Props =
		$props();

	const tone = $derived(stateTone(volume));
	let quotaBusy = $state(false),
		quotaError = $state('');
	async function switchQuotas(enabled: boolean) {
		quotaBusy = true;
		quotaError = '';
		try {
			await api.put(`/storage/volumes/${encodeURIComponent(volume.name)}/quotas`, { enabled });
			await refreshStorage();
		} catch (err) {
			quotaError = apiErrorText(err);
		}
		quotaBusy = false;
	}
	const usedPercent = $derived(
		volume.sizeBytes > 0 ? (100 * volume.usedBytes) / volume.sizeBytes : 0,
	);
	const rebuilding = $derived(
		volume.members.filter((member) => member.state === 'rebuilding').map((member) => member.disk),
	);

	const syncTitle = $derived.by(() => {
		if (volume.state === 'rebuilding')
			return t('sync.rebuilding', { disks: rebuilding.join(', ') || '…' });
		return t(volume.state === 'checking' ? 'sync.checking' : 'sync.syncing');
	});
	const syncNote = $derived(
		t(
			volume.state === 'rebuilding'
				? 'sync.rebuildingNote'
				: volume.state === 'checking'
					? 'sync.checkingNote'
					: 'sync.syncingNote',
		),
	);
	const syncRate = $derived.by(() => {
		if (!volume.sync) return '';
		const parts = [t('sync.speed', { speed: formatDiskBytes(volume.sync.speedBytesPerSecond) })];
		const left = volume.sync.remainingSeconds;
		if (left >= 60) parts.push(t('sync.remaining', { time: formatDuration(left) }));
		else if (left >= 0) parts.push(t('sync.almostDone'));
		return parts.join(' · ');
	});

	const problem = $derived(
		volume.state === 'degraded'
			? t('storage.degradedNote')
			: volume.state === 'failed'
				? t('storage.failedNote')
				: volume.state === 'missing'
					? t('storage.missingNote')
					: '',
	);
	// A locked volume waits for its password: nothing that needs it mounted.
	const locked = $derived(isLocked(volume));
	const canCheck = $derived(
		volume.level !== 'raid0' &&
			(volume.state === 'ok' || volume.state === 'checking') &&
			!lockedBlocks(volume),
	);
	const canAdd = $derived(takesDisk(volume) && !lockedBlocks(volume));
	const shrinks = $derived(canShrink(volume));
</script>

<article
	use:revealTarget
	id="volume-{volume.name}"
	class="tile"
	class:warn={tone === 'warn'}
	class:error={tone === 'error'}
	aria-label={volume.name}
>
	<div class="head">
		<span class="mono name">{volume.name}</span>
		<span class="chip">{t(`level.${volume.level}`)}</span><HelpTip term="raid" />
		<span class="chip">{t(`fs.${volume.filesystem}`)}</span>
		<span class="chip {tone}">{t(`state.${volume.state}`)}</span>
		{#if volume.encryption}<span class="chip" class:warn={locked}
				>{t(locked ? 'crypt.chipLocked' : 'crypt.chip')}</span
			>{/if}
		<span class="mono mount"
			>{volume.mounted
				? volume.mountPoint
				: t(locked ? 'crypt.waiting' : 'storage.notMounted')}</span
		>
	</div>

	{#if volume.sync}
		<div class="sync" role="status">
			<div class="sync-top">
				<span>{syncTitle}</span>
				<span class="mono percent">{formatPercent(volume.sync.percent)}</span>
			</div>
			<div class="track">
				<span class="metric-level" style:width="{volume.sync.percent}%"></span>
			</div>
			<div class="sync-bottom">
				<span class="mono">{syncRate}</span>
				<span>{syncNote}</span>
			</div>
		</div>
	{/if}
	{#if problem}<p class="notice-error" role="alert">{problem}</p>{/if}

	<div class="block">
		<span class="label">{t('storage.members')}</span>
		<div class="members">
			{#each volume.members as member (member.disk)}
				<span
					class="member"
					class:faulty={member.state === 'faulty'}
					class:building={member.state === 'rebuilding'}
				>
					<span
						class="dot"
						class:ok={member.state === 'active'}
						class:warn={member.state === 'rebuilding'}
						class:error={member.state === 'faulty'}
						aria-hidden="true"
					></span>
					<span class="mono">{member.disk || '?'}</span>
					<span class="member-state">{t(`member.${member.state}`)}</span>
					{#if canAdminister && (member.state === 'faulty' || member.state === 'spare' || (shrinks && member.state === 'active')) && member.disk}
						<button
							type="button"
							aria-label={t('storage.removeDisk', { disk: member.disk })}
							onclick={() => onremove(member.disk)}
							disabled={busy}><Icon name="close" size={14} /></button
						>
					{/if}
				</span>
			{/each}
		</div>
	</div>

	{#if volume.mounted}
		<div class="block">
			<div class="usage">
				<span class="label">{t('storage.used')}</span>
				<span class="mono"
					>{t('storage.usedOf', {
						used: formatDiskBytes(volume.usedBytes),
						total: formatDiskBytes(volume.sizeBytes),
					})}</span
				>
			</div>
			<div class="bar"><span class="metric-level" style:width="{usedPercent}%"></span></div>
		</div>
	{/if}
	{#if canAdminister && volume.shareQuotas && volume.shareQuotas !== 'unsupported'}
		<div class="quotas">
			<div>
				<span class="label">{t('storage.shareQuotas')}</span>
				<span>{t(`storage.shareQuotas.${volume.shareQuotas}`)}</span>
				{#if volume.shareQuotasError}<small class="problem">{volume.shareQuotasError}</small>{/if}
				{#if quotaError}<small class="problem" role="alert">{quotaError}</small>{/if}
			</div>
			<button
				type="button"
				class="btn"
				disabled={busy || quotaBusy}
				onclick={() => switchQuotas(volume.shareQuotas === 'off')}
				>{t(
					volume.shareQuotas === 'off' ? 'storage.shareQuotasOn' : 'storage.shareQuotasOff',
				)}</button
			>
		</div>
	{/if}
	{#if volume.mismatches > 0}
		<p class="notice-warn">
			{t('storage.mismatches')}: <span class="mono">{volume.mismatches}</span>
		</p>
	{/if}

	{#if canAdminister}
		<div class="actions">
			<a
				class="btn"
				href={'/storage/manage?' + new URLSearchParams({ volume: volume.name, mode: 'usage' })}
				>{t('ops.usage')}</a
			>
			{#if ['raid5', 'raid6'].includes(volume.level)}<a
					class="btn"
					href={'/storage/manage?' + new URLSearchParams({ volume: volume.name, mode: 'grow' })}
					>{t('ops.grow')}</a
				>{/if}
			{#if canCheck}
				<button
					type="button"
					class="btn"
					onclick={() => oncheck(volume.state !== 'checking')}
					disabled={busy}
				>
					{t(volume.state === 'checking' ? 'storage.checkStop' : 'storage.checkStart')}
				</button>
			{/if}
			{#if canAdd}<button type="button" class="btn" onclick={onadd} disabled={busy}
					>{t('storage.addDisk')}</button
				>{/if}
			{#if volume.members.length > 1 && !(volume.level === 'raid0' && volume.filesystem !== 'btrfs') && !lockedBlocks(volume)}<a
					class="btn"
					href="/storage/replace?volume={encodeURIComponent(volume.name)}">{t('swap.button')}</a
				>{/if}
			{#if volume.filesystem === 'btrfs' && volume.state === 'ok' && !locked}<button
					type="button"
					class="btn"
					onclick={onprofile}
					disabled={busy}>{t('btrfs.profileChange')}</button
				>{/if}
		</div>
		<!-- Destroying actions stand apart from maintenance, in red. -->
		<div class="danger-zone">
			<div>
				<strong>{t('ui.dangerZone')}</strong>
				<p class="muted">{t('storage.deleteHint')}</p>
			</div>
			<button type="button" class="btn btn-danger" onclick={ondelete} disabled={busy}
				>{t('storage.delete')}</button
			>
		</div>
	{/if}
	{#if canAdminister && volume.encryption}<EncryptionPanel {volume} {busy} />{/if}
	{#if canAdminister && volume.filesystem !== 'btrfs'}<CachePanel {volume} />{/if}
	{#if canAdminister && volume.mounted && volume.filesystem === 'btrfs'}<Snapshots
			volume={volume.name}
		/>{/if}
</article>

<style>
	.quotas {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px 16px;
	}
	.quotas > div {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.quotas .problem {
		color: var(--error-text);
		overflow-wrap: anywhere;
	}
	article {
		flex: 1 1 520px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 18px;
		padding: 22px;
	}
	article.warn {
		border-color: var(--warn-border);
	}
	article.error {
		border-color: var(--error-border);
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 10px;
	}
	.name {
		font-size: var(--text-xl);
		font-weight: 500;
	}
	.mount {
		margin-left: auto;
		font-size: var(--text-base);
		color: var(--muted);
	}
	.sync {
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 16px;
		background: var(--warn-bg);
		border: 1px solid var(--warn-border);
		border-radius: var(--radius-ctl);
		color: var(--warn-text);
	}
	.sync-top,
	.sync-bottom {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		align-items: baseline;
		gap: 6px 16px;
	}
	.sync-top {
		font-size: var(--text-md);
	}
	.sync-bottom {
		font-size: var(--text-base);
	}
	.percent {
		font-size: var(--text-xl);
		font-weight: 500;
	}
	.track {
		height: 8px;
		background: var(--warn-border);
		border-radius: var(--radius-bar);
		overflow: hidden;
	}
	.track span {
		display: block;
		height: 100%;
		background: var(--status-warn);
		border-radius: var(--radius-bar);
	}
	.block {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.label {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.members {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.member {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		padding: 6px 10px;
		background: var(--chip);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		font-size: var(--text-base);
	}
	.member.building {
		border-color: var(--warn-border);
	}
	.member.faulty {
		border-color: var(--error-border);
	}
	.member-state {
		color: var(--muted);
	}
	.member button {
		display: flex;
		padding: 2px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-small);
		color: var(--muted);
		cursor: pointer;
	}
	.member button:hover {
		color: var(--text);
	}
	.usage {
		display: flex;
		justify-content: space-between;
		gap: 12px;
		font-size: var(--text-base);
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		padding-top: 4px;
	}
	.danger-zone {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px 16px;
		margin-top: 6px;
		padding-top: 12px;
		border-top: 1px solid var(--border);
	}
	.danger-zone strong {
		color: var(--status-error);
		font-size: var(--text-base);
		letter-spacing: 0.04em;
		text-transform: uppercase;
	}
	.danger-zone p {
		margin: 4px 0 0;
		font-size: var(--text-base);
	}
</style>
