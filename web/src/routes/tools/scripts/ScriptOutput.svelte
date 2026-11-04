<script lang="ts">
	// The live output of a script: asks every second for what is new while
	// it runs, then shows the end of the last run.
	import { api } from '#lib/api.ts';
	import { poll } from '#lib/poll.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { outcomeTone, shortDuration, type ScriptOutput } from './scripts.ts';

	type Props = { id: string; name: string; running: boolean; onended: () => void };
	let { id, name, running, onended }: Props = $props();

	let text = $state('');
	let next = 0;
	let skipped = $state(false);
	let live = $state(false);
	let result = $state<ScriptOutput['run'] | null>(null);
	let error = $state('');
	let box: HTMLPreElement | undefined = $state();
	let follow = true;

	async function load() {
		try {
			const out = await api.get<ScriptOutput>(`/scripts/${id}/output?from=${next}`);
			if (out.skipped && next === 0) skipped = true;
			else if (out.skipped) {
				text = '';
				skipped = true;
			}
			text += out.text;
			next = out.next;
			error = '';
			const was = live;
			live = out.running;
			result = out.run ?? null;
			if (was && !out.running) onended();
			if (follow && box) requestAnimationFrame(() => box && (box.scrollTop = box.scrollHeight));
		} catch (err) {
			error = apiErrorText(err);
		}
	}
	// A new run (started from the list) begins with an empty output.
	$effect(() => {
		if (running && !live) {
			text = '';
			next = 0;
			skipped = false;
			live = true;
		}
	});
	$effect(() => {
		void load();
	});
	$effect(() => {
		if (!live) return;
		return poll(load, { interval: 1000, immediate: false });
	});
	function scrolled() {
		if (box) follow = box.scrollTop + box.clientHeight >= box.scrollHeight - 8;
	}
</script>

<div class="output">
	<div class="state">
		<span class="muted">{t('scripts.outputOf', { name })}</span>
		{#if live}<span class="chip info" role="status">{t('scripts.outcome.running')}</span
			>{:else if result}<span class="chip {outcomeTone(result.outcome)}" role="status"
				>{t(`scripts.outcome.${result.outcome}` as MessageKey)}{#if result.exit !== undefined}
					· {t('scripts.exit', { code: result.exit })}{/if}
				· {shortDuration(result.durationMs)}</span
			>{/if}
	</div>
	{#if skipped}<p class="muted">{t('scripts.outputSkipped')}</p>{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<pre
		class="mono"
		bind:this={box}
		onscroll={scrolled}
		aria-label={t('scripts.outputOf', { name })}
		aria-live="off">{text || (live ? '' : t('scripts.noOutput'))}</pre>
</div>

<style>
	.output {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.state {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.output p {
		margin: 0;
	}
	pre {
		margin: 0;
		min-height: 6rem;
		max-height: 24rem;
		overflow: auto;
		padding: var(--space-3);
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
