<script lang="ts" module>
	// 1.13 (Baustand step 4): sorting the entries of a side bar – the
	// folders of Mail, the address books of Kontakte, the calendars of
	// Kalender. A grip in each entry: dragged with the pointer (mouse, pen
	// or finger) among the entries of its group, or moved with the arrow
	// keys while it has the focus (Home/End: first/last). On the entry
	// itself Alt+↑/↓ does the same (sortKey). What moved is said through
	// the frame's status line (SortStatus).
	//
	// The entries of a group carry data-sort-group and data-sort-key on the
	// element that stands for them (a folder's line); the grip finds its
	// siblings through them below the nearest [data-sort-list]. Entries of a
	// group need not stand next to each other (subfolders between them).
	import { tick } from 'svelte';
	import { t } from '../i18n/index.svelte.ts';
	import { moveKey } from '../side-order.svelte.ts';

	export const sortStatus = $state({ text: '' });

	/** Says what moved (the same words twice are said twice). */
	export function announceSort(name: string, keys: string[], key: string) {
		const text = t('ui.sort.moved', {
			name,
			pos: keys.indexOf(key) + 1,
			count: keys.length,
		});
		sortStatus.text = sortStatus.text === text ? text + ' ' : text;
	}

	/** The new order for a key of the sorting keys, or null for any other key. */
	export function sortStep(e: KeyboardEvent, keys: string[], key: string): string[] | null {
		const at = keys.indexOf(key);
		if (at < 0) return null;
		const to =
			e.key === 'ArrowUp'
				? at - 1
				: e.key === 'ArrowDown'
					? at + 1
					: e.key === 'Home'
						? 0
						: e.key === 'End'
							? keys.length - 1
							: null;
		if (to === null) return null;
		return moveKey(keys, key, to);
	}

	/**
	 * Alt+↑/↓ on an entry (its button): moves it and says so. Returns
	 * whether the key was taken.
	 */
	export function sortKey(
		e: KeyboardEvent,
		keys: string[],
		key: string,
		name: string,
		onmove: (keys: string[]) => void,
	): boolean {
		if (!e.altKey || (e.key !== 'ArrowUp' && e.key !== 'ArrowDown')) return false;
		e.preventDefault();
		const next = sortStep(e, keys, key);
		if (next && next !== keys) {
			onmove(next);
			announceSort(name, next, key);
			const el = e.currentTarget instanceof HTMLElement ? e.currentTarget : null;
			// Moving a node takes the focus away in some browsers: back to it.
			void tick().then(() => el?.isConnected && el.focus());
		}
		return true;
	}
</script>

<script lang="ts">
	import Icon from './Icon.svelte';

	type Props = {
		/** The group's name (a mailbox, a parent folder; '' for a flat list). */
		group: string;
		/** This entry's key. */
		key: string;
		/** The keys of the group in the order shown. */
		keys: string[];
		/** The entry's name, for the label and what is said. */
		name: string;
		onmove: (keys: string[]) => void;
	};
	let { group, key, keys, name, onmove }: Props = $props();

	let button = $state<HTMLButtonElement>();

	function move(next: string[]) {
		if (next === keys || next.join('\u0000') === keys.join('\u0000')) return;
		onmove(next);
		announceSort(name, next, key);
		void tick().then(() => button?.isConnected && button.focus());
	}

	function keydown(e: KeyboardEvent) {
		if (drag && e.key === 'Escape') {
			e.preventDefault();
			stop(false);
			return;
		}
		const next = sortStep(e, keys, key);
		if (!next) return;
		e.preventDefault();
		move(next);
	}

	// Dragging: the entry follows the pointer as a line where it will land.
	type Drag = {
		id: number;
		y0: number;
		moving: boolean;
		row: HTMLElement;
		others: HTMLElement[];
		to: number;
		scroller: HTMLElement | null;
	};
	let drag = $state<Drag | null>(null);
	const sel = (s: string) => (typeof CSS !== 'undefined' && CSS.escape ? CSS.escape(s) : s);

	function down(e: PointerEvent) {
		if (e.button !== 0 || !button) return;
		const row = button.closest<HTMLElement>('[data-sort-key]');
		const list = button.closest<HTMLElement>('[data-sort-list]');
		if (!row || !list) return;
		e.preventDefault();
		button.focus();
		const others = [
			...list.querySelectorAll<HTMLElement>(`[data-sort-group="${sel(group)}"][data-sort-key]`),
		].filter((el) => el !== row);
		let scroller: HTMLElement | null = list;
		while (scroller && scroller.scrollHeight <= scroller.clientHeight)
			scroller = scroller.parentElement;
		drag = {
			id: e.pointerId,
			y0: e.clientY,
			moving: false,
			row,
			others,
			to: keys.indexOf(key),
			scroller,
		};
		button.setPointerCapture?.(e.pointerId);
	}
	function clearMarks(d: Drag) {
		for (const el of d.others) el.removeAttribute('data-sort-drop');
	}
	function over(e: PointerEvent) {
		const d = drag;
		if (!d || e.pointerId !== d.id) return;
		if (!d.moving && Math.abs(e.clientY - d.y0) < 4) return;
		d.moving = true;
		d.row.setAttribute('data-sort-moving', '');
		// Near the edge of the scrolling column it scrolls along.
		const box = d.scroller?.getBoundingClientRect();
		if (d.scroller && box) {
			if (e.clientY < box.top + 32) d.scroller.scrollTop -= 12;
			else if (e.clientY > box.bottom - 32) d.scroller.scrollTop += 12;
		}
		// The place among the others: before the first whose middle is below.
		let to = d.others.length;
		for (let i = 0; i < d.others.length; i++) {
			const r = d.others[i].getBoundingClientRect();
			if (e.clientY < r.top + r.height / 2) {
				to = i;
				break;
			}
		}
		d.to = to;
		clearMarks(d);
		if (to < d.others.length) d.others[to].setAttribute('data-sort-drop', 'before');
		else d.others.at(-1)?.setAttribute('data-sort-drop', 'after');
	}
	function stop(take: boolean) {
		const d = drag;
		if (!d) return;
		drag = null;
		clearMarks(d);
		d.row.removeAttribute('data-sort-moving');
		if (take && d.moving) move(moveKey(keys, key, d.to));
	}
</script>

<button
	type="button"
	class="sort-grip"
	class:active={!!drag?.moving}
	bind:this={button}
	aria-label={t('ui.sort.move', { name })}
	aria-keyshortcuts="ArrowUp ArrowDown Home End"
	title={t('ui.sort.hint')}
	onpointerdown={down}
	onpointermove={over}
	onpointerup={() => stop(true)}
	onpointercancel={() => stop(false)}
	onlostpointercapture={() => stop(true)}
	onkeydown={keydown}><Icon name="grip" size={14} /></button
>

<style>
	.sort-grip {
		flex: none;
		display: inline-grid;
		place-items: center;
		width: 22px;
		height: 28px;
		padding: 0;
		border: 0;
		border-radius: var(--radius-small);
		background: none;
		color: var(--muted);
		cursor: grab;
		touch-action: none;
	}
	.sort-grip:hover,
	.sort-grip:focus-visible,
	.sort-grip.active {
		background: var(--chip);
		color: var(--text);
	}
	.sort-grip.active {
		cursor: grabbing;
	}
	.sort-grip:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	@media (pointer: coarse) {
		.sort-grip {
			width: 36px;
			height: 44px;
		}
	}
	/* Where the entry lands, and the entry on its way (the group's elements). */
	:global([data-sort-drop='before']) {
		box-shadow: inset 0 2px 0 var(--accent);
	}
	:global([data-sort-drop='after']) {
		box-shadow: inset 0 -2px 0 var(--accent);
	}
	:global([data-sort-moving]) {
		opacity: 0.55;
	}
</style>
