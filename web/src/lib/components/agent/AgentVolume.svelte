<script lang="ts">
	// Where the KI-Agent keeps everything (PLAN 58): chosen when it is
	// switched on, moved later with "Umziehen". titand copies, checks,
	// switches and only then removes the old files.
	import { onDestroy } from 'svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatBytes } from '#lib/format.ts';
	import Dialog from '#lib/components/Dialog.svelte';

	type Volume = {
		name: string;
		filesystem?: string;
		sizeBytes: number;
		freeBytes: number;
		kind?: 'ssd' | 'hdd' | 'usb' | 'mixed';
		current: boolean;
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

<section class="tile panel" aria-labelledby="ag-volume">
	<h2 class="tile-title" id="ag-volume">{t('agent.vol.title')}</h2>
	<p class="muted">{t('agent.vol.intro')}</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if running && move}
		<div class="moving" role="status">
			<span
				>{t(`agent.vol.state.${move.state}` as MessageKey, { from: move.from, to: move.to })}</span
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
		<div class="row">
			<span class="t">
				{#if here}
					<strong>{here.name}</strong>
					<small
						>{t('agent.vol.free', {
							free: formatBytes(here.freeBytes),
							size: formatBytes(here.sizeBytes),
						})}{kindLabel(here) ? ' · ' + kindLabel(here) : ''}</small
					>
				{:else if current}
					<strong>{current}</strong><small>{t('agent.vol.missing')}</small>
				{:else}
					<small class="muted">{t('agent.vol.none')}</small>
				{/if}
			</span>
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
		</div>
	{/if}
</section>

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
		gap: 6px;
		margin: 12px 0 6px;
		padding: 0;
		border: 0;
	}
	.vol {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 10px 12px;
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
		padding: 2px 8px;
		border-radius: 999px;
		font-size: var(--text-xs, 0.75rem);
		color: var(--accent);
		border: 1px solid var(--accent);
	}
	.row {
		display: flex;
		align-items: center;
		gap: 12px;
		margin-top: 10px;
		flex-wrap: wrap;
	}
	.moving {
		display: flex;
		flex-direction: column;
		gap: 6px;
		margin-top: 10px;
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
