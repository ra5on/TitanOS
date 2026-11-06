<script lang="ts">
	// The views of the group opened from the agent (PLAN 54-G): 'buddy'
	// (Backup-Buddy, owners and administrators) and 'catalog' (Skill-Katalog,
	// every account of the agent). Other ids are not this group's.
	import type { FeatureViewId } from '#lib/agent-features.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import FamilyBuddy from './FamilyBuddy.svelte';
	import FamilyCatalog from './FamilyCatalog.svelte';

	type Props = { id: FeatureViewId; onclose: () => void; calm?: boolean };
	let { id, onclose }: Props = $props();
</script>

{#if id === 'buddy'}
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
		margin-top: 12px;
	}
</style>
