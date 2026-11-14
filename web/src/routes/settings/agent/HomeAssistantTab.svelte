<script lang="ts">
	// Settings › KI-Agent › Home Assistant: what the agent may switch there,
	// who it tells, and Assist.
	import Icon from '#lib/components/Icon.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { Mode, Settings } from '#lib/agent.ts';
	import AgentHARelease from './AgentHARelease.svelte';
	import AssistCard from './AssistCard.svelte';

	let {
		settings = $bindable(),
		haNotify = $bindable(),
		haRules = $bindable(),
		admins,
		modeOptions,
	}: {
		settings: Settings;
		haNotify: string;
		haRules: { key: string; mode: Mode }[];
		admins: { name: string; role: string }[];
		modeOptions: { value: Mode; label: string }[];
	} = $props();
</script>

<!-- PLAN 78-1: plain headings with one row per setting (they were cards). -->
<SettingGroup title={t('agent.sectionHA')} desc={t('agent.haIntro')}>
	<SettingRow
		icon="bell"
		title={t('agent.haNotify')}
		desc={t('agent.haNotifyHint')}
		for="ag-ha-notify"
	>
		{#snippet control()}
			<input
				id="ag-ha-notify"
				class="field"
				bind:value={haNotify}
				placeholder="notify.mobile_app_iphone"
			/>
		{/snippet}
	</SettingRow>
</SettingGroup>

<!-- PLAN 91: Assist with its key, its profile and its own rights. -->
<AssistCard bind:settings />

{#if settings}<AgentHARelease bind:settings />{/if}

<SettingGroup title={t('agent.haRules')}>
	{#snippet tools()}
		<button type="button" class="btn" onclick={() => haRules.push({ key: 'domain:', mode: 'ask' })}
			><Icon name="plus" size={15} />{t('agent.haRuleAdd')}</button
		>
	{/snippet}
	{#each haRules as r, i (i)}
		<!-- A rule is a row without a name: what it is for, then what applies. -->
		<div class="srow plain">
			<div class="srow-head frule">
				<span class="frule-where">
					<input
						class="field"
						bind:value={r.key}
						placeholder="domain:lock · entity:light.flur · area:kueche"
						aria-label={t('agent.haRules')}
					/>
				</span>
				<div class="srow-ctl">
					<Segments compact label={t('agent.haRules')} bind:value={r.mode} options={modeOptions} />
					<button
						type="button"
						class="btn"
						aria-label={t('common.delete')}
						title={t('common.delete')}
						onclick={() => haRules.splice(i, 1)}><Icon name="trash" size={15} /></button
					>
				</div>
			</div>
		</div>
	{/each}
	<p class="muted">{t('agent.haRulesHint')}</p>
</SettingGroup>

<style>
	.frule-where {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		flex: 1 1 0;
		min-width: 0;
	}
	.frule-where .field {
		flex: 1 1 9rem;
		min-width: 0;
	}
	.frule .srow-ctl {
		max-width: none;
	}
	/* The phone: the rule on a line of its own. */
	@media (max-width: 639px) {
		.frule-where {
			flex-basis: 100%;
		}
	}
</style>
