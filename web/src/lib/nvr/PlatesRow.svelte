<script lang="ts">
	// PLAN 79-7: known number plates – a name ("Annas Auto") and the plates
	// that belong to it; a plate the cameras read can be taken over with one
	// click. Part of the NVR's settings (docs/NVR.md, `plates`).
	import { api } from '../api.ts';
	import SettingRow from '../components/settings/SettingRow.svelte';
	import { i18n, t } from '../i18n/index.svelte.ts';
	import { cameraName, nvr } from './nvr.svelte.ts';
	import { dayClock } from './texts.ts';
	import type { PlateEntry, RecognizedPlate } from './types.ts';

	let {
		plates,
		supported,
		busy,
		onsave,
	}: {
		plates: PlateEntry[];
		/** False on a NAS whose settings know no plates yet. */
		supported: boolean;
		busy: boolean;
		onsave: (plates: PlateEntry[]) => void;
	} = $props();

	const locale = $derived(i18n.locale === 'de' ? 'de-DE' : 'en-GB');
	const used = $derived(nvr.cameras.some((c) => c.detect?.plates));
	type Draft = { name: string; plates: string };
	let draft = $state<Draft[]>([]);
	$effect(() => {
		draft = plates.map((p) => ({ name: p.name, plates: p.plates.join(', ') }));
	});
	const clean = (rows: Draft[]): PlateEntry[] =>
		rows
			.map((row) => ({
				name: row.name.trim(),
				plates: row.plates
					.split(',')
					.map((p) => p.trim())
					.filter(Boolean),
			}))
			.filter((row) => row.name && row.plates.length);
	const dirty = $derived(JSON.stringify(clean(draft)) !== JSON.stringify(plates));

	let recognized = $state<RecognizedPlate[] | null>(null);
	let open = $state(false);
	$effect(() => {
		if (!open || recognized !== null) return;
		api.get<{ plates: RecognizedPlate[] }>('/nvr/plates/recognized').then(
			(res) => (recognized = res.plates ?? []),
			() => (recognized = []),
		);
	});
	const known = $derived(new Set(clean(draft).flatMap((row) => row.plates)));
	const nameOf = cameraName;
</script>

<SettingRow
	icon="file"
	title={t('nvr.plates.title')}
	desc={used ? t('nvr.plates.hint') : t('nvr.plates.unused')}
	summary={plates.length
		? plates.length === 1
			? t('nvr.plates.countOne')
			: t('nvr.plates.count', { count: plates.length })
		: t('nvr.plates.none')}
	id="nvr-plates"
	bind:open
>
	<div class="srow-block">
		{#if !supported}
			<p class="muted">{t('nvr.plates.missing')}</p>
		{:else}
			<ul class="rows">
				{#each draft as row, i (i)}
					<li>
						<input
							class="field"
							type="text"
							maxlength="40"
							bind:value={row.name}
							aria-label={t('nvr.plates.name')}
							placeholder={t('nvr.plates.name')}
						/>
						<input
							class="field mono"
							type="text"
							bind:value={row.plates}
							aria-label={t('nvr.plates.plates')}
							placeholder="M-AB 1234, MAB12*"
							spellcheck="false"
						/>
						<button
							type="button"
							class="btn"
							onclick={() => (draft = draft.filter((_, at) => at !== i))}
							>{t('nvr.plates.remove')}</button
						>
					</li>
				{/each}
			</ul>
			<div class="acts">
				<button
					type="button"
					class="btn"
					onclick={() => (draft = [...draft, { name: '', plates: '' }])}
					>{t('nvr.plates.add')}</button
				>
				<button
					type="button"
					class="btn btn-primary"
					disabled={busy || !dirty}
					onclick={() => onsave(clean(draft))}>{t('common.save')}</button
				>
			</div>
			<p class="muted">{t('nvr.plates.patternHint')}</p>
			{#if recognized?.length}
				<h4>{t('nvr.plates.seen')}</h4>
				<ul class="seen">
					{#each recognized as plate (plate.plate)}
						<li>
							<span
								><b class="mono">{plate.plate}</b>
								<small
									>{t('nvr.plates.seenInfo', {
										count: plate.count > 0 ? String(plate.count) : '–',
										time: plate.lastSeen ? dayClock(plate.lastSeen, locale) : '–',
										camera: plate.camera ? nameOf(plate.camera) : '–',
									})}</small
								></span
							>
							{#if !known.has(plate.plate)}
								<button
									type="button"
									class="btn"
									onclick={() => (draft = [...draft, { name: '', plates: plate.plate }])}
									>{t('nvr.plates.take')}</button
								>
							{:else}<span class="chip ok">{t('nvr.plates.known')}</span>{/if}
						</li>
					{/each}
				</ul>
				{#if recognized.some((p) => !p.count || !p.lastSeen || !p.camera)}
					<p class="muted old">{t('nvr.plates.seenOld')}</p>
				{/if}
			{/if}
		{/if}
	</div>
</SettingRow>

<style>
	.rows,
	.seen {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.rows li {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.rows .field {
		flex: 1 1 12rem;
		min-width: 0;
	}
	.acts {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin: var(--space-2) 0;
	}
	.seen li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2) var(--space-3);
	}
	.seen span:first-child {
		display: flex;
		flex-direction: column;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.seen small {
		color: var(--muted);
	}
	h4 {
		margin: var(--space-3) 0 var(--space-2);
		font-size: var(--text-base);
		font-weight: 600;
	}
	p {
		margin: 0;
	}
	.old {
		margin-top: var(--space-2);
		font-size: var(--text-sm);
	}
</style>
