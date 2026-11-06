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
		/** The smaller form below a chat title. */
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
	let accent = $state('');
	function toggle() {
		if (!open && trigger) {
			const r = trigger.getBoundingClientRect();
			// The agent's own accent (Darstellung) lives on .agent, not on <body>.
			accent = getComputedStyle(trigger).getPropertyValue('--accent').trim();
			at = { top: r.bottom + 8, left: r.left + r.width / 2 };
		}
		open = !open;
	}
	// The panel lives at the end of the page: glass surfaces around the
	// button would otherwise hold the fixed sheet inside them.
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
				>{/if}<Icon name="chevronDown" size={small ? 12 : 16} /></button
		>
	{:else}
		<span class="trigger static"><strong>{model || '–'}</strong></span>
	{/if}
	{#if open}
		<div
			class="layer"
			use:portal
			style:--top="{at.top}px"
			style:--left="{at.left}px"
			style:--accent={accent || undefined}
		>
			<button
				type="button"
				class="scrim"
				aria-label={t('common.close')}
				transition:fade={{ duration: ms }}
				onclick={() => (open = false)}
			></button>
			<div
				class="panel"
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
	.trigger {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		max-width: 100%;
		padding: 6px 12px;
		border-radius: 999px;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: 15px;
		cursor: pointer;
		white-space: nowrap;
		transition: background 0.15s ease;
	}
	.trigger:hover:not(.static) {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.trigger strong {
		font-weight: 600;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.trigger span {
		color: var(--muted);
	}
	.trigger :global(svg) {
		color: var(--muted);
		flex: none;
	}
	.static {
		cursor: default;
	}
	.small .trigger {
		font-size: var(--text-xs);
		padding: 1px 6px;
	}
	.small .trigger strong {
		font-weight: 500;
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
		padding: 8px 12px 10px;
		border-radius: 18px;
		border: 1px solid var(--glass-border, var(--border));
		background: color-mix(in srgb, var(--surface) 90%, transparent);
		-webkit-backdrop-filter: blur(28px) saturate(170%);
		backdrop-filter: blur(28px) saturate(170%);
		box-shadow: 0 24px 60px -20px rgb(0 0 0 / 0.6);
		text-align: left;
	}
	h3 {
		margin: 8px 4px 2px;
		font-size: var(--text-xs);
		font-weight: 500;
		letter-spacing: 0.07em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.opt {
		display: flex;
		align-items: center;
		gap: 12px;
		width: 100%;
		padding: 10px 6px;
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
		margin: 2px auto 8px;
	}
	@media (max-width: 760px) {
		.scrim {
			background: rgb(0 0 0 / 0.45);
			-webkit-backdrop-filter: blur(3px);
			backdrop-filter: blur(3px);
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
			padding: 8px 16px calc(16px + env(safe-area-inset-bottom, 0px));
		}
		.opt {
			font-size: 16px;
			min-height: 50px;
		}
	}
</style>
