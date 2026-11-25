<script lang="ts">
	// One chat of the KI-Agent: the person's messages, the steps and thoughts
	// of each turn folded into one line ("✓ 5 Schritte · 6,1 s … Anzeigen"),
	// the answers (safe Markdown, tables in a card) with Vorlesen, Kopieren
	// and what the agent remembered, questions with buttons, the banner of a
	// step that waits for approval and, while it runs, what streams in.
	// The parts are in ./chat/ (1.01); this view keeps the order of the
	// lines, the scrolling and the sheets.
	import { tick, untrack } from 'svelte';
	import { blocks, type Chat, type Entry, type Pending } from '#lib/agent.ts';
	import AnswerBlock from './chat/AnswerBlock.svelte';
	import ApprovalCard from './chat/ApprovalCard.svelte';
	import DraftCard from './chat/DraftCard.svelte';
	import ChatFiles from './chat/ChatFiles.svelte';
	import ChatFind from './chat/ChatFind.svelte';
	import LiveLine from './chat/LiveLine.svelte';
	import MessageSheet from './chat/MessageSheet.svelte';
	import NoteLine from './chat/NoteLine.svelte';
	import PlanBox from './chat/PlanBox.svelte';
	import StepsLine from './chat/StepsLine.svelte';
	import StepsSheet from './chat/StepsSheet.svelte';
	import UserMessage from './chat/UserMessage.svelte';
	import { Copied, type StepsBlock } from './chat/chat-text.svelte.ts';

	let {
		chat,
		live,
		busy = false,
		deciding = false,
		ondecide,
		onedit,
		onchoice,
		onretry,
		onundo,
		readonly = false,
		showSteps = false,
		onplanned,
		onresend,
		onremember,
		ontemplate,
		jump = '',
		find = $bindable(null),
		phone = false,
		calm = false,
	}: {
		/**
		 * PLAN 90-4: asks a message of the person again – with a text it was
		 * changed ("Bearbeiten und erneut senden"), without one the same is
		 * asked again ("Neu erzeugen"). Everything after it leaves the chat.
		 */
		onresend?: (entry: Entry, text?: string) => void;
		/** "Das merken": writes the text into the memory. */
		onremember?: (text: string) => void;
		ontemplate?: (text: string) => void;
		jump?: string;
		/** The search in this chat: null is closed, else what is looked for. */
		find?: string | null;
		phone?: boolean;
		calm?: boolean;
		/** Opens the overview of planned tasks (from a confirmation). */
		onplanned?: () => void;
		/** Another account's chat in a shared project: nothing to answer. */
		readonly?: boolean;
		/** Open thinking and tool steps by default (PLAN 53). */
		showSteps?: boolean;
		chat: Chat;
		live: { thinking: string; content: string; step: Entry | null };
		busy?: boolean;
		deciding?: boolean;
		ondecide: (pending: Pending, approve: boolean) => void;
		/** 1.13: a waiting step goes into the program's editor instead. */
		onedit?: (pending: Pending) => void;
		onchoice: (choice: string) => void;
		onretry?: () => void;
		onundo?: (step: Entry) => void;
	} = $props();
	const undone = $derived(
		new Set(
			chat.entries
				.filter((e) => e.note === 'undone')
				.map((e) => (e.tool ?? '') + '|' + (e.content ?? '')),
		),
	);
	/** Kopieren gives its feedback at one text at a time. */
	const copied = new Copied();

	// ----- PLAN 90-4: change a message, ask again -----
	const canChange = $derived(!!onresend && !readonly && !busy);
	const lastUser = $derived(chat.entries.findLast((e) => e.role === 'user'));
	let editing = $state<{ id: string; text: string } | null>(null);
	// On the phone a long press on the own message opens its actions.
	let sheet = $state<Entry | null>(null);

	// ----- the steps of a turn: one line, a sheet with each step -----
	let stepsOpen = $state<StepsBlock | null>(null);
	/** A step taken back closes the sheet first. */
	const undo = $derived(
		onundo
			? (step: Entry) => {
					stepsOpen = null;
					onundo(step);
				}
			: undefined,
	);

	let column = $state<HTMLDivElement>();
	let scroller = $state<HTMLDivElement>();
	let pinned = $state(true);
	const list = $derived(blocks(chat.entries));
	// The cards of a turn stand under its last answer: all steps since the
	// person's message.
	function turnEnds(i: number): boolean {
		for (let j = i + 1; j < list.length; j++) {
			if (list[j].kind === 'user') return true;
			if (list[j].kind === 'answer') return false;
		}
		return true;
	}
	function turnSteps(i: number): Entry[] {
		const steps: Entry[] = [];
		for (let j = i - 1; j >= 0 && list[j].kind !== 'user'; j--) {
			const b = list[j];
			if (b.kind === 'steps') steps.unshift(...b.steps);
		}
		return steps;
	}
	// Only the last answer offers its buttons; older questions are answered.
	const lastAnswer = $derived.by(() => {
		for (let i = list.length - 1; i >= 0; i--) {
			const b = list[i];
			if (b.kind === 'user') return '';
			if (b.kind === 'answer') return b.entry.id;
		}
		return '';
	});

	// Owner 16 Nov 2026: reading further up while the answer is still being
	// written. Any move upwards is the person's (the view itself only ever
	// moves down), so it lets go at once – before, the view pulled back as
	// long as one was within 80 px of the end, which a finger or a slow
	// wheel never left. Back at the end, it follows again.
	let lastTop = 0;
	// 1.14 (owner 19 Nov 2026: "Bei mehreren Nachrichten sind die Punkte
	// wieder zu weit unten"): only a move of the person lets go – a wheel,
	// a finger (with the glide after it), a key, the scroll bar. The view
	// also moves up when the browser clamps it (content got shorter, the
	// chat got taller) or iOS pans for the keyboard; that used to let go
	// too, and the working line then grew under the edge.
	let moved = 0;
	const GLIDE = 1500;
	function person() {
		moved = performance.now();
	}
	let touching = false;
	function byPerson() {
		return touching || performance.now() - moved < GLIDE;
	}
	function scrolled() {
		if (!scroller) return;
		const top = scroller.scrollTop;
		const below = scroller.scrollHeight - top - scroller.clientHeight;
		if (top < lastTop - 1 && below > 2 && byPerson()) pinned = false;
		else if (below < 24) pinned = true;
		lastTop = top;
	}
	// Another chat starts at its end, whatever was scrolled in the last one.
	let shownChat = '';
	$effect(() => {
		const id = chat.id;
		if (id === untrack(() => shownChat)) return;
		shownChat = id;
		pinned = true;
		lastTop = 0;
		ownSent = untrack(() => chat.entries.filter((e) => e.role === 'user').length);
	});
	// An explicit search target wins over the initial scroll to the last turn.
	$effect(() => {
		const entry = jump,
			id = chat.id,
			el = column;
		if (!entry || !id || !el) return;
		let active = true;
		void tick().then(() => {
			if (!active) return;
			const target = el.querySelector<HTMLElement>('#agent-entry-' + CSS.escape(entry));
			if (target) {
				pinned = false;
				target.focus({ preventScroll: true });
				target.scrollIntoView({ block: 'center' });
			}
		});
		return () => {
			active = false;
		};
	});
	// A message of one's own shows at the end again.
	let ownSent = 0;
	$effect(() => {
		const sent = chat.entries.filter((e) => e.role === 'user').length;
		if (sent > untrack(() => ownSent)) pinned = true;
		ownSent = sent;
	});
	// Stay at the bottom while text streams in, unless the person scrolled up.
	function toEnd() {
		if (!pinned || !scroller) return;
		// A move upwards whose scroll event has not come yet (a fast
		// stream) is the person's too: do not pull back over it.
		const below = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight;
		if (scroller.scrollTop < lastTop - 1 && below > 2 && byPerson()) {
			pinned = false;
			lastTop = scroller.scrollTop;
			return;
		}
		scroller.scrollTo({ top: scroller.scrollHeight });
	}
	$effect(() => {
		void chat.entries.length;
		void live.content;
		void live.thinking;
		void live.step;
		void busy;
		void chat.pending;
		if (pinned && scroller) queueMicrotask(toEnd);
	});
	// 1.01 (owner 16 Nov 2026: after a follow-up question the working line
	// stood half under the edge): whatever makes the chat taller – the
	// working line coming in after one's own message, a card, an image that
	// loads, an animation – brings the end into view while it is followed.
	// 1.14: the chat itself changing its height counts too – on the phone
	// the keyboard closes after sending and the bar below comes back.
	$effect(() => {
		const el = column;
		const view = scroller;
		if (!el || !view || typeof ResizeObserver === 'undefined') return;
		const watch = new ResizeObserver(() => toEnd());
		watch.observe(el);
		watch.observe(view);
		return () => watch.disconnect();
	});
	// Keys that move the view are the person's (in an input they type).
	function key(e: KeyboardEvent) {
		if (['ArrowUp', 'ArrowDown', 'PageUp', 'PageDown', 'Home', 'End', ' '].includes(e.key))
			person();
	} // The moves of the person, passive (they never stop the scroll).
	$effect(() => {
		const el = scroller;
		if (!el) return;
		const down = () => ((touching = true), person());
		const up = () => ((touching = false), person());
		const on: [string, EventListener][] = [
			['wheel', person],
			['pointerdown', person],
			['keydown', key as EventListener],
			['touchstart', down],
			['touchmove', person],
			['touchend', up],
			['touchcancel', up],
		];
		for (const [type, fn] of on) el.addEventListener(type, fn, { passive: true });
		return () => {
			for (const [type, fn] of on) el.removeEventListener(type, fn);
		};
	});

	// What the agent remembered right before an answer goes into the small
	// row under that answer (PLAN 68), not as a block of its own.
	const LEARN = ['memory', 'note', 'skill', 'schedule', 'homeassistant', 'album'];
	const isLearn = (b: (typeof list)[number]) =>
		b.kind === 'note' &&
		LEARN.includes(b.entry.note ?? '') &&
		!(b.entry.note === 'schedule' && b.entry.content?.includes('„'));
	const learnedBefore = $derived.by(() => {
		const by = new Map<number, Entry[]>();
		const taken = new Set<string>();
		list.forEach((b, i) => {
			if (b.kind !== 'answer') return;
			const notes: Entry[] = [];
			for (let j = i - 1; j >= 0; j--) {
				const prev = list[j];
				if (prev.kind === 'note' && isLearn(prev)) notes.unshift(prev.entry);
				else break;
			}
			if (notes.length) {
				by.set(i, notes);
				for (const n of notes) taken.add(n.id);
			}
		});
		return { by, taken };
	});
</script>

<svelte:window
	onkeydown={(e) => {
		const full = e.key === 'Escape' ? column?.querySelector('.code.full') : null;
		if (!full) return;
		e.stopPropagation();
		full.querySelector<HTMLButtonElement>('.code-tools button:last-child')?.click();
	}}
/>

<div class="messages" bind:this={scroller} onscroll={scrolled}>
	{#if find !== null}
		<ChatFind bind:find {column} {scroller} version={chat.entries.length} />
	{/if}
	<div class="column" bind:this={column}>
		{#each list as block, i (block.kind === 'steps' ? `s${i}` : block.entry.id)}
			{#if block.kind === 'user'}
				<UserMessage
					entry={block.entry}
					bind:editing
					{ontemplate}
					{phone}
					{canChange}
					{copied}
					onhold={(entry) => (sheet = entry)}
					onresend={(entry, text) => onresend?.(entry, text)}
				/>
			{:else if block.kind === 'steps'}
				{#if block.steps.length || block.thinking}
					<StepsLine
						{block}
						{showSteps}
						{busy}
						{undone}
						onopen={() => (stepsOpen = block)}
						onundo={undo}
					/>
				{/if}
				<!-- 1.13: drafts of "Nur Vorschläge" and of steps taken into an editor. -->
				{#each block.steps.filter((x) => x.draft) as step (step.id)}
					{#if step.draft}<DraftCard
							draft={step.draft}
							chatId={chat.id}
							entryId={step.id}
							{readonly}
						/>{/if}
				{/each}
			{:else if block.kind === 'answer'}
				{@const last = block.entry.id === lastAnswer}
				<AnswerBlock
					entry={block.entry}
					cards={turnEnds(i) ? turnSteps(i) : null}
					active={last && !busy && !readonly && !chat.pending}
					learned={learnedBefore.by.get(i) ?? []}
					{copied}
					{onchoice}
					onregenerate={canChange && lastUser && last && block.entry.content
						? () => onresend?.(lastUser)
						: undefined}
				/>
			{:else if block.kind === 'note'}
				{#if !learnedBefore.taken.has(block.entry.id)}
					<NoteLine
						entry={block.entry}
						learn={isLearn(block)}
						last={i === list.length - 1}
						{onretry}
						{onplanned}
					/>
				{/if}
			{/if}
		{/each}

		<ChatFiles entries={chat.entries} />

		{#if chat.plan?.length}
			<PlanBox plan={chat.plan} />
		{/if}

		<LiveLine {chat} {live} {busy} {calm} />

		{#if chat.pending && !busy && !readonly}
			<ApprovalCard pending={chat.pending} {deciding} {ondecide} {onedit} />
		{/if}
	</div>
</div>

{#if sheet}
	{@const shown = sheet}
	<!-- Each action takes the message before the sheet goes (`shown` follows it). -->
	<MessageSheet
		entry={shown}
		{calm}
		{canChange}
		last={shown.id === lastUser?.id}
		onclose={() => (sheet = null)}
		onedit={() => {
			const entry = shown;
			sheet = null;
			editing = { id: entry.id, text: entry.content ?? '' };
		}}
		oncopy={() => {
			const entry = shown;
			sheet = null;
			void copied.copy(entry.id, entry.content ?? '');
		}}
		onregenerate={() => {
			const entry = shown;
			sheet = null;
			onresend?.(entry);
		}}
		onfind={() => {
			sheet = null;
			find = '';
		}}
		ontemplate={ontemplate && shown.role === 'user'
			? () => {
					sheet = null;
					ontemplate(shown.content ?? '');
				}
			: undefined}
		onremember={onremember
			? () => {
					const text = shown.content ?? '';
					sheet = null;
					onremember(text);
				}
			: undefined}
	/>
{/if}

{#if stepsOpen}
	<StepsSheet
		block={stepsOpen}
		{calm}
		{busy}
		{undone}
		onclose={() => (stepsOpen = null)}
		{onundo}
	/>
{/if}

<style>
	/* PLAN 68: the chat in the design language of TitanOS – at most 760 px
	   wide in the middle, the person's message a bubble on the right tinted
	   with the accent, the answer plain text, steps one line, tables in a
	   card, the approval one banner with a warning frame. The parts bring
	   their own styles (./chat/). */
	.messages {
		flex: 1;
		overflow-y: auto;
		padding: 20px var(--space-5);
		overscroll-behavior: contain;
	}
	.column {
		max-width: var(--ag-chat-width, 760px);
		margin: 0 auto;
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		font-size: 0.9375rem;
	}
	@media (max-width: 760px) {
		/* The round buttons of the head float over the chat. */
		.messages {
			/* A title on two or three lines makes the head taller (--agent-head). */
			padding: max(calc(var(--agent-top, 0px) + 64px), calc(var(--agent-head, 0px) + 8px))
				var(--space-4) var(--space-3);
		}
		.column {
			font-size: 1rem;
			gap: var(--space-3-5, 14px);
		}
	}
</style>
