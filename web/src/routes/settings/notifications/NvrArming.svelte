<script lang="ts">
	// Settings › Notifications, section "Cameras": when the NVR reports
	// movement (decided 2.11.2026). The NVR page only shows the state.
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { onMount } from 'svelte';
	import type { Arming, ArmWindow, NvrState } from '../../nvr/nvr.ts';

	let nvr = $state<NvrState | null>(null);
	let draft = $state<Arming>({ mode: 'off' });
	let error = $state('');
	let saved = $state(false);
	let busy = $state(false);

	const DAYS = [1, 2, 3, 4, 5, 6, 0];

	async function load() {
		try {
			nvr = await api.get<NvrState>('/nvr');
			draft = structuredClone({
				mode: nvr.settings.arming.mode,
				schedule: nvr.settings.arming.schedule ?? [],
				cameras: nvr.settings.arming.cameras ?? [],
				quietMinutes: nvr.settings.arming.quietMinutes || 5,
			});
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	onMount(() => void load());

	async function save() {
		busy = true;
		error = '';
		saved = false;
		try {
			nvr = await api.put<NvrState>('/nvr/arming', $state.snapshot(draft));
			saved = true;
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}

	function addWindow() {
		draft.schedule = [
			...(draft.schedule ?? []),
			{ days: [1, 2, 3, 4, 5], from: '22:00', to: '06:00' },
		];
	}
	function toggleDay(w: ArmWindow, d: number) {
		w.days = w.days.includes(d) ? w.days.filter((x) => x !== d) : [...w.days, d];
	}
	function toggleCamera(id: string) {
		const list = draft.cameras ?? [];
		draft.cameras = list.includes(id) ? list.filter((x) => x !== id) : [...list, id];
	}
	const detecting = $derived((nvr?.cameras ?? []).filter((c) => c.detect));
</script>

{#if nvr}
	<section class="tile panel">
		<h2 class="tile-title">{t('nvrArm.title')}</h2>
		<p class="muted small">{t('nvrArm.intro')}</p>
		{#if error}<div class="notice-error" role="alert">{error}</div>{/if}
		{#if saved}<div class="notice-ok" role="status">{t('nvrArm.saved')}</div>{/if}
		<p class="small">
			<span class="chip {nvr.armedNow ? 'ok' : ''}"
				>{nvr.armedNow ? '● ' + t('nvrArm.armedNow') : '○ ' + t('nvrArm.disarmedNow')}</span
			>
		</p>

		<label
			><span>{t('nvrArm.mode')}</span>
			<select class="field" bind:value={draft.mode}>
				<option value="off">{t('nvrArm.modeOff')}</option>
				<option value="on">{t('nvrArm.modeOn')}</option>
				<option value="schedule">{t('nvrArm.modeSchedule')}</option>
			</select>
		</label>

		{#if draft.mode === 'schedule'}
			{#each draft.schedule ?? [] as w, i (i)}
				<div class="window">
					<div class="days">
						{#each DAYS as d (d)}
							<label class="check"
								><input
									type="checkbox"
									checked={w.days.includes(d)}
									onchange={() => toggleDay(w, d)}
								/><span>{t(`nvrArm.day${d}` as 'nvrArm.day0')}</span></label
							>
						{/each}
					</div>
					<label
						><span>{t('nvrArm.from')}</span><input
							class="field"
							type="time"
							bind:value={w.from}
						/></label
					>
					<label
						><span>{t('nvrArm.to')}</span><input
							class="field"
							type="time"
							bind:value={w.to}
						/></label
					>
					<button
						type="button"
						class="btn btn-danger-outline"
						onclick={() => (draft.schedule = (draft.schedule ?? []).filter((_, j) => j !== i))}
						>{t('nvrArm.removeWindow')}</button
					>
				</div>
			{/each}
			<div class="row-actions">
				<button type="button" class="btn" onclick={addWindow}>{t('nvrArm.addWindow')}</button>
			</div>
			<p class="muted small">{t('nvrArm.scheduleHint')}</p>
		{/if}

		<fieldset>
			<legend>{t('nvrArm.cameras')}</legend>
			{#if detecting.length === 0}
				<p class="muted small">{t('nvrArm.noDetecting')}</p>
			{/if}
			{#each detecting as c (c.id)}
				<label class="check"
					><input
						type="checkbox"
						checked={(draft.cameras ?? []).length === 0 || (draft.cameras ?? []).includes(c.id)}
						onchange={() => {
							if ((draft.cameras ?? []).length === 0) draft.cameras = detecting.map((x) => x.id);
							toggleCamera(c.id);
						}}
					/><span>{c.name}</span></label
				>
			{/each}
		</fieldset>

		<label
			><span>{t('nvrArm.quiet')}</span>
			<input class="field" type="number" min="1" max="1440" bind:value={draft.quietMinutes} />
		</label>
		<p class="muted small">{t('nvrArm.haHint')}</p>

		<div class="row-actions">
			<button type="button" class="btn btn-primary" onclick={save} disabled={busy}
				>{t('common.save')}</button
			>
		</div>
	</section>
{/if}

<style>
	section {
		display: grid;
		gap: 12px;
	}
	label {
		display: grid;
		gap: 4px;
	}
	label.check {
		display: flex;
		gap: 6px;
		align-items: center;
	}
	.window {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
		align-items: end;
		padding: 10px;
		border: 1px solid var(--border);
		border-radius: 8px;
	}
	.days {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
	}
	fieldset {
		border: 0;
		padding: 0;
		margin: 0;
		display: grid;
		gap: 6px;
	}
	legend {
		margin-bottom: 4px;
	}
</style>
