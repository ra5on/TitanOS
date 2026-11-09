<script lang="ts">
	// Model and thinking level of the KI-Agent at the top of the chat
	// (PLAN 48), like the model choice of the big chat apps: a tap opens a
	// sheet from the bottom on the phone and a panel below the button on the
	// desktop. The levels are the ones the chosen model reports (Ollama
	// thinking.values); a level the model does not offer falls back to its
	// default.
	import { fly, fade } from 'svelte/transition';
	import Icon from '#lib/components/Icon.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import { levelLabel, type Model } from '#lib/agent.ts';

	let {
		models = [],
		model = $bindable(''),
		think = $bindable(''),
		small = false,
		calm = false,
	}: {
		models?: Model[];
		model?: string;
		think?: string;
		/** Kept for callers; the picker is one chip in every place (PLAN 68). */
		small?: boolean;
		calm?: boolean;
	} = $props();

	let open = $state(false);
	let phone = $state(matchMedia('(max-width: 760px)').matches);
	const usable = $derived(models.filter((m) => m.capabilities.includes('tools')));
	const chosen = $derived(usable.find((m) => m.name === model));
	const levels = $derived(chosen?.levels ?? []);
	const choice = $derived(usable.length > 1 || levels.length > 0);
	$effect(() => {
		if (chosen && think && !levels.includes(think)) think = chosen.defaultLevel ?? '';
	});
	$effect(() => {
		const query = matchMedia('(max-width: 760px)');
		const change = () => (phone = query.matches);
		query.addEventListener('change', change);
		return () => query.removeEventListener('change', change);
	});
	const LEVEL_HINT: Record<string, 'agent.levelLowHint' | 'agent.levelHighHint'> = {
		low: 'agent.levelLowHint',
		high: 'agent.levelHighHint',
	};
	function caps(m: Model): string {
		return [
			formatBytes(m.size),
			m.capabilities.includes('vision') ? t('agent.capVision') : '',
			m.capabilities.includes('thinking') ? t('agent.capThinking') : '',
		]
			.filter(Boolean)
			.join(' · ');
	}
	const ms = $derived(calm ? 0 : 220);
	let trigger = $state<HTMLElement>();
	let at = $state({ top: 0, left: 0 });
	function toggle() {
		if (!open && trigger) {
			const r = trigger.getBoundingClientRect();
			// Centered below the chip, but never past the edge of the window.
			const mid = r.left + r.width / 2;
			at = { top: r.bottom + 8, left: Math.max(172, Math.min(mid, innerWidth - 172)) };
		}
		open = !open;
	}
	// The panel lives at the end of the page, above everything else.
	function portal(node: HTMLElement) {
		document.body.appendChild(node);
		return { destroy: () => node.remove() };
	}
</script>

<svelte:window
	onkeydown={(e) => {
		if (open && e.key === 'Escape') open = false;
	}}
/>

<span class="picker" class:small>
	{#if choice}
		<button
			type="button"
			class="trigger"
			aria-haspopup="dialog"
			aria-expanded={open}
			aria-label={t('agent.modelPick')}
			bind:this={trigger}
			onclick={toggle}
			><strong>{model || '–'}</strong>{#if think}<span>· {levelLabel(think, i18n.locale)}</span
				>{/if}<Icon name="chevronDown" size={14} /></button
		>
	{:else}
		<span class="trigger static"><strong>{model || '–'}</strong></span>
	{/if}
	{#if open}
		<div class="layer" use:portal style:--top="{at.top}px" style:--left="{at.left}px">
			<button
				type="button"
				class="scrim"
				aria-label={t('common.close')}
				transition:fade={{ duration: ms }}
				onclick={() => (open = false)}
			></button>
			<div
				class="panel floating-menu"
				role="dialog"
				aria-label={t('agent.modelPick')}
				transition:fly={phone ? { y: 320, duration: ms, opacity: 1 } : { y: -8, duration: ms }}
			>
				{#if phone}<span class="grab" aria-hidden="true"></span>{/if}
				{#if usable.length > 1}
					<h3>{t('agent.model')}</h3>
					{#each usable as m (m.name)}
						<button
							type="button"
							class="opt"
							aria-pressed={m.name === model}
							onclick={() => {
								model = m.name;
								if (!(m.levels ?? []).length) open = false;
							}}
						>
							<span>{m.name}<small>{caps(m)}</small></span>
							{#if m.name === model}<Icon name="check" size={18} />{/if}
						</button>
					{/each}
				{/if}
				{#if levels.length}
					<h3>{t('agent.think')}</h3>
					{#each levels as level (level)}
						<button
							type="button"
							class="opt"
							aria-pressed={level === think}
							onclick={() => {
								think = level;
								open = false;
							}}
						>
							<span
								>{levelLabel(level, i18n.locale)}{#if LEVEL_HINT[level]}<small
										>{t(LEVEL_HINT[level])}</small
									>{/if}</span
							>
							{#if level === think}<Icon name="check" size={18} />{/if}
						</button>
					{/each}
				{/if}
			</div>
		</div>
	{/if}
</span>

<style>
	.picker {
		position: relative;
		display: inline-flex;
		justify-content: center;
		max-width: 100%;
	}
	/* PLAN 68: a chip "gpt-oss:20b · Mittel ▾" in the head of the chat. */
	.trigger {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		max-width: 100%;
		min-height: 30px;
		padding: var(--space-1) var(--space-2-5);
		border-radius: 999px;
		border: 0;
		background: var(--chip);
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
		white-space: nowrap;
	}
	.trigger:hover:not(.static) {
		background: color-mix(in srgb, var(--text) 12%, var(--chip));
	}
	.trigger:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.trigger strong {
		font-weight: 500;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.trigger span {
		color: var(--text);
	}
	.trigger :global(svg) {
		color: var(--muted);
		flex: none;
	}
	.static {
		cursor: default;
	}
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 60;
		border: 0;
		padding: 0;
		background: rgb(0 0 0 / 0.25);
		cursor: default;
	}
	.panel {
		position: fixed;
		z-index: 61;
		top: var(--top);
		left: var(--left);
		translate: -50% 0;
		width: 320px;
		max-height: min(70dvh, calc(100dvh - var(--top) - 16px));
		overflow-y: auto;
		padding: var(--space-2) var(--space-3) var(--space-2-5);
		text-align: left;
	}
	h3 {
		margin: var(--space-2) var(--space-1) 2px;
		font-size: var(--text-xs);
		font-weight: 500;
		letter-spacing: 0.07em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.opt {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		width: 100%;
		padding: var(--space-2-5) var(--space-1-5);
		border: 0;
		border-bottom: 1px solid color-mix(in srgb, var(--border) 70%, transparent);
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		text-align: left;
		cursor: pointer;
	}
	.opt:last-child {
		border-bottom: 0;
	}
	.opt span {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.opt small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.opt :global(svg) {
		color: var(--accent);
		flex: none;
	}
	.grab {
		display: block;
		width: 40px;
		height: 5px;
		border-radius: 3px;
		background: var(--border);
		margin: 2px auto var(--space-2);
	}
	@media (max-width: 760px) {
		.scrim {
			background: rgb(0 0 0 / 0.45);
		}
		.panel {
			position: fixed;
			top: auto;
			left: 0;
			right: 0;
			bottom: 0;
			translate: none;
			width: auto;
			border-radius: 22px 22px 0 0;
			padding: var(--space-2) var(--space-4) calc(var(--space-4) + env(safe-area-inset-bottom, 0px));
		}
		.opt {
			font-size: 16px;
			min-height: 50px;
		}
	}
</style>
