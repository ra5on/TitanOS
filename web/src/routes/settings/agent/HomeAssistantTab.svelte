<script lang="ts">
	// Settings › KI-Agent › Home Assistant: what the agent may switch there,
	// who it tells, and Assist.
	import Icon from '#lib/components/Icon.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { Mode, Settings } from '#lib/agent.ts';
	import AgentHARelease from './AgentHARelease.svelte';

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

<section class="tile ui-card" aria-labelledby="ag-ha">
	<div class="card-head"><h2 id="ag-ha">{t('agent.sectionHA')}</h2></div>
	<div class="set-row one"><span class="set-hint">{t('agent.haIntro')}</span></div>
	<label class="set-row">
		<span class="set-label"
			>{t('agent.haNotify')}<span class="set-hint">{t('agent.haNotifyHint')}</span></span
		>
		<span class="set-control">
			<input class="field" bind:value={haNotify} placeholder="notify.mobile_app_iphone" />
		</span>
	</label>
	<label class="set-row">
		<span class="set-label"
			>{t('agent.haAssist')}<span class="set-hint">{t('agent.haAssistHint')}</span></span
		>
		<span class="set-control end"
			><input
				type="checkbox"
				class="switch"
				role="switch"
				bind:checked={settings.homeassistant.assist}
			/></span
		>
	</label>
	{#if settings.homeassistant.assist}
		<label class="set-row">
			<span class="set-label">{t('agent.haAssistAs')}</span>
			<span class="set-control">
				<select class="field" bind:value={settings.homeassistant.assistAs}>
					<option value="">{t('agent.firstOwner')}</option>
					{#each admins as a (a.name)}<option value={a.name}>{a.name}</option>{/each}
				</select>
			</span>
		</label>
		<div class="set-row one">
			<p class="notice-ok">
				{t('agent.haAssistSetup', {
					url: typeof location === 'undefined' ? '' : location.origin + '/agent-ollama',
				})}
			</p>
		</div>
	{/if}
</section>

{#if settings}<AgentHARelease bind:settings />{/if}

<section class="tile ui-card" aria-labelledby="ag-ha-rules">
	<div class="card-head">
		<h2 id="ag-ha-rules">{t('agent.haRules')}</h2>
		<div class="card-tools">
			<button
				type="button"
				class="btn"
				onclick={() => haRules.push({ key: 'domain:', mode: 'ask' })}
				><Icon name="plus" size={15} />{t('agent.haRuleAdd')}</button
			>
		</div>
	</div>
	{#each haRules as r, i (i)}
		<div class="set-row frule">
			<span class="set-label frule-where">
				<input
					class="field"
					bind:value={r.key}
					placeholder="domain:lock · entity:light.flur · area:kueche"
					aria-label={t('agent.haRules')}
				/>
			</span>
			<div class="set-control end">
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
	{/each}
	<div class="card-foot"><span class="grow">{t('agent.haRulesHint')}</span></div>
</section>

<style>
	/* PLAN 68: every tab in cards with rows "label (+ hint) left, control
	   right"; lists as rows; the card's own actions in its foot. */
	.set-row.one {
		grid-template-columns: minmax(0, 1fr);
	}
	.set-row.one > p {
		margin: 0;
	}
	/* The hint on the card's surface: a little toward the text for 4.5:1. */
	.set-hint {
		color: color-mix(in srgb, var(--muted) 85%, var(--text));
	}
	.set-control > .field {
		flex: 1 1 12rem;
		min-width: 0;
	}

	.set-control .btn {
		max-width: 100%;
		white-space: normal;
		height: auto;
	}

	/* The ways to the NAS: a radio list as rows. */

	.card-foot > .grow {
		color: color-mix(in srgb, var(--muted) 85%, var(--text));
	}

	/* PLAN 68: an area is a row; the label opens its tools. */

	.frule .set-control :global(.segments) {
		flex-wrap: nowrap;
	}

	.frule-where {
		flex-direction: row;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.frule-where .field {
		flex: 1 1 9rem;
		min-width: 0;
	}

	/* The phone: long choices on a line of their own. */
	@media (max-width: 639px) {
		.frule {
			grid-template-columns: minmax(0, 1fr) !important;
		}
		.frule .set-control {
			justify-content: flex-start;
		}
	}
</style>
