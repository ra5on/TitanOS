<script lang="ts">
	import { tick, untrack } from 'svelte';
	import AgentDots from './AgentDots.svelte';
	import type { Cue } from './guide.ts';
	let {
		cue,
		mode,
		reduced,
	}: { cue: Cue | null; mode: 'full' | 'subtle' | 'off'; reduced: boolean } = $props();
	let guide = $state<HTMLElement>();
	let showing = $state(false);
	$effect(() => {
		const root = guide?.parentElement,
			el = guide,
			current = cue;
		if (!current || !el || !root || mode === 'off' || reduced || !el.animate) {
			showing = false;
			return;
		}
		let cancelled = false;
		const animations: Animation[] = [];
		const subtle = mode === 'subtle';
		async function follow() {
			await tick();
			if (cancelled || !current || !el || !root) return;
			const origin = root.getBoundingClientRect();
			const points = current.stops
				.map((id) =>
					[...root.querySelectorAll<HTMLElement>('[data-guide]')]
						.find((n) => n.dataset.guide === id && n.getClientRects().length)
						?.getBoundingClientRect(),
				)
				.filter((p) => p && p.width && p.height);
			if (!points.length) return;
			const half = el.offsetWidth / 2;
			const frames = points.map((p) => ({
				transform:
					'translate3d(' +
					(p!.left + p!.width / 2 - origin.left - half) +
					'px,' +
					(p!.top + p!.height / 2 - origin.top - half) +
					'px,0)',
			}));
			if (current.origin && !subtle) {
				const frame = {
					transform:
						'translate3d(' +
						(current.origin.x - half) +
						'px,' +
						(current.origin.y - half) +
						'px,0)',
				};
				if (frame.transform !== frames[0].transform) frames.unshift(frame);
			}
			showing = true;
			const final = frames.at(-1)!;
			if (!subtle && frames.length > 1) {
				for (const [i, trail] of [...el.querySelectorAll<HTMLElement>('.trail')].entries()) {
					const delayed = trail.animate(
						[{ opacity: 0 }, { opacity: 0.55 - i * 0.1 }, { opacity: 0 }],
						{ duration: Math.min(1100, points.length * 195), delay: i * 52 },
					);
					animations.push(delayed);
				}
			}
			const flight = el.animate(
				subtle
					? [
							{ ...final, opacity: 0 },
							{ ...final, opacity: 1 },
						]
					: frames.length === 1
						? [
								{ ...final, opacity: 0 },
								{ ...final, opacity: 1 },
							]
						: frames,
				{
					duration: subtle ? 160 : Math.min(1150, Math.max(250, frames.length * 190)),
					easing: 'cubic-bezier(.22,.8,.27,1)',
					fill: 'forwards',
				},
			);
			animations.push(flight);
			try {
				await flight.finished;
				if (cancelled) return;
				if (!subtle) {
					const dest = current.stops.at(-1);
					const target = [...root.querySelectorAll<HTMLElement>('[data-guide]')].find(
						(n) => n.dataset.guide === dest && n.getClientRects().length,
					);
					if (target)
						animations.push(
							target.animate(
								[
									{ boxShadow: 'inset 0 0 0 2px var(--pa-trail)' },
									{ boxShadow: 'inset 0 0 0 0px transparent' },
								],
								{ duration: 440 },
							),
						);
				}
				if (current.hold) return;
				const fade = el.animate([{ opacity: 1 }, { opacity: 0 }], {
					duration: 200,
					fill: 'forwards',
				});
				animations.push(fade);
				await fade.finished;
				if (!cancelled) showing = false;
			} catch {
				/* navigation replaces a running flight */
			}
		}
		void untrack(follow);
		return () => {
			cancelled = true;
			for (const a of animations) a.cancel();
			showing = false;
		};
	});
</script>

<span bind:this={guide} class="guide" class:active={showing} aria-hidden="true"
	><i class="trail"></i><i class="trail second"></i><AgentDots
		motif={cue?.motif || ''}
		calm={!showing || mode !== 'full' || reduced}
	/></span
>

<style>
	.guide {
		position: absolute;
		left: 0;
		top: 0;
		display: grid;
		place-items: center;
		width: var(--ag-dot-size, 22px);
		height: var(--ag-dot-size, 22px);
		z-index: 9;
		pointer-events: none;
		visibility: hidden;
		filter: drop-shadow(0 0 5px var(--pa-trail));
		will-change: transform;
	}
	.trail {
		position: absolute;
		width: 42%;
		height: 14%;
		background: var(--pa-trail);
		border-radius: 100%;
		opacity: 0;
		transform: translate(-85%, 25%);
	}
	.trail.second {
		transform: translate(-145%, 25%);
		width: 32%;
	}
	.guide.active {
		visibility: visible;
	}
	@media (prefers-reduced-motion: reduce) {
		.guide {
			display: none;
		}
	}
</style>
