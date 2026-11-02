<script lang="ts">
	// Resolution, frame rate and bit rate of a camera's streams, set over
	// ONVIF. A change asks first; the original setting is kept for "reset".
	import Dialog from '#lib/components/Dialog.svelte';
	import { api } from '#lib/api.ts';
	import { formatBytes } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { onMount } from 'svelte';
	import { gbPerDay, type Camera, type Quality, type QualityChange } from './nvr.ts';

	type Props = { camera: Camera; onclose: () => void };
	let { camera, onclose }: Props = $props();

	let streams = $state<Quality[]>([]);
	let original = $state<Record<string, QualityChange>>({});
	let edit = $state<Record<string, { res: string; fps: number; bitrate: number }>>({});
	let asking = $state('');
	let askingReset = $state('');
	let loading = $state(true);
	let busy = $state(false);
	let error = $state('');
	let done = $state('');

	const resKey = (w: number, h: number) => `${w}x${h}`;

	async function load() {
		loading = true;
		error = '';
		try {
			const res = await api.get<{ streams: Quality[]; original?: Record<string, QualityChange> }>(
				`/nvr/cameras/${camera.id}/quality`,
			);
			streams = res.streams;
			original = res.original ?? {};
			edit = Object.fromEntries(
				streams.map((q) => [
					q.token,
					{ res: resKey(q.width, q.height), fps: q.fps, bitrate: q.bitrateKb },
				]),
			);
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			loading = false;
		}
	}
	onMount(() => void load());

	function changeOf(q: Quality): QualityChange {
		const e = edit[q.token]!;
		const [w = 0, h = 0] = e.res.split('x').map(Number);
		return {
			token: q.token,
			width: w,
			height: h,
			fps: Number(e.fps),
			bitrateKb: Number(e.bitrate),
		};
	}

	async function apply(q: Quality) {
		busy = true;
		error = '';
		done = '';
		try {
			await api.put(`/nvr/cameras/${camera.id}/quality`, changeOf(q));
			asking = '';
			done = t('nvr.qualityDone');
			await load();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}

	async function reset(q: Quality) {
		busy = true;
		error = '';
		done = '';
		try {
			await api.post(`/nvr/cameras/${camera.id}/quality/reset`, { token: q.token });
			askingReset = '';
			done = t('nvr.qualityReset');
			await load();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}

	const dirty = (q: Quality) => {
		const e = edit[q.token];
		return (
			!!e && (e.res !== resKey(q.width, q.height) || e.fps !== q.fps || e.bitrate !== q.bitrateKb)
		);
	};
</script>

<Dialog title={t('nvr.qualityTitle', { name: camera.name })} locked={busy} {onclose}>
	<div class="body">
		<p class="muted">{t('nvr.qualityIntro')}</p>
		{#if loading}<p class="muted">{t('nvr.qualityLoading')}</p>{/if}
		{#if error}<div class="notice-error" role="alert">{error}</div>{/if}
		{#if done}<div class="notice-ok" role="status">{done}</div>{/if}

		{#each streams as q (q.token)}
			{@const e = edit[q.token]}
			{#if e}
				<section class="stream">
					<strong>{q.name || q.token}</strong>
					<span class="muted small">{q.codec}</span>
					<div class="grid">
						<label
							><span>{t('nvr.qualityResolution')}</span>
							<select class="field" bind:value={e.res}>
								{#each q.resolutions?.length ? q.resolutions : [{ width: q.width, height: q.height }] as r (resKey(r.width, r.height))}
									<option value={resKey(r.width, r.height)}>{r.width} × {r.height}</option>
								{/each}
							</select>
						</label>
						<label
							><span>{t('nvr.qualityFps')}</span>
							<input
								class="field"
								type="number"
								min={q.fpsMin || 1}
								max={q.fpsMax || 60}
								bind:value={e.fps}
							/>
						</label>
						<label
							><span>{t('nvr.qualityBitrate')}</span>
							<input
								class="field"
								type="number"
								min={q.bitrateMin || 64}
								max={q.bitrateMax || 32768}
								step="64"
								bind:value={e.bitrate}
							/>
						</label>
					</div>
					<p class="muted small">
						{t('nvr.qualityEstimate', {
							now: formatBytes(gbPerDay(q.bitrateKb) * 1e9),
							next: formatBytes(gbPerDay(Number(e.bitrate)) * 1e9),
						})}
					</p>
					{#if asking === q.token}
						<div class="notice-warn" role="alert">
							<p>{t('nvr.qualityConfirm')}</p>
							<div class="row-actions">
								<button
									type="button"
									class="btn btn-primary"
									onclick={() => apply(q)}
									disabled={busy}>{t('nvr.qualityApply')}</button
								>
								<button type="button" class="btn" onclick={() => (asking = '')} disabled={busy}
									>{t('common.cancel')}</button
								>
							</div>
						</div>
					{:else}
						<div class="row-actions">
							<button
								type="button"
								class="btn btn-primary"
								disabled={busy || !dirty(q)}
								onclick={() => (asking = q.token)}>{t('nvr.qualityApply')}</button
							>
							{#if original[q.token]}
								<button
									type="button"
									class="btn"
									disabled={busy}
									onclick={() => (askingReset = q.token)}>{t('nvr.qualityResetBtn')}</button
								>
							{/if}
						</div>
					{/if}
					{#if askingReset === q.token}
						<div class="notice-warn" role="alert">
							<p>{t('nvr.qualityResetConfirm')}</p>
							<div class="row-actions">
								<button
									type="button"
									class="btn btn-primary"
									onclick={() => reset(q)}
									disabled={busy}>{t('nvr.qualityResetBtn')}</button
								>
								<button type="button" class="btn" onclick={() => (askingReset = '')} disabled={busy}
									>{t('common.cancel')}</button
								>
							</div>
						</div>
					{/if}
				</section>
			{/if}
		{/each}

		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose} disabled={busy}
				>{t('common.close')}</button
			>
		</div>
	</div>
</Dialog>

<style>
	.body {
		display: grid;
		gap: 12px;
		min-width: min(520px, 100%);
	}
	.stream {
		display: grid;
		gap: 8px;
		padding: 12px;
		border: 1px solid var(--border);
		border-radius: 8px;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
		gap: 12px;
	}
	label {
		display: grid;
		gap: 4px;
	}
	.stream p {
		margin: 0;
	}
</style>
