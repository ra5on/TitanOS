<script lang="ts">
	// The switcher (Alt+Q): the open windows, the most recent first.
	import { t } from '../i18n/index.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import type { Program } from './programs.svelte.ts';
	import { byRecency, focus, wm } from './wm.svelte.ts';

	let { programOf }: { programOf: (app: string) => Program | undefined } = $props();
</script>

{#if wm.switcher}
	<div class="switch-dim" aria-hidden="true"></div>
	<div class="switcher glass-panel" role="listbox" aria-label={t('desktop.switcher.title')}>
		{#each byRecency() as w, i (w.id)}
			{@const p = programOf(w.app)}
			<button
				type="button"
				class="thumb"
				class:on={i === wm.switcherIndex}
				role="option"
				aria-selected={i === wm.switcherIndex}
				onclick={() => {
					wm.switcher = false;
					focus(w.id);
				}}
			>
				<span class="mini"><span></span><span></span><span></span><span></span></span>
				<span class="thumb-name"
					><DesktopIcon
						glyph={p?.glyph ?? ''}
						gradient={p?.gradient ?? 'grey'}
						image={p?.image ?? ''}
						name={w.title}
						size={24}
					/><b>{w.title}</b></span
				>
			</button>
		{/each}
		<p class="keys">{t('desktop.switcher.keys')}</p>
	</div>
{/if}

<style>
	.switch-dim {
		position: absolute;
		inset: 0;
		background: rgb(0 0 0 / 0.35);
		z-index: 9600;
	}
	.switcher {
		position: absolute;
		left: 50%;
		top: 50%;
		transform: translate(-50%, -50%);
		z-index: 9700;
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: 14px;
		padding: 20px;
		border-radius: 22px;
		max-width: calc(100% - 40px);
	}
	.thumb {
		width: 190px;
		padding: var(--space-2);
		border: 0;
		border-radius: 14px;
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.thumb.on {
		outline: 3px solid var(--accent);
		background: color-mix(in srgb, var(--accent) 14%, transparent);
	}
	.mini {
		height: 110px;
		border-radius: 10px;
		background: var(--bg);
		border: 1px solid var(--border);
		padding: var(--space-2);
		display: grid;
		grid-template-columns: 34px 1fr 1fr;
		grid-template-rows: 1fr 1fr;
		gap: 5px;
	}
	.mini span {
		background: var(--chip);
		border-radius: 4px;
	}
	.mini span:first-child {
		grid-row: 1 / 3;
		background: var(--surface);
	}
	.thumb-name {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin-top: var(--space-2);
		min-width: 0;
	}
	.thumb-name b {
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.keys {
		flex-basis: 100%;
		margin: var(--space-1) 0 0;
		text-align: center;
		color: var(--muted);
		font-size: var(--text-sm);
	}
</style>
