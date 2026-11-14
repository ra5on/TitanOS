<script lang="ts">
	// PLAN 90-4: project, model and thinking level of the chat in one sheet
	// (a tap on the title in the phone's one-line head, "/projekt" and
	// "/modell" in the input). The levels are the ones the chosen model
	// reports; a level it does not offer falls back to its default.
	import Icon from '#lib/components/Icon.svelte';
	import BottomSheet from './BottomSheet.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import { levelLabel, type Model, type Project } from '#lib/agent.ts';

	let {
		projects,
		project = '',
		models = [],
		model = $bindable(''),
		think = $bindable(''),
		calm = false,
		onproject,
		onclose,
	}: {
		projects: Project[];
		/** The project of the chat, or the one a new chat goes to. */
		project?: string;
		models?: Model[];
		model?: string;
		think?: string;
		calm?: boolean;
		/** Without it the project cannot be changed (another account's chat). */
		onproject?: (id: string) => void;
		onclose: () => void;
	} = $props();

	const usable = $derived(models.filter((m) => m.capabilities.includes('tools')));
	const chosen = $derived(usable.find((m) => m.name === model));
	const levels = $derived(chosen?.levels ?? []);
	$effect(() => {
		if (chosen && think && !levels.includes(think)) think = chosen.defaultLevel ?? '';
	});
	const caps = (m: Model) =>
		[
			formatBytes(m.size),
			m.capabilities.includes('vision') ? t('agent.capVision') : '',
			m.capabilities.includes('thinking') ? t('agent.capThinking') : '',
		]
			.filter(Boolean)
			.join(' · ');
</script>

<BottomSheet label={t('agent.head.title')} {calm} {onclose}>
	{#if onproject}
		<h3 class="sh-group">{t('agent.head.project')}</h3>
		{#each projects as p (p.id)}
			<button
				type="button"
				class="sh-item"
				aria-pressed={p.id === project}
				onclick={() => {
					onproject(p.id);
					onclose();
				}}
				><span class="emo" aria-hidden="true">{p.icon || '📁'}</span><span class="sh-text"
					>{p.name}</span
				>{#if p.id === project}<span class="sh-check" aria-hidden="true">✓</span>{/if}</button
			>
		{/each}
		<button
			type="button"
			class="sh-item"
			aria-pressed={!project}
			onclick={() => {
				onproject('');
				onclose();
			}}
			><Icon name="close" size={18} /><span class="sh-text">{t('agent.noProject')}</span
			>{#if !project}<span class="sh-check" aria-hidden="true">✓</span>{/if}</button
		>
		<hr class="sh-rule" />
	{/if}
	<h3 class="sh-group">{t('agent.model')}</h3>
	{#each usable as m (m.name)}
		<button
			type="button"
			class="sh-item"
			aria-pressed={m.name === model}
			onclick={() => (model = m.name)}
			><Icon name="chip" size={18} /><span class="sh-text">{m.name}<small>{caps(m)}</small></span
			>{#if m.name === model}<span class="sh-check" aria-hidden="true">✓</span>{/if}</button
		>
	{:else}
		<p class="sh-quote">{t('agent.noToolModel')}</p>
	{/each}
	{#if levels.length}
		<h3 class="sh-group">{t('agent.think')}</h3>
		<div class="seg" role="radiogroup" aria-label={t('agent.think')}>
			{#each levels as level (level)}
				<label class:on={level === think}
					><input type="radio" bind:group={think} value={level} />{levelLabel(
						level,
						i18n.locale,
					)}</label
				>
			{/each}
		</div>
	{/if}
</BottomSheet>

<style>
	.emo {
		width: 18px;
		text-align: center;
		flex: none;
	}
	.seg {
		display: flex;
		margin: var(--space-1) var(--space-4) var(--space-2);
		padding: 3px;
		border-radius: var(--radius-ctl);
		background: var(--track);
	}
	.seg label {
		flex: 1;
		min-height: 44px;
		display: grid;
		place-items: center;
		padding: 0 var(--space-2);
		border-radius: calc(var(--radius-ctl) - 2px);
		color: var(--muted);
		text-align: center;
		cursor: pointer;
	}
	.seg label.on {
		background: var(--surface);
		color: var(--text);
		font-weight: 600;
		box-shadow: 0 1px 2px rgb(0 0 0 / 0.25);
	}
	.seg input {
		position: absolute;
		opacity: 0;
		pointer-events: none;
	}
	.seg label:has(:focus-visible) {
		outline: 2px solid var(--accent);
	}
</style>
