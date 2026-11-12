<script lang="ts">
	// PLAN 79-7: one day of a camera as a line – recorded stretches, movement
	// and recognized objects in colour, gaps empty, events as marks, the
	// playhead. A click chooses a time; the line can be widened (24 h, 6 h,
	// 1 h per window width) and then scrolls sideways inside its own frame.
	import { i18n, t } from '../i18n/index.svelte.ts';
	import { clock, labelName } from './texts.ts';
	import { DAY, runs } from './timeline.ts';
	import type { NvrEvent, Segment } from './types.ts';

	let {
		from,
		segments,
		events,
		at,
		zoom,
		/** A chosen stretch (export). */
		range = null,
		onpick,
	}: {
		/** The start of the day. */
		from: number;
		segments: Segment[];
		events: NvrEvent[];
		/** The playhead. */
		at: number | null;
		/** Hours visible in the width of the frame: 24, 6 or 1. */
		zoom: number;
		range?: { from: number; to: number } | null;
		onpick: (time: number) => void;
	} = $props();

	const locale = $derived(i18n.locale === 'de' ? 'de-DE' : 'en-GB');
	const pieces = $derived(runs(segments));
	const pct = (time: number) => `${(Math.max(0, Math.min(DAY, time - from)) / DAY) * 100}%`;
	const widthPct = (a: number, b: number) => `${(Math.max(0, b - a) / DAY) * 100}%`;
	const hours = $derived(
		Array.from({ length: 25 }, (_, h) => h).filter((h) =>
			zoom >= 24 ? h % 3 === 0 : zoom >= 6 ? true : true,
		),
	);
	let frame = $state<HTMLElement>();
	let track = $state<HTMLElement>();

	function timeAt(clientX: number): number {
		if (!track) return from;
		const box = track.getBoundingClientRect();
		return from + Math.max(0, Math.min(1, (clientX - box.left) / box.width)) * DAY;
	}
	function key(event: KeyboardEvent) {
		const step = event.shiftKey ? 600 : 30;
		if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
			event.preventDefault();
			onpick(
				Math.max(
					from,
					Math.min(from + DAY - 1, (at ?? from) + (event.key === 'ArrowLeft' ? -step : step)),
				),
			);
		}
	}
	// A wider line keeps the playhead in view.
	$effect(() => {
		void zoom;
		if (!frame || !track || at === null) return;
		const x = ((at - from) / DAY) * track.clientWidth;
		if (x < frame.scrollLeft || x > frame.scrollLeft + frame.clientWidth)
			frame.scrollLeft = Math.max(0, x - frame.clientWidth / 2);
	});
</script>

<div class="timeline" bind:this={frame}>
	<div class="inner" style:width="{(24 / zoom) * 100}%">
		<div
			class="track"
			bind:this={track}
			role="slider"
			tabindex="0"
			aria-label={t('nvr.rec.timeline')}
			aria-valuemin={0}
			aria-valuemax={DAY}
			aria-valuenow={at === null ? 0 : Math.round(at - from)}
			aria-valuetext={at === null ? t('nvr.rec.noTime') : clock(at, locale, true)}
			onclick={(e) => onpick(timeAt(e.clientX))}
			onkeydown={key}
		>
			{#each pieces as piece (piece.start)}
				<span
					class="piece {piece.kind}"
					style:left={pct(piece.start)}
					style:width={widthPct(piece.start, piece.end)}
				></span>
			{/each}
			{#if range}
				<span
					class="range"
					style:left={pct(range.from)}
					style:width={widthPct(range.from, range.to)}
				></span>
			{/if}
			{#each events as event (event.id)}
				<span
					class="event"
					style:left={pct(event.start)}
					title="{labelName(event.label)} · {clock(event.start, locale)}"
				></span>
			{/each}
			{#if at !== null}<span class="head" style:left={pct(at)}></span>{/if}
		</div>
		<div class="scale" aria-hidden="true">
			{#each hours as h (h)}
				<span style:left="{(h / 24) * 100}%" class:last={h === 24}
					>{String(h).padStart(2, '0')}</span
				>
			{/each}
		</div>
	</div>
</div>

<style>
	.timeline {
		overflow-x: auto;
		scrollbar-width: thin;
		padding-bottom: 2px;
		min-width: 0;
	}
	.inner {
		min-width: 100%;
	}
	.track {
		position: relative;
		height: 44px;
		border: 1px solid var(--border);
		border-radius: 8px;
		background: var(--track, var(--surface));
		cursor: pointer;
		overflow: hidden;
	}
	.track:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.piece {
		position: absolute;
		top: 0;
		bottom: 0;
		min-width: 1px;
		background: color-mix(in srgb, var(--accent) 35%, transparent);
	}
	.piece.motion {
		background: color-mix(in srgb, var(--accent) 70%, transparent);
	}
	.piece.objects {
		background: #1f9d6b;
	}
	.range {
		position: absolute;
		top: 0;
		bottom: 0;
		border: 2px solid var(--text);
		border-radius: 4px;
		pointer-events: none;
	}
	.event {
		position: absolute;
		top: 2px;
		width: 8px;
		height: 8px;
		margin-left: -4px;
		border-radius: 50%;
		background: #f79009;
		box-shadow: 0 0 0 1px var(--surface);
		pointer-events: none;
	}
	.head {
		position: absolute;
		top: 0;
		bottom: 0;
		width: 2px;
		margin-left: -1px;
		background: var(--text);
		pointer-events: none;
	}
	.scale {
		position: relative;
		height: 18px;
		margin-top: 2px;
		color: var(--muted);
		font-size: var(--text-xs, 0.72rem);
	}
	.scale span {
		position: absolute;
		transform: translateX(-50%);
	}
	.scale span:first-child {
		transform: none;
	}
	.scale span.last {
		transform: translateX(-100%);
	}
</style>
