<script lang="ts">
	// The nine functions of the agent (PLAN 54) in four groups, one column
	// (owner rule: ordered like the other settings, no wizards). Owners and
	// administrators switch them on for the NAS; every account sets its own
	// parts (folders, mailbox, sources).
	import { ApiError } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { rebaseFeatures } from '#lib/features-rebase.ts';
	import {
		loadFeatures,
		loadMine,
		saveFeatures,
		saveMine,
		type Features,
		type FeaturesMine,
		type FeaturesPublic,
	} from '#lib/agent-features.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
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
		/** Settings › KI-Agent: the intro stands under this heading. */
		heading?: string;
	} = $props();

	let nas = $state<Features | null>(null);
	let mine = $state<FeaturesMine | null>(null);
	let pub = $state<FeaturesPublic | null>(null);
	let error = $state('');
	// What happened to a save another tab overtook (1.03).
	let notice = $state('');

	// The two loads do not depend on each other (PLAN 90-6): the switches of
	// the NAS come from titand's memory and also load for an administrator who
	// may not use the agent or while the agent's volume is missing; before,
	// every switch then showed "off" whatever was set.
	async function load() {
		const [n, m] = await Promise.allSettled([
			admin ? loadFeatures() : Promise.resolve(null),
			loadMine(),
		]);
		if (n.status === 'fulfilled') nas = n.value;
		else error = apiErrorText(n.reason);
		if (m.status === 'fulfilled') {
			mine = m.value.mine;
			pub = m.value.nas;
		} else if (!error) error = apiErrorText(m.reason);
	}
	void load();

	async function savenas(next: Features) {
		error = notice = '';
		const base = nas;
		try {
			nas = await saveFeatures(next);
		} catch (e) {
			if (!(e instanceof ApiError && e.code === 'agent_features_changed') || !base) {
				error = apiErrorText(e);
				throw e;
			}
			// 1.03: overtaken by another tab or administrator. Load the new
			// state; a change to another function is saved on top of it, one
			// to the same function is not, and the page says which.
			await rebase(base, next, e);
		}
		// Saved. What the accounts see of it is only a second look.
		try {
			pub = (await loadMine()).nas;
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	async function rebase(base: Features, next: Features, conflict: unknown) {
		let fresh: Features;
		try {
			fresh = await loadFeatures();
		} catch (e) {
			error = apiErrorText(conflict);
			throw e;
		}
		const merged = rebaseFeatures(base, next, fresh);
		if (!merged) {
			nas = fresh;
			error = t('agent.f.changedLost');
			throw conflict;
		}
		try {
			nas = merged === fresh ? fresh : await saveFeatures(merged);
			notice = t('agent.f.changedKept');
		} catch (e) {
			nas = fresh;
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

<!-- PLAN 78-5: a plain heading per group, each function a flat row (switch
     on the right). -->
<div class="features">
	{#if heading}
		<SettingGroup title={heading} id="f-head" desc={t('agent.f.intro')}>
			{#if !admin}<p class="muted intro">{t('agent.f.adminOnly')}</p>{/if}
		</SettingGroup>
	{:else}
		<p class="muted intro">{t('agent.f.intro')}</p>
		{#if !admin}<p class="muted intro">{t('agent.f.adminOnly')}</p>{/if}
	{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if notice}<p class="notice-ok" role="status" data-features-notice>{notice}</p>{/if}
	{#each groups as g (g.id)}
		<SettingGroup title={t(g.label)} id="f-{g.id}">
			<g.part {...groupProps} />
		</SettingGroup>
	{/each}
</div>

<style>
	.features {
		display: flex;
		flex-direction: column;
		gap: var(--gap, var(--space-4));
	}
	.features > :global(*) {
		flex-shrink: 0;
	}
	.intro {
		margin: 0;
	}
	/* The groups use .fgrp around their FeatureRows (also in their sheets):
	   the rows of a group stand together, no box around them. */
	.features :global(.fgrp) {
		display: flex;
		flex-direction: column;
		gap: 3px;
		min-width: 0;
	}
</style>
