<script lang="ts">
	// The nine functions of the agent (PLAN 54) in four groups, one column
	// (owner rule: ordered like the other settings, no wizards). Owners and
	// administrators switch them on for the NAS; every account sets its own
	// parts (folders, mailbox, sources).
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import {
		loadFeatures,
		loadMine,
		saveFeatures,
		saveMine,
		type Features,
		type FeaturesMine,
		type FeaturesPublic,
	} from '#lib/agent-features.ts';
	import DocsGroup from './DocsGroup.svelte';
	import ProtectGroup from './ProtectGroup.svelte';
	import ForesightGroup from './ForesightGroup.svelte';
	import FamilyGroup from './FamilyGroup.svelte';

	let {
		admin,
		calm = false,
		heading,
	}: {
		admin: boolean;
		calm?: boolean;
		/** Settings › KI-Agent: the intro becomes a card with this heading. */
		heading?: string;
	} = $props();

	let nas = $state<Features | null>(null);
	let mine = $state<FeaturesMine | null>(null);
	let pub = $state<FeaturesPublic | null>(null);
	let error = $state('');

	async function load() {
		try {
			const m = await loadMine();
			mine = m.mine;
			pub = m.nas;
			if (admin) nas = await loadFeatures();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	void load();

	async function savenas(next: Features) {
		error = '';
		try {
			nas = await saveFeatures(next);
			pub = (await loadMine()).nas;
		} catch (e) {
			error = apiErrorText(e);
			throw e;
		}
	}
	async function savemine(next: FeaturesMine) {
		error = '';
		try {
			mine = await saveMine(next);
		} catch (e) {
			error = apiErrorText(e);
			throw e;
		}
	}
	const groupProps = $derived({ admin, nas, pub, mine, savenas, savemine, calm });
	const groups = [
		{ id: 'docs', label: 'agent.f.group.docs', part: DocsGroup },
		{ id: 'protect', label: 'agent.f.group.protect', part: ProtectGroup },
		{ id: 'foresight', label: 'agent.f.group.foresight', part: ForesightGroup },
		{ id: 'family', label: 'agent.f.group.family', part: FamilyGroup },
	] as const;
</script>

<!-- PLAN 68: one card per group, each function a row (switch on the right). -->
<div class="features">
	{#if heading}
		<section class="tile ui-card fcard" aria-labelledby="f-head">
			<div class="card-head"><h2 id="f-head">{heading}</h2></div>
			<div class="set-row one">
				<span class="set-hint">{t('agent.f.intro')}</span>
				{#if !admin}<span class="set-hint">{t('agent.f.adminOnly')}</span>{/if}
			</div>
		</section>
	{:else}
		<p class="muted intro">{t('agent.f.intro')}</p>
		{#if !admin}<p class="muted intro">{t('agent.f.adminOnly')}</p>{/if}
	{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#each groups as g (g.id)}
		<section class="tile ui-card fcard" aria-labelledby="f-{g.id}">
			<div class="card-head"><h3 id="f-{g.id}">{t(g.label)}</h3></div>
			<g.part {...groupProps} />
		</section>
	{/each}
</div>

<style>
	.features {
		display: flex;
		flex-direction: column;
		gap: var(--gap, 16px);
	}
	.features > :global(*) {
		flex-shrink: 0;
	}
	.intro {
		margin: 0;
	}
	/* No backdrop filter on these cards: the sheets of a function are fixed
	   and must not be held inside the card. */
	.fcard {
		-webkit-backdrop-filter: none;
		backdrop-filter: none;
	}
	.fcard .card-head > h3 {
		margin: 0;
		font-size: var(--text-md);
		font-weight: 600;
	}
	.set-row.one {
		grid-template-columns: minmax(0, 1fr);
	}
	.set-hint {
		color: color-mix(in srgb, var(--muted) 85%, var(--text));
	}
	/* The groups use .fgrp around their FeatureRows (also in their sheets). */
	.features :global(.fgrp) {
		border: 1px solid var(--border);
		border-radius: 14px;
		overflow: hidden;
		background: var(--surface);
	}
	/* In the list the rows of a group sit straight in the card. */
	.features .fcard > :global(.fgrp) {
		border: 0;
		border-radius: 0;
		background: none;
	}
	.features .fcard > :global(.fgrp + .fgrp) {
		border-top: 1px solid var(--panel-border, var(--border));
	}
</style>
