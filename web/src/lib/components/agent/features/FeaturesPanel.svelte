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

	let { admin, calm = false }: { admin: boolean; calm?: boolean } = $props();

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
</script>

<div class="features">
	<p class="muted intro">{t('agent.f.intro')}</p>
	{#if !admin}<p class="muted intro">{t('agent.f.adminOnly')}</p>{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<h3 class="h">{t('agent.f.group.docs')}</h3>
	<DocsGroup {...groupProps} />
	<h3 class="h">{t('agent.f.group.protect')}</h3>
	<ProtectGroup {...groupProps} />
	<h3 class="h">{t('agent.f.group.foresight')}</h3>
	<ForesightGroup {...groupProps} />
	<h3 class="h">{t('agent.f.group.family')}</h3>
	<FamilyGroup {...groupProps} />
</div>

<style>
	.features {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	.features > :global(*) {
		flex-shrink: 0;
	}
	.intro {
		margin: 0;
	}
	.h {
		margin: 8px 2px -2px;
		font-size: var(--text-xs, 0.75rem);
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--muted);
	}
	/* The groups use .fgrp around their FeatureRows. */
	.features :global(.fgrp) {
		border: 1px solid var(--border);
		border-radius: 14px;
		overflow: hidden;
		background: var(--surface);
	}
</style>
