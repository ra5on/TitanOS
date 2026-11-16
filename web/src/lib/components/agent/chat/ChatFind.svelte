<script lang="ts">
	// The search in a chat (PLAN 90-4): hits in the person's messages and in
	// the answers are counted ("2 von 3"), Enter/Umschalt+Enter or the
	// arrows jump, Escape closes. The hits are marked with the CSS Custom
	// Highlight API, which leaves the page's elements alone; a browser
	// without it still counts and jumps, only without the marks.
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	let {
		find = $bindable(),
		column,
		scroller,
		version,
	}: {
		/** What is looked for; null closes the search. */
		find: string | null;
		column?: HTMLElement;
		scroller?: HTMLElement;
		/** Changes when the chat got new lines: look again. */
		version: number;
	} = $props();

	let hits = $state.raw<Range[]>([]);
	let hit = $state(0);
	type Highlights = { set: (name: string, value: unknown) => void; delete: (name: string) => void };
	const highlights = (): Highlights | null =>
		typeof CSS !== 'undefined' && 'highlights' in CSS && typeof Highlight !== 'undefined'
			? (CSS as unknown as { highlights: Highlights }).highlights
			: null;
	$effect(() => {
		const q = (find ?? '').trim().toLowerCase();
		void version;
		const found: Range[] = [];
		if (q && column) {
			for (const part of column.querySelectorAll('.user > span, .answer > .md')) {
				const walker = document.createTreeWalker(part, NodeFilter.SHOW_TEXT);
				for (let node = walker.nextNode(); node; node = walker.nextNode()) {
					const text = (node.nodeValue ?? '').toLowerCase();
					for (let i = text.indexOf(q); i >= 0; i = text.indexOf(q, i + q.length)) {
						const range = document.createRange();
						range.setStart(node, i);
						range.setEnd(node, i + q.length);
						found.push(range);
					}
				}
			}
		}
		hits = found;
		hit = found.length ? found.length - 1 : 0;
		return () => {
			highlights()?.delete('agent-find');
			highlights()?.delete('agent-find-current');
		};
	});
	$effect(() => {
		const all = hits;
		const current = all[hit];
		const h = highlights();
		if (h && current) {
			h.set('agent-find', new Highlight(...all.filter((r) => r !== current)));
			h.set('agent-find-current', new Highlight(current));
		} else {
			h?.delete('agent-find');
			h?.delete('agent-find-current');
		}
		if (current && scroller) {
			const box = current.getBoundingClientRect();
			const frame = scroller.getBoundingClientRect();
			if (box.top < frame.top + 70 || box.bottom > frame.bottom - 20)
				scroller.scrollBy({ top: box.top - frame.top - frame.height / 3 });
		}
	});
	const step = (by: number) => hits.length && (hit = (hit + by + hits.length) % hits.length);
	function findInput(node: HTMLInputElement) {
		node.focus();
		node.select();
	}
</script>

<div class="find" role="search">
	<Icon name="search" size={16} />
	<input
		type="search"
		bind:value={find}
		use:findInput
		placeholder={t('agent.findInChat')}
		aria-label={t('agent.findInChat')}
		onkeydown={(e) => {
			if (e.key === 'Enter') {
				e.preventDefault();
				step(e.shiftKey ? -1 : 1);
			} else if (e.key === 'Escape') {
				e.stopPropagation();
				find = null;
			}
		}}
	/>
	<span class="find-n" role="status"
		>{!(find ?? '').trim()
			? ''
			: hits.length
				? t('agent.findCount', { n: hit + 1, count: hits.length })
				: t('agent.findNone')}</span
	>
	<button
		type="button"
		class="find-btn"
		disabled={!hits.length}
		aria-label={t('agent.findPrev')}
		title={t('agent.findPrev')}
		onclick={() => step(-1)}><Icon name="arrowUp" size={16} /></button
	>
	<button
		type="button"
		class="find-btn"
		disabled={!hits.length}
		aria-label={t('agent.findNext')}
		title={t('agent.findNext')}
		onclick={() => step(1)}><Icon name="arrowDown" size={16} /></button
	>
	<button
		type="button"
		class="find-btn"
		aria-label={t('agent.findClose')}
		title={t('agent.findClose')}
		onclick={() => (find = null)}><Icon name="close" size={16} /></button
	>
</div>

<style>
	/* The search in the chat stays at the top while the chat scrolls. */
	.find {
		position: sticky;
		top: 0;
		z-index: 2;
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		max-width: 760px;
		margin: 0 auto var(--space-3);
		padding: var(--space-1) var(--space-1-5) var(--space-1) var(--space-3);
		border: 1px solid var(--accent);
		border-radius: var(--radius-ctl);
		background: var(--surface);
		color: var(--muted);
		box-shadow: var(--shadow-menu, 0 6px 18px rgb(0 0 0 / 0.25));
	}
	.find input {
		flex: 1;
		min-width: 0;
		min-height: 32px;
		border: 0;
		outline: 0;
		padding: 0;
		background: none;
		color: var(--text);
		font: inherit;
	}
	.find-n {
		flex: none;
		font-size: var(--text-sm);
	}
	.find-btn {
		display: grid;
		place-items: center;
		width: 32px;
		height: 32px;
		flex: none;
		border: 1px solid var(--border);
		border-radius: 8px;
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	.find-btn:disabled {
		color: var(--muted);
		cursor: default;
	}
	.find-btn:focus-visible {
		outline: 2px solid var(--accent);
	}
	:global(::highlight(agent-find)) {
		background-color: color-mix(in srgb, var(--status-warn) 40%, transparent);
		color: inherit;
	}
	:global(::highlight(agent-find-current)) {
		background-color: var(--status-warn);
		color: var(--on-warn, #000);
	}
	@media (max-width: 760px) {
		.find {
			top: max(calc(var(--agent-top, 0px) + 56px), var(--agent-head, 0px));
			gap: 2px;
			padding-left: var(--space-2-5);
		}
		.find-btn {
			min-height: 44px;
			min-width: 44px;
		}
		.find input {
			min-height: 44px;
			font-size: max(16px, 1rem);
		}
	}
</style>
