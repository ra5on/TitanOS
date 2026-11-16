<script lang="ts">
	// While the agent works: the answer as it streams in, and a line with the
	// figure of what is done right now and its word; over a named step a
	// light runs. When a run this view saw ends with an answer, a tick with
	// "Fertig" for a moment.
	import { untrack } from 'svelte';
	import { quintOut } from 'svelte/easing';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { busyKind, stepDetail, type Chat, type Entry } from '#lib/agent.ts';
	import BusyFigure from '../BusyFigure.svelte';
	import Markdown from './Markdown.svelte';
	import { stepLabel } from './chat-text.svelte.ts';

	let {
		chat,
		live,
		busy = false,
		calm = false,
	}: {
		chat: Chat;
		live: { thinking: string; content: string; step: Entry | null };
		busy?: boolean;
		calm?: boolean;
	} = $props();

	// ----- while the agent works: words and a figure for what it does -----
	// (owner 15 Nov 2026) Thinking, a dozen words of our own take turns; a
	// running step has words of its kind ("Stöbert im Netz …") and the dots
	// form a figure for it. At night two more words join, after twenty
	// seconds words of patience and the time. When the answer stands, the
	// dots become a tick for a moment. Still with "Animationen: Aus".
	const WORDS = {
		'': 18,
		web: 8,
		files: 8,
		write: 8,
		home: 6,
		switch: 3,
		code: 8,
		system: 4,
	} as const;
	const key = (k: string) => t(`agent.busy.${k}` as MessageKey);
	let busyTurn = $state(Math.floor(Math.random() * 12));
	let elapsed = $state(0);
	$effect(() => {
		if (!busy) return;
		const start = Date.now();
		elapsed = 0;
		const clock = setInterval(() => (elapsed = Math.floor((Date.now() - start) / 1000)), 1000);
		const words = calm ? undefined : setInterval(() => (busyTurn += 1), 2800);
		return () => {
			clearInterval(clock);
			clearInterval(words);
		};
	});
	const kind = $derived(busyKind(live.step?.tool));
	const busyWord = $derived.by(() => {
		if (kind) return key(`${kind}.${(busyTurn % WORDS[kind]) + 1}`);
		const hour = new Date().getHours();
		if (elapsed >= 20 && busyTurn % 3 === 0)
			return key(`long.${(Math.floor(busyTurn / 3) % 6) + 1}`);
		if ((hour >= 23 || hour < 5) && busyTurn % 4 === 1)
			return key(`night.${(Math.floor(busyTurn / 4) % 2) + 1}`);
		return key(String((busyTurn % WORDS['']) + 1));
	});
	// How the tick leaves (owner 16 Nov 2026: it went away with a jolt): it
	// fades first, then its room closes – the height and, with it, the gap
	// the column keeps between its lines, so nothing below or above jumps
	// when the line is gone.
	function leave(node: HTMLElement) {
		const height = node.offsetHeight;
		const gap = parseFloat(getComputedStyle(node.parentElement ?? node).rowGap) || 0;
		return {
			duration: 620,
			easing: (t: number) => t,
			css: (t: number) => {
				// t runs from 1 to 0: fade in the first 45 %, close in the rest.
				const fade = Math.max(0, (t - 0.55) / 0.45);
				const room = quintOut(Math.min(1, t / 0.55));
				return `overflow:hidden;min-height:0;height:${room * height}px;margin-top:${-(1 - room) * gap}px;opacity:${fade}`;
			},
		};
	}
	// The tick: only after a run this view saw end with an answer.
	let done = $state(false);
	let wasBusy = false;
	let busyChat = '';
	let doneTimer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => {
		// Another chat opened while one ran (1.01: runs per chat): its run
		// did not end here.
		const same = chat.id === busyChat;
		busyChat = chat.id;
		const ended = same && wasBusy && !busy;
		wasBusy = busy;
		if (!same) {
			clearTimeout(doneTimer);
			done = false;
		}
		if (busy) {
			clearTimeout(doneTimer);
			done = false;
		}
		if (!ended) return;
		untrack(() => {
			const last = chat.entries.at(-1);
			if (calm || chat.pending || last?.role !== 'assistant' || !last.content) return;
			done = true;
			doneTimer = setTimeout(() => (done = false), 1700);
		});
	});
</script>

{#if busy}
	<!-- What happens right now: the dots in the figure of what is done,
	     and its word; over a named step a light runs. -->
	{#if live.content}
		<Markdown text={live.content} streaming answer />
	{/if}
	<p class="live-line" class:quiet={!!live.content && !live.step}>
		<!-- 0.100: the figure in space, one per kind of work (BusyFigure). -->
		{#key live.content && !live.step ? 'write' : kind}
			<BusyFigure
				kind={live.content && !live.step ? 'write' : kind}
				size={live.content && !live.step ? 20 : 26}
				{calm}
			/>
		{/key}
		{#if live.step}
			<span class="sweep"
				>{kind ? busyWord : stepLabel(live.step)}{#if stepDetail(live.step.args)}
					· {stepDetail(live.step.args)}{/if}</span
			>
		{:else if !live.content}
			{#key busyWord}<span class="busy-word">{busyWord}</span>{/key}
		{/if}
		{#if elapsed >= 20 && !live.content}
			<span class="took">{t('agent.busy.seconds', { n: elapsed })}</span>
		{/if}
	</p>
{:else if done}
	<p class="live-line done" out:leave>
		<svg class="tick" viewBox="0 0 18 18" aria-hidden="true">
			<circle cx="9" cy="9" r="7.5" />
			<path d="M5.4 9.4l2.5 2.5 4.7-5" />
		</svg>
		<span>{t('agent.busy.done')}</span>
	</p>
{/if}

<style>
	/* While the agent works: three dots, and a light that runs over the
	   line from left to right (owner 15 Nov 2026). "Animationen: Aus" and
	   "Weniger Bewegung" leave the line still. */
	.live-line {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		margin: 0;
		min-height: 32px;
		font-size: 0.95em;
		color: var(--muted);
		animation: come 0.32s cubic-bezier(0.22, 1, 0.36, 1) both;
	}
	@keyframes come {
		from {
			opacity: 0;
			transform: translateY(6px);
		}
	}
	:global(.agent.calm) .live-line {
		animation: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.live-line {
			animation: none;
		}
	}
	/* Under an answer that streams in, the figure stays, smaller and alone. */
	.live-line.quiet {
		min-height: 22px;
		opacity: 0.8;
	}
	.took {
		font-size: 0.86em;
		font-variant-numeric: tabular-nums;
		color: var(--muted);
		opacity: 0.8;
	}
	/* The word rolls in from below when it changes. */
	.busy-word {
		display: inline-block;
		animation: roll 0.36s cubic-bezier(0.22, 1, 0.36, 1) both;
		color: var(--ag-warm, var(--accent));
	}
	@keyframes roll {
		from {
			opacity: 0;
			transform: translateY(0.55em);
		}
	}
	/* The end of a run: a ring closes, the tick is drawn. */
	.live-line.done {
		overflow: hidden;
		/* It takes the place of the working line: no step in, only a fade. */
		animation: settle 0.3s ease-out both;
	}
	@keyframes settle {
		from {
			opacity: 0;
		}
	}
	.tick {
		flex: none;
		width: 18px;
		height: 18px;
		fill: none;
		stroke: var(--ag-warm, var(--accent));
		stroke-width: 1.8;
		stroke-linecap: round;
		stroke-linejoin: round;
		animation: pop 0.5s cubic-bezier(0.34, 1.56, 0.64, 1) both;
	}
	.tick circle {
		stroke-dasharray: 48;
		stroke-dashoffset: 48;
		opacity: 0.55;
		animation: draw 0.45s ease-out forwards;
	}
	.tick path {
		stroke-dasharray: 12;
		stroke-dashoffset: 12;
		animation: draw 0.3s 0.3s ease-out forwards;
	}
	@keyframes draw {
		to {
			stroke-dashoffset: 0;
		}
	}
	@keyframes pop {
		from {
			transform: scale(0.4);
			opacity: 0;
		}
	}
	.sweep {
		/* Grey like in the chat apps: the orange dots say that something runs,
		   the light over the grey line says that it is still running. */
		--sweep: var(--muted);
		min-width: 0;
		overflow-wrap: anywhere;
		color: transparent;
		background: linear-gradient(
			90deg,
			var(--sweep) 0%,
			var(--sweep) 38%,
			color-mix(in srgb, var(--sweep) 30%, transparent) 50%,
			var(--sweep) 62%,
			var(--sweep) 100%
		);
		background-size: 220% 100%;
		-webkit-background-clip: text;
		background-clip: text;
		animation: sweep 1.7s linear infinite;
	}
	@keyframes sweep {
		from {
			background-position: 100% 0;
		}
		to {
			background-position: 0% 0;
		}
	}
	:global(.agent.calm) .sweep,
	:global(.agent.calm) .busy-word {
		animation: none;
	}
	:global(.agent.calm) .sweep {
		color: var(--sweep);
		background: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.busy-word,
		.tick,
		.tick *,
		.sweep {
			animation: none;
		}
		.sweep {
			color: var(--sweep);
			background: none;
		}
		.tick * {
			stroke-dashoffset: 0;
		}
		.live-line.done {
			animation: none;
		}
	}
</style>
