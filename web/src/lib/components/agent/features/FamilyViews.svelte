<script lang="ts">
	// The views of the group opened from the agent (PLAN 54-G): 'buddy'
	// (Backup-Buddy, owners and administrators) and 'catalog' (Skill-Katalog,
	// every account of the agent). Other ids are not this group's.
	import type { FeatureViewId } from '#lib/agent-features.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import FamilyBuddy from './FamilyBuddy.svelte';
	import FamilyCatalog from './FamilyCatalog.svelte';
	import FeaturePage from './FeaturePage.svelte';
	import { featurePage } from './page-mode.ts';

	type Props = { id: FeatureViewId; onclose: () => void; calm?: boolean };
	let { id, onclose }: Props = $props();
	// PLAN 72: a page in the agent's main area on a computer.
	const page = featurePage();
</script>

{#if page && id === 'buddy'}
	<FeaturePage title={t('agent.f.family.buddy.title')} {onclose}><FamilyBuddy /></FeaturePage>
{:else if page && id === 'catalog'}
	<FeaturePage title={t('agent.f.family.catalog.title')} {onclose}><FamilyCatalog /></FeaturePage>
{:else if id === 'buddy'}
	<Dialog title={t('agent.f.family.buddy.title')} {onclose}>
		<FamilyBuddy />
		<div class="close">
			<button type="button" class="btn" onclick={onclose}>{t('agent.f.family.close')}</button>
		</div>
	</Dialog>
{:else if id === 'catalog'}
	<Dialog title={t('agent.f.family.catalog.title')} {onclose}>
		<FamilyCatalog />
		<div class="close">
			<button type="button" class="btn" onclick={onclose}>{t('agent.f.family.close')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.close {
		display: flex;
		justify-content: flex-end;
		margin-top: var(--space-3);
	}
</style>
