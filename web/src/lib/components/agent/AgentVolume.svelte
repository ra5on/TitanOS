<script lang="ts">
	// Where the KI-Agent keeps everything (PLAN 58): chosen when it is
	// switched on, moved later with "Umziehen". titand copies, checks,
	// switches and only then removes the old files.
	import { onDestroy } from 'svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatBytes } from '#lib/format.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';

	type Volume = {
		name: string;
		filesystem?: string;
		sizeBytes: number;
		freeBytes: number;
		kind?: 'ssd' | 'hdd' | 'usb' | 'mixed';
		current: boolean;
		oldCopy?: boolean;
	};
	type Move = {
		from: string;
		to: string;
		state: 'copying' | 'checking' | 'switching' | 'cleaning' | 'done' | 'failed';
		files: number;
		bytes: number;
		total: number;
		skipped?: number;
		error?: string;
		cleanup?: string;
	};

	type Props = {
		/** The volume in the saved settings ('' before the first choice). */
		current: string;
		/** Opens the choice when the agent is switched on without a volume. */
		choosing?: boolean;
		onchosen: (volume: string) => void;
		onmoved?: (volume: string) => void;
	};
	let { current, choosing = $bindable(false), onchosen, onmoved }: Props = $props();

	let volumes = $state<Volume[]>([]);
	let move = $state<Move | null>(null);
	let error = $state('');
	let pick = $state('');
	let moving = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;

	async function load() {
		try {
			const r = await api.get<{ volumes: Volume[]; move: Move | null }>('/agent/volumes');
			volumes = r.volumes;
			move = r.move;
			if (move && !finished(move)) poll();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	void load();
	onDestroy(() => clearTimeout(timer));

	const finished = (m: Move) => m.state === 'done' || m.state === 'failed';
	function poll() {
		clearTimeout(timer);
		timer = setTimeout(async () => {
			try {
				const r = await api.get<{ move: Move | null }>('/agent/volume/move');
				move = r.move;
				if (move && !finished(move)) poll();
				else if (move?.state === 'done') {
					onmoved?.(move.to);
					void load();
				}
			} catch {
				poll();
			}
		}, 1000);
	}

	const here = $derived(volumes.find((v) => v.name === current));
	const others = $derived(volumes.filter((v) => v.name !== current));
	const kindLabel = (v: Volume) => (v.kind ? t(`agent.vol.kind.${v.kind}` as MessageKey) : '');
	const running = $derived(!!move && !finished(move));
	const percent = $derived(
		move && move.total > 0 ? Math.min(100, (move.bytes / move.total) * 100) : 0,
	);

	$effect(() => {
		if (choosing && !pick) {
			pick = (volumes.find((v) => v.kind === 'ssd') ?? volumes[0])?.name ?? '';
		}
	});

	function choose() {
		if (!pick) return;
		onchosen(pick);
		choosing = false;
		pick = '';
	}

	let removing = $state('');
	async function removeOld(volume: string) {
		error = '';
		try {
			await api.post('/agent/volume/cleanup', { volume });
			removing = '';
			void load();
		} catch (e) {
			error = apiErrorText(e);
		}
	}

	async function start() {
		if (!pick) return;
		error = '';
		try {
			const r = await api.post<{ move: Move }>('/agent/volume/move', { to: pick });
			move = r.move;
			moving = false;
			pick = '';
			poll();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
</script>

{#snippet list(items: Volume[])}
	<fieldset class="vols">
		<legend class="sr-only">{t('agent.vol.title')}</legend>
		{#each items as v (v.name)}
			<label class="vol" class:on={pick === v.name}>
				<input type="radio" name="agent-volume" value={v.name} bind:group={pick} />
				<span class="t">
					<strong>{v.name}</strong>
					<small
						>{t('agent.vol.free', {
							free: formatBytes(v.freeBytes),
							size: formatBytes(v.sizeBytes),
						})}{kindLabel(v) ? ' · ' + kindLabel(v) : ''}</small
					>
				</span>
				{#if v.kind === 'ssd'}<span class="badge">{t('agent.vol.recommended')}</span>{/if}
			</label>
		{/each}
	</fieldset>
	<p class="muted small">{t('agent.vol.hint')}</p>
{/snippet}

<!-- PLAN 78-1: a plain heading with rows (it was a card); only the
     settings page of the agent shows it. -->
<SettingGroup title={t('agent.vol.title')} desc={t('agent.vol.intro')}>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if running && move}
		<div class="srow">
			<div class="srow-block wide moving" role="status">
				<span
					>{t(`agent.vol.state.${move.state}` as MessageKey, {
						from: move.from,
						to: move.to,
					})}</span
				>
				<div
					class="bar"
					role="progressbar"
					aria-valuemin="0"
					aria-valuemax="100"
					aria-valuenow={Math.round(percent)}
					aria-label={t('agent.vol.progress')}
				>
					<span style:width="{percent}%"></span>
				</div>
				<small class="muted"
					>{t('agent.vol.copied', {
						files: move.files,
						bytes: formatBytes(move.bytes),
						total: formatBytes(move.total),
					})}</small
				>
			</div>
		</div>
	{:else}
		{#if move?.state === 'done'}
			<p class="notice-ok" role="status">
				{t('agent.vol.done', { to: move.to })}{#if move.skipped}
					{t('agent.vol.skipped', { n: move.skipped })}{/if}{#if move.cleanup}
					{t('agent.vol.cleanup', { from: move.from })}{/if}
			</p>
		{:else if move?.state === 'failed'}
			<p class="notice-warn" role="alert">
				{t('agent.vol.failed', { from: move.from, error: move.error ?? '' })}
			</p>
		{/if}
		<SettingRow
			icon="disk"
			title={here?.name ?? (current || t('agent.vol.title'))}
			desc={here
				? t('agent.vol.free', {
						free: formatBytes(here.freeBytes),
						size: formatBytes(here.sizeBytes),
					}) + (kindLabel(here) ? ' · ' + kindLabel(here) : '')
				: current
					? t('agent.vol.missing')
					: t('agent.vol.none')}
		>
			{#snippet control()}
				{#if current && others.length > 0}
					<button
						type="button"
						class="btn"
						onclick={() => {
							pick = others[0].name;
							moving = true;
						}}>{t('agent.vol.move')}</button
					>
				{/if}
			{/snippet}
		</SettingRow>
		{#each volumes.filter((v) => v.oldCopy) as v (v.name)}
			<SettingRow
				icon="archive"
				title={t('agent.vol.oldTitle', { volume: v.name })}
				desc={t('agent.vol.oldText')}
			>
				{#snippet control()}
					<button type="button" class="btn" onclick={() => (removing = v.name)}
						>{t('agent.vol.oldRemove')}</button
					>
				{/snippet}
			</SettingRow>
		{/each}
	{/if}
</SettingGroup>

{#if choosing}
	<Dialog title={t('agent.vol.chooseTitle')} onclose={() => ((choosing = false), (pick = ''))}>
		<p>{t('agent.vol.chooseText')}</p>
		{#if volumes.length === 0}
			<p class="notice-warn">{t('agent.vol.noneMounted')}</p>
		{:else}
			{@render list(volumes)}
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn btn-primary" disabled={!pick} onclick={choose}
				>{t('agent.vol.take')}</button
			>
			<button type="button" class="btn" onclick={() => ((choosing = false), (pick = ''))}
				>{t('common.cancel')}</button
			>
		</div>
	</Dialog>
{/if}

{#if removing}
	<Dialog title={t('agent.vol.oldRemove')} onclose={() => (removing = '')}>
		<p>{t('agent.vol.oldConfirm', { volume: removing })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = '')}>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" onclick={() => removeOld(removing)}
				>{t('agent.vol.oldRemove')}</button
			>
		</div>
	</Dialog>
{/if}

{#if moving}
	<Dialog title={t('agent.vol.moveTitle')} onclose={() => ((moving = false), (pick = ''))}>
		<p>{t('agent.vol.moveText', { from: current })}</p>
		{@render list(others)}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn btn-primary" disabled={!pick} onclick={start}
				>{t('agent.vol.moveStart')}</button
			>
			<button type="button" class="btn" onclick={() => ((moving = false), (pick = ''))}
				>{t('common.cancel')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.vols {
		position: relative;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		margin: var(--space-3) 0 var(--space-1-5);
		padding: 0;
		border: 0;
	}
	.vol {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		padding: var(--space-2-5) var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		cursor: pointer;
	}
	.vol.on {
		border-color: var(--accent);
	}
	.t {
		display: flex;
		flex-direction: column;
		flex: 1;
		min-width: 0;
	}
	.t small {
		color: var(--muted);
	}
	.badge {
		flex-shrink: 0;
		padding: 2px var(--space-2);
		border-radius: 999px;
		font-size: var(--text-xs, 0.75rem);
		color: var(--accent);
		border: 1px solid var(--accent);
	}
	.moving {
		gap: var(--space-1-5);
	}
	.bar {
		height: 8px;
		border-radius: 999px;
		background: color-mix(in srgb, var(--text) 12%, transparent);
		overflow: hidden;
	}
	.bar > span {
		display: block;
		height: 100%;
		background: var(--accent);
		transition: width 0.4s ease;
	}
	.small {
		font-size: var(--text-sm, 0.875rem);
	}
</style>
