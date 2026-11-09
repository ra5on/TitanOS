<script lang="ts">
	// Gruppe „Gemeinschaft und Familie“ (PLAN 54-G): the rows of the two
	// functions and their settings. Owners and administrators switch them on
	// for the NAS. The Familien-Tagebuch was removed with 0.69 (owner, 8.11.2026).
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import type { Features, FeaturesMine, FeaturesPublic } from '#lib/agent-features.ts';
	import { loadBuddy, type Buddy } from '#lib/features/family.ts';
	import FeatureRow from './FeatureRow.svelte';
	import FamilyViews from './FamilyViews.svelte';

	type Props = {
		admin: boolean;
		nas: Features | null;
		pub: FeaturesPublic | null;
		mine: FeaturesMine | null;
		savenas: (next: Features) => Promise<void>;
		savemine: (next: FeaturesMine) => Promise<void>;
		calm?: boolean;
	};
	let { admin, nas, pub, mine, savenas, savemine, calm = false }: Props = $props();

	const tk = (key: string) => t(key as MessageKey);
	type Part = 'buddy' | 'catalog';
	let open = $state<Part | null>(null);
	let view = $state<'buddy' | 'catalog' | null>(null);
	let error = $state('');
	let saved = $state(false);
	let busy = $state(false);

	// Drafts of the settings while a dialog is open.
	let buddy = $state<Buddy>({});
	let volumes = $state<string[]>([]);

	const isOn = (p: Part) => (admin && nas ? !!nas[p]?.enabled : !!pub?.[p]);

	function openPart(p: Part) {
		error = '';
		saved = false;
		if (p === 'buddy' && nas) {
			buddy = { ...nas.buddy };
			loadBuddy()
				.then((v) => (volumes = v.volumes ?? []))
				.catch(() => (volumes = []));
		}
		open = p;
	}

	async function toggle(p: Part, on: boolean) {
		if (!nas) return;
		error = '';
		try {
			if (p === 'buddy') await savenas({ ...nas, buddy: { ...nas.buddy, enabled: on } });
			if (p === 'catalog') await savenas({ ...nas, catalog: { ...nas.catalog, enabled: on } });
		} catch {
			// The panel shows the error; a buddy without a volume opens its settings.
			if (p === 'buddy' && on) openPart('buddy');
		}
	}

	async function run(work: () => Promise<void>) {
		error = '';
		saved = false;
		busy = true;
		try {
			await work();
			saved = true;
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}

	const saveBuddy = () => run(() => savenas({ ...nas!, buddy: { ...buddy } }));
	const weekdays = $derived([0, 1, 2, 3, 4, 5, 6].map((d) => tk(`agent.f.family.wd${d}`)));
	const buddySub = $derived(
		isOn('buddy') && nas?.buddy?.every
			? tk(`agent.f.family.buddy.every.${nas.buddy.every}`)
			: t('agent.f.family.buddy.sub'),
	);
</script>

<div class="fgrp">
	<FeatureRow
		icon="🤝"
		color="#14b8a6"
		title={t('agent.f.family.buddy.title')}
		sub={buddySub}
		on={isOn('buddy')}
		toggle={admin && !!nas}
		ontoggle={(on) => toggle('buddy', on)}
		onopen={admin ? () => openPart('buddy') : undefined}
	/>
	<FeatureRow
		icon="🧩"
		color="#a855f7"
		title={t('agent.f.family.catalog.title')}
		sub={t('agent.f.family.catalog.sub')}
		on={isOn('catalog')}
		toggle={admin && !!nas}
		ontoggle={(on) => toggle('catalog', on)}
		onopen={admin || pub?.catalog ? () => openPart('catalog') : undefined}
	/>
</div>

{#if open === 'buddy' && nas}
	<Dialog title={t('agent.f.family.buddy.title')} onclose={() => (open = null)}>
		<form
			class="fam"
			onsubmit={(e) => {
				e.preventDefault();
				void saveBuddy();
			}}
		>
			<p class="muted">{t('agent.f.family.buddy.intro')}</p>
			<label class="field-row"
				><span>{t('agent.f.family.buddy.address')}</span>
				<input class="field" bind:value={buddy.address} placeholder="nas-eva.tail1234.ts.net" />
				<small class="muted">{t('agent.f.family.buddy.addressHint')}</small></label
			>
			<label class="field-row"
				><span>{t('agent.f.family.buddy.volume')}</span>
				<select class="field" bind:value={buddy.volume}>
					<option value="">{t('agent.f.family.buddy.noVolume')}</option>
					{#each volumes as v (v)}<option value={v}>{v}</option>{/each}
					{#if buddy.volume && !volumes.includes(buddy.volume)}<option value={buddy.volume}
							>{buddy.volume}</option
						>{/if}
				</select>
				<small class="muted">{t('agent.f.family.buddy.volumeHint')}</small></label
			>
			<div class="two">
				<label class="field-row"
					><span>{t('agent.f.family.buddy.every')}</span>
					<select class="field" bind:value={buddy.every}>
						{#each ['daily', 'weekly', 'monthly'] as e (e)}<option value={e}
								>{tk(`agent.f.family.buddy.every.${e}`)}</option
							>{/each}
					</select></label
				>
				<label class="field-row"
					><span>{t('agent.f.family.buddy.time')}</span>
					<input class="field" type="time" bind:value={buddy.time} /></label
				>
			</div>
			{#if buddy.every === 'weekly'}
				<label class="field-row"
					><span>{t('agent.f.family.buddy.weekday')}</span>
					<select class="field" bind:value={buddy.weekday}>
						{#each weekdays as name, d (d)}<option value={d}>{name}</option>{/each}
					</select></label
				>
			{/if}
			<div class="two">
				<label class="field-row"
					><span>{t('agent.f.family.buddy.probe')}</span>
					<select class="field" bind:value={buddy.probe}>
						<option value="monthly">{t('agent.f.family.buddy.probe.monthly')}</option>
						<option value="off">{t('agent.f.family.buddy.probe.off')}</option>
					</select></label
				>
				<label class="field-row"
					><span>{t('agent.f.family.buddy.probeFiles')}</span>
					<input
						class="field"
						type="number"
						min="1"
						max="200"
						bind:value={buddy.probeFiles}
					/></label
				>
			</div>
			<div class="two">
				<label class="field-row"
					><span>{t('agent.f.family.buddy.protect')}</span>
					<input
						class="field"
						type="number"
						min="7"
						max="365"
						bind:value={buddy.protectDays}
					/></label
				>
			</div>
			<small class="muted">{t('agent.f.family.buddy.protectHint')}</small>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			{#if saved}<p class="muted" role="status">{t('agent.f.saved')}</p>{/if}
			<div class="actions">
				{#if nas.buddy?.enabled}<button type="button" class="btn" onclick={() => (view = 'buddy')}
						>{t('agent.f.family.buddy.open')}</button
					>{/if}
				<button class="btn btn-primary" disabled={busy}>{t('agent.f.family.save')}</button>
			</div>
		</form>
	</Dialog>
{/if}

{#if open === 'catalog'}
	<Dialog title={t('agent.f.family.catalog.title')} onclose={() => (open = null)}>
		<div class="fam">
			<p class="muted">{t('agent.f.family.catalog.intro')}</p>
			<div class="actions">
				{#if isOn('catalog')}<button
						type="button"
						class="btn btn-primary"
						onclick={() => (view = 'catalog')}>{t('agent.f.family.catalog.open')}</button
					>{:else}<p class="muted">{t('agent.f.family.catalog.offHint')}</p>{/if}
			</div>
		</div>
	</Dialog>
{/if}

{#if view}
	<FamilyViews id={view} {calm} onclose={() => (view = null)} />
{/if}

<style>
	.fam {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		min-width: 0;
	}
	.fam p {
		margin: 0;
	}
	.field-row {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		min-width: 0;
	}
	.field-row > span {
		font-weight: 600;
	}
	.field-row .field {
		width: 100%;
		min-width: 0;
	}
	.two {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--space-2-5);
	}
	@media (max-width: 420px) {
		.two {
			grid-template-columns: 1fr;
		}
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: var(--space-2);
	}
</style>
