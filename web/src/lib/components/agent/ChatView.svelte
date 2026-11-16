<script lang="ts">
	// One chat of the KI-Agent: the person's messages, the steps and thoughts
	// of each turn folded into one line ("✓ 5 Schritte · 6,1 s … Anzeigen"),
	// the answers (safe Markdown, tables in a card) with Vorlesen, Kopieren
	// and what the agent remembered, questions with buttons, the banner of a
	// step that waits for approval and, while it runs, what streams in.
	import { onMount, untrack } from 'svelte';
	import { fly } from 'svelte/transition';
	import { quintOut } from 'svelte/easing';
	import Icon from '#lib/components/Icon.svelte';
	import BottomSheet from './BottomSheet.svelte';
	import ResultCards from './ResultCards.svelte';
	import BusyFigure from './BusyFigure.svelte';
	import { appForFile, loadWebApps, webAppById } from '#lib/webapps/webapps.svelte.ts';
	import { openInWebApp } from '#lib/webapps/open.ts';
	import { chatMarkdown } from '#lib/chat-markdown.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { canSpeak, speak, speech } from '#lib/agent-speech.svelte.ts';
	import { i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		approvalTitle,
		blocks,
		busyKind,
		stepDetail,
		stepFailed,
		stepWord,
		type Chat,
		type Entry,
		type Pending,
	} from '#lib/agent.ts';

	let {
		chat,
		live,
		busy = false,
		deciding = false,
		ondecide,
		onchoice,
		onretry,
		onundo,
		readonly = false,
		showSteps = false,
		onplanned,
		onresend,
		onremember,
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
		onchoice: (choice: string) => void;
		onretry?: () => void;
		onundo?: (step: Entry) => void;
	} = $props();
	// Steps TitanOS can take back (agent_extra.go undoActions).
	const UNDOABLE = ['file_write', 'files_delete', 'ha_call'];
	const undone = $derived(
		new Set(
			chat.entries
				.filter((e) => e.note === 'undone')
				.map((e) => (e.tool ?? '') + '|' + (e.content ?? '')),
		),
	);

	// ----- PLAN 90-4: change a message, ask again -----
	const canChange = $derived(!!onresend && !readonly && !busy);
	const lastUser = $derived(chat.entries.findLast((e) => e.role === 'user'));
	let editing = $state<{ id: string; text: string } | null>(null);
	function resend(entry: Entry) {
		const text = editing?.text.trim() ?? '';
		if (!text) return;
		editing = null;
		onresend?.(entry, text);
	}
	function editArea(node: HTMLTextAreaElement) {
		node.focus();
		node.setSelectionRange(node.value.length, node.value.length);
	}
	// On the phone a long press on the own message opens its actions.
	let sheet = $state<Entry | null>(null);
	function hold(node: HTMLElement, entry: Entry) {
		let timer: ReturnType<typeof setTimeout> | undefined;
		let at = { x: 0, y: 0 };
		const clear = () => clearTimeout(timer);
		const down = (e: PointerEvent) => {
			if (e.pointerType !== 'touch') return;
			at = { x: e.clientX, y: e.clientY };
			clear();
			timer = setTimeout(() => (sheet = entry), 500);
		};
		const move = (e: PointerEvent) => {
			if (Math.hypot(e.clientX - at.x, e.clientY - at.y) > 8) clear();
		};
		const menu = (e: Event) => {
			if (!phone) return;
			e.preventDefault();
			clear();
			sheet = entry;
		};
		node.addEventListener('pointerdown', down);
		node.addEventListener('pointermove', move);
		node.addEventListener('pointerup', clear);
		node.addEventListener('pointercancel', clear);
		node.addEventListener('contextmenu', menu);
		return {
			destroy() {
				clear();
				node.removeEventListener('pointerdown', down);
				node.removeEventListener('pointermove', move);
				node.removeEventListener('pointerup', clear);
				node.removeEventListener('pointercancel', clear);
				node.removeEventListener('contextmenu', menu);
			},
		};
	}

	// ----- PLAN 90-4: search in this chat -----
	// The hits are marked with the CSS Custom Highlight API, which leaves the
	// page's elements alone. A browser without it still counts the hits and
	// scrolls to them, only without the marks.
	let column = $state<HTMLDivElement>();
	let hits = $state.raw<Range[]>([]);
	let hit = $state(0);
	type Highlights = { set: (name: string, value: unknown) => void; delete: (name: string) => void };
	const highlights = (): Highlights | null =>
		typeof CSS !== 'undefined' && 'highlights' in CSS && typeof Highlight !== 'undefined'
			? (CSS as unknown as { highlights: Highlights }).highlights
			: null;
	$effect(() => {
		const q = (find ?? '').trim().toLowerCase();
		void chat.entries.length;
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

	// ----- tables and code: a button that copies them -----
	// A table stays a table on the phone too (owner 15 Nov 2026: like the
	// chat apps); one that is wider than the screen scrolls sideways.
	function tableText(table: HTMLTableElement): string {
		return [...table.rows]
			.map((r) => [...r.cells].map((c) => (c.textContent ?? '').trim()).join('\t'))
			.join('\n');
	}
	function copyButton(label: string, text: () => string): HTMLButtonElement {
		const button = document.createElement('button');
		button.type = 'button';
		button.textContent = label;
		button.addEventListener('click', () => {
			void copy('block', text()).then(() => {
				button.textContent = copied?.ok ? t('agent.copiedShort') : t('agent.copyFailed');
				setTimeout(() => (button.textContent = label), 1800);
			});
		});
		return button;
	}
	$effect(() => {
		void list;
		void live.content;
		if (!column) return;
		for (const wrap of column.querySelectorAll<HTMLElement>('.md .table:not([data-done])')) {
			const table = wrap.querySelector('table');
			if (!table) continue;
			wrap.dataset.done = 'true';
			const foot = document.createElement('div');
			foot.className = 'table-foot';
			foot.append(copyButton(t('agent.tableCopy'), () => tableText(table)));
			wrap.after(foot);
		}
		for (const head of column.querySelectorAll<HTMLElement>('.md .code-head:not([data-done])')) {
			const code = head.parentElement?.querySelector('code');
			if (!code) continue;
			head.dataset.done = 'true';
			// "Vergrößern" lays the block over the whole chat, "Verkleinern"
			// (or Escape) puts it back.
			const box = head.parentElement!;
			const grow = document.createElement('button');
			grow.type = 'button';
			grow.textContent = t('agent.codeGrow');
			grow.addEventListener('click', () => {
				const full = box.classList.toggle('full');
				grow.textContent = full ? t('agent.codeShrink') : t('agent.codeGrow');
			});
			const tools = document.createElement('span');
			tools.className = 'code-tools';
			tools.append(
				copyButton(t('agent.copy'), () => code.textContent ?? ''),
				grow,
			);
			head.append(tools);
		}
	});

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
	let doneTimer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => {
		const ended = wasBusy && !busy;
		wasBusy = busy;
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

	// ----- the steps of a turn: one line, a sheet with each step -----
	type StepsBlock = { steps: Entry[]; thinking: string };
	let stepsOpen = $state<StepsBlock | null>(null);
	let stepShown = $state<Entry | 'thinking' | null>(null);
	function stepOutput(step: Entry): string {
		const text = step.result || step.summary || '';
		return text.length > 6000 ? text.slice(0, 5999) + '…' : text;
	}

	let scroller = $state<HTMLDivElement>();
	let pinned = $state(true);
	const list = $derived(blocks(chat.entries));
	// The web app set for a file type, if one is installed (0.95): a
	// document the agent wrote opens there with one tap.
	onMount(() => void loadWebApps().catch(() => {}));
	function fileApp(name: string): { key: string; name: string } | null {
		const key = appForFile(name);
		if (!key?.startsWith('webapp:')) return null;
		const app = webAppById(key.slice(7));
		return app ? { key, name: app.name } : null;
	}
	// The cards of a turn stand under its last answer: all steps since the
	// person's message.
	function turnEnds(i: number): boolean {
		for (let j = i + 1; j < list.length; j++) {
			if (list[j].kind === 'user') return true;
			if (list[j].kind === 'answer') return false;
		}
		return true;
	}
	/** A message sent a moment ago: it comes in like the line that follows
	 *  it instead of standing there at once (0.100, look through the chat). */
	const fresh = (e: Entry) => Date.now() - Date.parse(e.at) < 4000;
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
	function scrolled() {
		if (!scroller) return;
		const top = scroller.scrollTop;
		const below = scroller.scrollHeight - top - scroller.clientHeight;
		// Content that got shorter moves the view up too: at the end it stays pinned.
		if (top < lastTop - 1 && below > 2) pinned = false;
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
	// A message of one's own shows at the end again.
	let ownSent = 0;
	$effect(() => {
		const sent = chat.entries.filter((e) => e.role === 'user').length;
		if (sent > untrack(() => ownSent)) pinned = true;
		ownSent = sent;
	});
	// Stay at the bottom while text streams in, unless the person scrolled up.
	$effect(() => {
		void chat.entries.length;
		void live.content;
		void live.thinking;
		void chat.pending;
		if (pinned && scroller)
			queueMicrotask(() => {
				if (!pinned || !scroller) return;
				// A move upwards whose scroll event has not come yet (a fast
				// stream) is the person's too: do not pull back over it.
				const below = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight;
				if (scroller.scrollTop < lastTop - 1 && below > 2) {
					pinned = false;
					lastTop = scroller.scrollTop;
					return;
				}
				scroller.scrollTo({ top: scroller.scrollHeight });
			});
	});

	function stepLabel(e: Entry): string {
		if (e.role === 'note')
			return t(
				e.note === 'verify'
					? 'agent.noteVerify'
					: e.note === 'approved'
						? 'agent.noteApproved'
						: 'agent.noteDeclined',
			);
		const word = stepWord(e.tool);
		return word ? t(`agent.step.${word}` as MessageKey) : (e.tool ?? '').replaceAll('_', ' ');
	}
	function seconds(steps: Entry[]): string {
		const ms = steps.reduce((sum, s) => sum + (s.durationMs ?? 0), 0);
		// "0,0 s" says nothing: below a tenth of a second no time is shown.
		if (ms < 100) return '';
		const n = (ms / 1000).toLocaleString(i18n.locale, {
			minimumFractionDigits: 1,
			maximumFractionDigits: 1,
		});
		return ` · ${n} s`;
	}
	/** What the steps did, in short ("Docker-Apps gelesen, VMs gelesen"). */
	function stepsGist(steps: Entry[]): string {
		const words = [...new Set(steps.filter((s) => s.role === 'tool').map(stepLabel))];
		return words.slice(0, 3).join(', ') + (words.length > 3 ? ' …' : '');
	}
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
	// Kopieren: the clipboard API needs HTTPS; on plain http in the home
	// network an invisible text field does it.
	let copied = $state<{ id: string; ok: boolean } | null>(null);
	let copyTimer: ReturnType<typeof setTimeout> | undefined;
	async function copy(id: string, text: string) {
		let ok = false;
		try {
			await navigator.clipboard.writeText(text);
			ok = true;
		} catch {
			const area = document.createElement('textarea');
			area.value = text;
			area.setAttribute('readonly', '');
			area.style.position = 'fixed';
			area.style.opacity = '0';
			document.body.appendChild(area);
			area.select();
			try {
				ok = document.execCommand('copy');
			} catch {
				ok = false;
			}
			area.remove();
		}
		copied = { id, ok };
		clearTimeout(copyTimer);
		copyTimer = setTimeout(() => (copied = null), 1800);
	}
	const noteText: Record<string, MessageKey> = {
		memory: 'agent.noteMemory',
		note: 'agent.noteNote',
		skill: 'agent.noteSkill',
		schedule: 'agent.noteSchedule',
		homeassistant: 'agent.noteHA',
		album: 'agent.noteAlbum',
		stopped: 'agent.noteStopped',
		interrupted: 'agent.noteInterrupted',
		error: 'agent.noteError',
		limit: 'agent.noteLimit',
		timelimit: 'agent.noteTimeLimit',
		loop: 'agent.noteLoop',
		undone: 'agent.noteUndone',
	};
	function askTitle(p: Pending): string {
		const plain = approvalTitle(p);
		const what = plain
			? t(`agent.ask.${plain.key}` as MessageKey, { name: plain.name })
			: p.summary || p.tool.replaceAll('_', ' ');
		return t('agent.ask.title', { what });
	}
	const levelText: Record<string, MessageKey> = {
		normal: 'agent.levelChange',
		danger: 'agent.levelDanger',
		destroy: 'agent.levelDestroy',
	};
	function argLines(args: unknown): [string, string][] {
		if (!args || typeof args !== 'object') return [];
		return Object.entries(args as Record<string, unknown>)
			.filter(([k]) => k !== 'content' && k !== 'sheets' && k !== 'code')
			.map(([k, v]) => [k, typeof v === 'string' ? v : JSON.stringify(v)] as [string, string])
			.map(([k, v]) => [k, v.length > 160 ? v.slice(0, 159) + '…' : v]);
	}
	function preview(args: unknown): string {
		if (!args || typeof args !== 'object') return '';
		// A program the agent wants to run is shown whole, however long:
		// nobody approves lines they could not read.
		const code = (args as Record<string, unknown>).code;
		if (typeof code === 'string') return code;
		const c = (args as Record<string, unknown>).content;
		return typeof c === 'string' ? (c.length > 600 ? c.slice(0, 599) + '…' : c) : '';
	}
</script>

{#snippet stepBody(step: Entry | null, chevron: boolean)}
	<span class="mark"
		><Icon
			name={!step ? 'bulb' : stepFailed(step) ? 'close' : step.role === 'note' ? 'shield' : 'check'}
			size={16}
		/></span
	>
	<span class="what"
		>{step ? stepLabel(step) : t('agent.thoughts')}{#if step && stepDetail(step.args)}<small
				>{stepDetail(step.args)}</small
			>{/if}</span
	>
	{#if step && stepFailed(step)}<span class="sr-only">{t('agent.stepFailed')}</span>{/if}
	{#if step && (step.durationMs ?? 0) >= 100}<span class="time"
			>{((step.durationMs ?? 0) / 1000).toFixed(1).replace('.', ',')} s</span
		>{/if}
	{#if chevron}<Icon name="chevronRight" size={15} />{/if}
{/snippet}

{#snippet stepList(block: StepsBlock, tappable: boolean)}
	<ol class="step-list">
		{#if block.thinking}
			<li>
				{#if tappable}
					<button type="button" class="step" onclick={() => (stepShown = 'thinking')}
						>{@render stepBody(null, true)}</button
					>
				{:else}
					<div class="step">{@render stepBody(null, false)}</div>
				{/if}
			</li>
		{/if}
		{#each block.steps as step (step.id)}
			<li class:failed={stepFailed(step)}>
				{#if tappable}
					<button type="button" class="step" onclick={() => (stepShown = step)}
						>{@render stepBody(step, true)}</button
					>
				{:else}
					<div class="step">{@render stepBody(step, false)}</div>
				{/if}
				{#if onundo && step.ok && step.tool && UNDOABLE.includes(step.tool) && !undone.has(step.tool + '|' + (step.summary ?? '')) && !busy}
					<button
						type="button"
						class="undo"
						onclick={() => {
							stepsOpen = null;
							onundo(step);
						}}>{t('agent.undo')}</button
					>
				{/if}
			</li>
		{/each}
	</ol>
{/snippet}

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
				>{!find.trim()
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
	{/if}
	<div class="column" bind:this={column}>
		{#each list as block, i (block.kind === 'steps' ? `s${i}` : block.entry.id)}
			{#if block.kind === 'user'}
				{@const entry = block.entry}
				{#if editing?.id === entry.id}
					<form
						class="editing"
						onsubmit={(e) => {
							e.preventDefault();
							resend(entry);
						}}
					>
						<textarea
							rows="3"
							maxlength="20000"
							bind:value={editing.text}
							use:editArea
							aria-label={t('agent.editMessage')}
							onkeydown={(e) => {
								if (e.key === 'Escape') editing = null;
								else if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
									e.preventDefault();
									resend(entry);
								}
							}}></textarea>
						<div class="edit-foot">
							<span>{t('agent.editHint')}</span>
							<button type="button" class="btn" onclick={() => (editing = null)}
								>{t('common.cancel')}</button
							>
							<button type="submit" class="btn btn-primary" disabled={!editing.text.trim()}
								>{t('agent.resend')}</button
							>
						</div>
					</form>
				{:else}
					<div class="me">
						<div class="user" class:fresh={fresh(entry)} use:hold={entry}>
							<span>{entry.content}</span>
						</div>
						{#if !phone}
							{@const done = copied?.id === entry.id ? copied : null}
							<div class="acts">
								{#if canChange}
									<button
										type="button"
										class="act"
										onclick={() => (editing = { id: entry.id, text: entry.content ?? '' })}
										><Icon name="edit" size={14} /><span>{t('agent.edit')}</span></button
									>
								{/if}
								<button
									type="button"
									class="act"
									onclick={() => copy(entry.id, entry.content ?? '')}
									><Icon name={done?.ok ? 'check' : 'copy'} size={14} /><span
										>{done
											? done.ok
												? t('agent.copiedShort')
												: t('agent.copyFailed')
											: t('agent.copy')}</span
									></button
								>
							</div>
						{/if}
					</div>
				{/if}
			{:else if block.kind === 'steps'}
				{@const tools = block.steps.filter((s) => s.role === 'tool').length}
				{#if block.steps.length || block.thinking}
					<button
						type="button"
						class="steps-line"
						aria-haspopup="dialog"
						onclick={() => {
							stepShown = null;
							stepsOpen = block;
						}}
					>
						<span class="count"
							>{block.steps.length
								? t('agent.steps', { count: tools }) + seconds(block.steps)
								: t('agent.thoughts')}</span
						>
						{#if stepsGist(block.steps)}<span class="gist">· {stepsGist(block.steps)}</span>{/if}
						<Icon name="chevronRight" size={14} />
					</button>
					{#if showSteps}{@render stepList(block, false)}{/if}
				{/if}
			{:else if block.kind === 'answer'}
				{@const prev = list[i - 1]}
				{@const learned = learnedBefore.by.get(i) ?? []}
				<div class="answer">
					{#if block.entry.content}
						<!-- eslint-disable-next-line svelte/no-at-html-tags -- chatMarkdown escapes everything first -->
						<div class="md">{@html chatMarkdown(block.entry.content)}</div>
					{/if}
					{#if turnEnds(i)}
						<ResultCards
							steps={turnSteps(i)}
							active={block.entry.id === lastAnswer && !busy && !readonly && !chat.pending}
							onask={onchoice}
						/>
					{/if}
					{#if block.entry.choices?.length && block.entry.id === lastAnswer && !busy && !readonly && !chat.pending}
						<div class="choices">
							{#each block.entry.choices as choice, n (choice)}
								<button
									type="button"
									class="btn"
									class:btn-primary={n === block.entry.choices.length - 1}
									onclick={() => onchoice(choice)}>{choice}</button
								>
							{/each}
						</div>
					{/if}
					{#if block.entry.content || learned.length}
						<div class="acts">
							{#if block.entry.content && canSpeak()}
								{@const reading = speech.id === block.entry.id}
								<button
									type="button"
									class="act"
									class:on={reading}
									aria-pressed={reading}
									onclick={() => speak(block.entry.id, block.entry.content ?? '', i18n.locale)}
									title={reading ? t('agent.readStop') : t('agent.readAloud')}
									><Icon name={reading ? 'stop' : 'volume'} size={17} /><span class="sr-only"
										>{reading ? t('agent.readStop') : t('agent.readAloud')}</span
									></button
								>
							{/if}
							{#if block.entry.content}
								{@const done = copied?.id === block.entry.id ? copied : null}
								<button
									type="button"
									class="act"
									title={t('agent.copy')}
									onclick={() => copy(block.entry.id, block.entry.content ?? '')}
									><Icon name={done?.ok ? 'check' : 'copy'} size={17} /><span class="sr-only"
										>{done
											? done.ok
												? t('agent.copiedShort')
												: t('agent.copyFailed')
											: t('agent.copy')}</span
									></button
								>
								{#if done}<span class="sr-only" role="status"
										>{done.ok ? t('agent.copiedShort') : t('agent.copyFailed')}</span
									>{/if}
							{/if}
							{#if canChange && lastUser && block.entry.id === lastAnswer && block.entry.content}
								<button
									type="button"
									class="act"
									title={t('agent.regenerate')}
									onclick={() => onresend?.(lastUser)}
									><Icon name="refresh" size={17} /><span class="sr-only"
										>{t('agent.regenerate')}</span
									></button
								>
							{/if}
							{#each learned as n (n.id)}
								<span class="learned">✦ {t(noteText[n.note ?? 'memory'])}: {n.content}</span>
							{/each}
						</div>
					{/if}
				</div>
			{:else if block.kind === 'note'}
				{#if learnedBefore.taken.has(block.entry.id)}
					<!-- shown in the row under the next answer -->
				{:else if block.entry.note === 'schedule' && block.entry.content?.includes('„')}
					<div class="plan-card">
						<span class="plan-ic" aria-hidden="true"
							>{block.entry.content.startsWith('Erinnerung') ? '⏰' : '🗓️'}</span
						>
						<span class="plan-text">{block.entry.content}</span>
						{#if onplanned}<button type="button" class="btn" onclick={onplanned}
								>{t('agent.planned')}</button
							>{/if}
					</div>
				{:else if block.entry.note === 'summary'}
					<details class="note edited">
						<summary>{t('agent.noteSummary')}</summary>
						<pre>{block.entry.content}</pre>
					</details>
				{:else if block.entry.note === 'snapshot'}
					<span class="learn snap">{block.entry.content}</span>
				{:else if block.entry.note === 'edited'}
					<details class="note edited">
						<summary>{t('agent.noteEdited')}</summary>
						<pre>{block.entry.content}</pre>
					</details>
				{:else if isLearn(block)}
					<span class="learn"
						>✦ {t(noteText[block.entry.note ?? 'memory'])}: {block.entry.content}</span
					>
				{:else}
					<div class="note" class:bad={block.entry.note === 'error'}>
						{t(noteText[block.entry.note ?? ''] ?? 'agent.noteError', {
							count: block.entry.content ?? '',
						})}
						{#if block.entry.note === 'error' && block.entry.content}<span class="muted">
								· {block.entry.content}</span
							>{/if}
						{#if ['error', 'interrupted', 'limit', 'timelimit', 'loop'].includes(block.entry.note ?? '') && i === list.length - 1 && onretry}
							<button type="button" class="btn" onclick={onretry}>{t('agent.continue')}</button>
						{/if}
					</div>
				{/if}
			{/if}
		{/each}

		{#each chat.entries.filter((e) => e.role === 'tool' && e.files?.length) as step (step.id)}
			{#each step.files ?? [] as file (file.root + file.path)}
				{@const name = file.path.split('/').pop() ?? ''}
				{@const app = fileApp(name)}
				<div class="file">
					<span class="doc" aria-hidden="true">{file.path.split('.').pop()?.toUpperCase()}</span>
					<span class="meta">
						<strong>{name}</strong>
						<span class="muted">{file.root.replace(/^share\//, '')} › {file.path}</span>
					</span>
					<span class="file-acts">
						<!-- 0.95: straight into the app of the file type (T-Word, T-Excel …). -->
						{#if app}
							<button
								type="button"
								class="btn btn-primary"
								onclick={() => openInWebApp(app.key, file.root, file.path)}
								>{t('agent.openInApp', { app: app.name })}</button
							>
						{/if}
						<a
							class="btn"
							href={`/files?root=${encodeURIComponent(file.root)}&path=${encodeURIComponent(file.path.split('/').slice(0, -1).join('/'))}`}
							>{t('agent.openInFiles')}</a
						>
					</span>
				</div>
			{/each}
		{/each}

		{#if chat.plan?.length}
			{@const done = chat.plan.filter((s) => s.done).length}
			<section class="plan" aria-label={t('agent.plan')}>
				<header>
					<strong>{t('agent.plan')}</strong>
					<span class="muted">{t('agent.planProgress', { done, total: chat.plan.length })}</span>
				</header>
				<ol>
					{#each chat.plan as step, n (n)}
						<li class:done={step.done}>
							<span class="box" aria-hidden="true">{step.done ? '✓' : ''}</span>
							<span>{step.text}</span>
							{#if step.done}<span class="sr-only">{t('agent.planDone')}</span>{/if}
						</li>
					{/each}
				</ol>
			</section>
		{/if}

		{#if busy}
			<!-- What happens right now: the dots in the figure of what is done,
			     and its word; over a named step a light runs. -->
			{#if live.content}
				<!-- eslint-disable-next-line svelte/no-at-html-tags -- chatMarkdown escapes everything first -->
				<div class="answer md">{@html chatMarkdown(live.content, true)}</div>
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

		{#if chat.pending && !busy && !readonly}
			{@const p = chat.pending}
			{@const lines = argLines(p.args)}
			<!-- Not working, waiting: a ring instead of the running dots. -->
			<p class="live-line wait">
				<span class="ring" aria-hidden="true"></span>{t('agent.waitsForYou')}
			</p>
			<!-- PLAN 68: one banner card. The level is written out, not only a
			     color; nothing here takes the focus or reacts to Enter. -->
			<div
				class="approval"
				class:danger={p.level !== 'normal'}
				role="group"
				aria-labelledby="pending-{p.id}"
			>
				<div class="ask">
					<span class="ask-ic" aria-hidden="true">{p.level === 'normal' ? '✎' : '▲'}</span>
					<div class="ask-text">
						<strong id="pending-{p.id}">{askTitle(p)}</strong>
						<span
							>{t('agent.ask.level', {
								level: t(levelText[p.level] ?? 'agent.levelChange'),
							})} · {t('agent.ask.at', { time: formatDateTime(new Date(p.at)) })}</span
						>
					</div>
					<div class="ask-acts">
						<button type="button" class="btn" disabled={deciding} onclick={() => ondecide(p, false)}
							>{t('agent.decline')}</button
						>
						<button
							type="button"
							class="btn {p.level === 'destroy' ? 'btn-danger' : 'btn-primary'}"
							disabled={deciding}
							onclick={() => ondecide(p, true)}>{t('agent.approve')}</button
						>
					</div>
				</div>
				<details class="ask-details" open={!!preview(p.args)}>
					<summary>{t('agent.ask.details')}</summary>
					<dl>
						<dt>{t('agent.ask.tool')}</dt>
						<dd>{p.tool}{p.summary && p.tool !== 'code_run' ? ' — ' + p.summary : ''}</dd>
						{#each lines as [k, v] (k)}<dt>{k}</dt>
							<dd>{v}</dd>{/each}
					</dl>
					{#if preview(p.args)}<pre>{preview(p.args)}</pre>{/if}
				</details>
			</div>
		{/if}
	</div>
</div>

{#if sheet}
	{@const shown = sheet}
	<BottomSheet label={t('agent.messageActions')} {calm} onclose={() => (sheet = null)}>
		<p class="sh-quote">„{shown.content}“</p>
		{#if canChange}
			<button
				type="button"
				class="sh-item"
				onclick={() => {
					const entry = sheet!;
					sheet = null;
					editing = { id: entry.id, text: entry.content ?? '' };
				}}
				><Icon name="edit" size={18} /><span class="sh-text">{t('agent.editResend')}</span></button
			>
		{/if}
		<button
			type="button"
			class="sh-item"
			onclick={() => {
				const entry = sheet!;
				sheet = null;
				void copy(entry.id, entry.content ?? '');
			}}><Icon name="copy" size={18} /><span class="sh-text">{t('agent.copy')}</span></button
		>
		{#if canChange && shown.id === lastUser?.id}
			<button
				type="button"
				class="sh-item"
				onclick={() => {
					const entry = sheet!;
					sheet = null;
					onresend?.(entry);
				}}
				><Icon name="refresh" size={18} /><span class="sh-text">{t('agent.regenerateAnswer')}</span
				></button
			>
		{/if}
		<button
			type="button"
			class="sh-item"
			onclick={() => {
				const entry = sheet!;
				sheet = null;
				find = '';
			}}
			><Icon name="search" size={18} /><span class="sh-text">{t('agent.findInChat')}</span></button
		>
		{#if onremember}
			<button
				type="button"
				class="sh-item"
				onclick={() => {
					const entry = sheet!;
					sheet = null;
					onremember(entry.content ?? '');
				}}
				><Icon name="bulb" size={18} /><span class="sh-text">{t('agent.rememberThis')}</span
				></button
			>
		{/if}
	</BottomSheet>
{/if}

{#if stepsOpen}
	{@const shown = stepShown}
	<BottomSheet
		label={t('agent.stepsSummary')}
		{calm}
		onclose={() => {
			stepsOpen = null;
			stepShown = null;
		}}
	>
		<div class="sum-head">
			<button
				type="button"
				class="sum-x"
				aria-label={shown ? t('agent.set.back') : t('common.close')}
				onclick={() => {
					if (shown) stepShown = null;
					else stepsOpen = null;
				}}><Icon name={shown ? 'chevronLeft' : 'close'} size={18} /></button
			>
			<strong
				>{shown === 'thinking'
					? t('agent.thoughts')
					: shown
						? stepLabel(shown)
						: t('agent.stepsSummary')}</strong
			>
		</div>
		{#key shown}
			<div
				class="sum-body"
				in:fly={{ x: shown ? 36 : -36, duration: calm ? 0 : 300, easing: quintOut }}
			>
				{#if shown === 'thinking'}
					<div class="sum-box"><pre>{stepsOpen.thinking}</pre></div>
				{:else if shown}
					{@const lines = argLines(shown.args)}
					{#if lines.length || preview(shown.args)}
						<h3 class="sum-group">{t('agent.stepInput')}</h3>
						<div class="sum-box">
							{#if lines.length}
								<dl>
									{#each lines as [k, v] (k)}<dt>{k}</dt>
										<dd>{v}</dd>{/each}
								</dl>
							{/if}
							{#if preview(shown.args)}<pre>{preview(shown.args)}</pre>{/if}
						</div>
					{/if}
					{#if stepOutput(shown)}
						<h3 class="sum-group">{t('agent.stepOutput')}</h3>
						<div class="sum-box"><pre>{stepOutput(shown)}</pre></div>
					{/if}
				{:else}
					{@render stepList(stepsOpen, true)}
				{/if}
			</div>
		{/key}
	</BottomSheet>
{/if}

<style>
	/* PLAN 68: the chat in the design language of TitanOS – at most 760 px
	   wide in the middle, the person's message a bubble on the right tinted
	   with the accent, the answer plain text, steps one line, tables in a
	   card, the approval one banner with a warning frame. */
	.messages {
		flex: 1;
		overflow-y: auto;
		padding: 20px var(--space-5);
		overscroll-behavior: contain;
	}
	.column {
		max-width: 760px;
		margin: 0 auto;
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		font-size: 0.9375rem;
	}
	.me {
		display: flex;
		flex-direction: column;
		align-items: flex-end;
		gap: 2px;
		margin: var(--space-1) 0;
	}
	.me .acts {
		margin: 0;
	}
	.user {
		display: flex;
		justify-content: flex-end;
		width: 100%;
	}
	/* Changing an own message: a field in its place. */
	.editing {
		padding: var(--space-3);
		border: 1px solid var(--accent);
		border-radius: var(--radius-tile);
		background: var(--surface);
	}
	.editing textarea {
		display: block;
		width: 100%;
		min-height: 64px;
		border: 0;
		outline: 0;
		resize: vertical;
		background: none;
		color: var(--text);
		font: inherit;
		line-height: 1.5;
	}
	.edit-foot {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin-top: var(--space-2);
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.edit-foot span {
		flex: 1;
		min-width: 160px;
	}
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
	/* Under a table: copy it. */
	.md :global(.table-foot) {
		display: flex;
		justify-content: flex-end;
		margin: calc(-1 * var(--space-2)) 0 var(--space-3);
	}
	.md :global(.table-foot button) {
		min-height: 28px;
		padding: 0 var(--space-1);
		border: 0;
		border-radius: var(--radius-small, 6px);
		background: none;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
	}
	.md :global(.table-foot button:hover) {
		color: var(--text);
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}

	.user span {
		max-width: 78%;
		padding: 10px 16px;
		border-radius: 20px;
		background: color-mix(in srgb, var(--text) 9%, var(--surface));
		color: var(--text);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.answer {
		line-height: 1.6;
		overflow-wrap: anywhere;
	}
	.md :global(p) {
		margin: 0 0 0.75em;
	}
	.md :global(a) {
		color: var(--ag-link, var(--accent));
	}
	.md :global(li) {
		margin: 0.4em 0;
	}
	.md :global(li::marker) {
		color: var(--muted);
	}
	.md :global(.table) {
		margin: var(--space-1) 0 var(--space-3);
		border: 1px solid var(--border);
		border-radius: 14px;
		background: none;
		overflow-x: auto;
	}
	.md :global(.table table) {
		width: 100%;
		overflow-wrap: normal;
	}
	/* Words stay whole in a table (owner 15 Nov 2026: "Docker-Ap / ps",
	   "System-Ü / bersicht" in a narrow first column): the readable page
	   lets cells break anywhere, here a word only breaks at a hyphen or
	   when it alone is wider than the table. */
	.md :global(.table :is(th, td)) {
		padding: 9px 14px;
		font-size: var(--text-base);
		overflow-wrap: break-word;
		word-break: normal;
		hyphens: manual;
	}
	.md :global(.table th) {
		font-size: inherit;
		font-weight: 600;
		background: color-mix(in srgb, var(--text) 4%, transparent);
	}
	.md :global(.table tr:last-child td) {
		border-bottom: 0;
	}
	/* A block of code: its language and "Kopieren" in the head; a long
	   line scrolls sideways instead of breaking. */
	.md :global(.code) {
		margin: var(--space-1) 0 var(--space-3);
		border: 1px solid var(--border);
		border-radius: 14px;
		background: var(--surface);
		overflow: hidden;
	}
	.md :global(.code-head) {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		min-height: 38px;
		padding: 0 var(--space-1-5) 0 14px;
		border-bottom: 1px solid var(--border);
		color: var(--muted);
		font-family: var(--font-mono, ui-monospace, monospace);
		font-size: var(--text-sm);
	}
	.md :global(.code-head button) {
		min-height: 30px;
		padding: 0 var(--space-2);
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--muted);
		font: inherit;
		font-family: var(--font-sans, inherit);
		cursor: pointer;
	}
	.md :global(.code-head button:hover) {
		color: var(--text);
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.md :global(.code pre) {
		margin: 0;
		padding: var(--space-3) 14px;
		overflow-x: auto;
		white-space: pre;
		line-height: 1.5;
		font-size: var(--text-base);
	}
	.md :global(.code-tools) {
		display: inline-flex;
		gap: 2px;
	}
	/* Enlarged: over the whole chat, with larger text; lines still scroll. */
	.md :global(.code.full) {
		position: fixed;
		z-index: 70;
		inset: 0;
		margin: 0;
		border: 0;
		border-radius: 0;
		display: flex;
		flex-direction: column;
		padding-top: var(--agent-top, 0px);
		background: var(--bg);
	}
	.md :global(.code.full pre) {
		flex: 1;
		overflow: auto;
		font-size: var(--text-md);
		padding-bottom: calc(env(safe-area-inset-bottom, 0px) + var(--space-4));
	}
	/* The colors of the code (lib/code-highlight.ts), mixed with the text
	   color so they hold on a light and a dark theme. */
	.md :global(.t-c) {
		color: var(--muted);
		font-style: italic;
	}
	.md :global(.t-a) {
		color: color-mix(in srgb, #d9905f 82%, var(--text));
	}
	.md :global(.t-s) {
		color: color-mix(in srgb, #6fae6c 82%, var(--text));
	}
	.md :global(.t-k) {
		color: color-mix(in srgb, #6f9ff0 82%, var(--text));
	}
	.md :global(.t-n),
	.md :global(.t-v) {
		color: color-mix(in srgb, #b98ae6 80%, var(--text));
	}
	.md :global(.code pre code) {
		padding: 0;
		background: none;
		font-size: inherit;
		overflow-wrap: normal;
	}
	.md :global(code) {
		font-family: var(--font-mono, ui-monospace, monospace);
		font-size: 0.9em;
		background: var(--chip);
		padding: 1px 5px;
		border-radius: var(--radius-small);
		overflow-wrap: anywhere;
	}
	.md :global(ul),
	.md :global(ol) {
		margin: 0 0 0.6em;
		padding-left: 1.3em;
	}
	.md :global(h1),
	.md :global(h2),
	.md :global(h3) {
		font-size: var(--text-md);
		margin: 0.6em 0 0.3em;
	}
	/* Steps and thoughts of a turn: one quiet line; a tap opens the sheet
	   "Zusammenfassung" with every step, its input and its output. */
	.steps-line {
		align-self: flex-start;
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		max-width: 100%;
		min-height: 32px;
		padding: 0 var(--space-1);
		margin-left: calc(-1 * var(--space-1));
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--muted);
		font: inherit;
		font-size: 0.9em;
		text-align: left;
		cursor: pointer;
	}
	.steps-line:hover {
		color: var(--text);
		background: color-mix(in srgb, var(--text) 6%, transparent);
	}
	.steps-line:focus-visible,
	.step:focus-visible,
	.sum-x:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.steps-line :global(svg) {
		flex: none;
	}
	.count {
		flex: none;
	}
	/* The short summary of the steps wraps instead of ending in "…"
	   (owner 16 Nov 2026: no cut-off text). */
	.gist {
		min-width: 0;
		overflow-wrap: anywhere;
		text-align: left;
	}
	.step-list {
		list-style: none;
		margin: 0;
		padding: 0 var(--space-3);
	}
	.column > .step-list {
		padding: 0;
		font-size: 0.9em;
	}
	.step-list li {
		position: relative;
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}
	/* The steps hang on one fine line. */
	.step-list li + li::before {
		content: '';
		position: absolute;
		left: 21px;
		top: -7px;
		height: 14px;
		width: 1px;
		background: var(--border);
	}
	.step {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: var(--space-3);
		min-height: 52px;
		padding: var(--space-1-5) var(--space-2-5);
		border: 0;
		border-radius: 12px;
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
	}
	button.step {
		cursor: pointer;
	}
	button.step:hover {
		background: color-mix(in srgb, var(--text) 6%, transparent);
	}
	.column > .step-list .step {
		min-height: 34px;
		padding-left: 0;
	}
	.column > .step-list li + li::before {
		left: 8px;
		top: -4px;
		height: 8px;
	}
	.step :global(svg) {
		flex: none;
		color: var(--muted);
	}
	.mark {
		flex: none;
		display: grid;
		place-items: center;
		width: 22px;
	}
	.what {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		line-height: 1.3;
		overflow-wrap: anywhere;
	}
	.what small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.step .time {
		flex: none;
		color: var(--muted);
		font-size: var(--text-xs);
	}
	.failed .mark :global(svg),
	.failed .what {
		color: var(--warn-text);
	}
	.undo {
		flex: none;
		min-height: 30px;
		border: 1px solid var(--border);
		background: none;
		color: var(--text);
		border-radius: 999px;
		padding: 0 var(--space-2-5);
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
	}
	.sum-head {
		position: relative;
		display: flex;
		align-items: center;
		justify-content: center;
		min-height: 48px;
		padding: 0 64px;
		margin-bottom: var(--space-1-5);
		text-align: center;
	}
	.sum-head strong {
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.sum-x {
		position: absolute;
		left: var(--space-4);
		top: 2px;
		width: 44px;
		height: 44px;
		display: grid;
		place-items: center;
		border: 1px solid var(--border);
		border-radius: 50%;
		background: color-mix(in srgb, var(--text) 6%, var(--surface));
		color: var(--text);
		cursor: pointer;
	}
	.sum-group {
		margin: var(--space-3) 22px var(--space-1-5);
		color: var(--muted);
		font-size: var(--text-md);
		font-weight: 400;
	}
	.sum-box {
		margin: 0 var(--space-4);
		padding: var(--space-3) var(--space-4);
		border: 1px solid var(--border);
		border-radius: 16px;
		background: color-mix(in srgb, var(--text) 4%, var(--surface));
	}
	.sum-box dl {
		margin: 0;
		display: grid;
		gap: 2px;
	}
	.sum-box dt {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.sum-box dd {
		margin: 0 0 var(--space-1-5);
		overflow-wrap: anywhere;
	}
	.sum-box pre {
		margin: 0;
		max-height: 46dvh;
		overflow: auto;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-family: inherit;
		font-size: var(--text-md);
		line-height: 1.5;
	}
	.choices {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
		margin-top: var(--space-1-5);
	}
	/* The small row under an answer: Vorlesen, Kopieren, Gemerkt. */
	.acts {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-1-5);
		margin-top: 2px;
		margin-left: calc(-1 * var(--space-1-5));
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.act {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		justify-content: center;
		min-height: 32px;
		min-width: 32px;
		padding: 0 var(--space-1-5);
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--muted);
		font: inherit;
		cursor: pointer;
	}
	.act:hover,
	.act.on {
		color: var(--text);
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.act:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.learned {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.learn {
		align-self: flex-start;
		font-size: var(--text-sm);
		padding: 3px var(--space-2-5);
		border-radius: 999px;
		background: var(--chip);
		color: var(--text);
	}
	.note {
		font-size: var(--text-base);
		color: var(--muted);
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex-wrap: wrap;
	}
	.note.bad {
		color: var(--warn-text);
	}
	.note summary {
		cursor: pointer;
	}
	.edited pre {
		white-space: pre-wrap;
		font-size: var(--text-sm);
		max-height: 240px;
		overflow: auto;
	}
	.file {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4);
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--surface);
		text-decoration: none;
		color: var(--text);
	}
	.file {
		flex-wrap: wrap;
	}
	.file-acts {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.file-acts .btn {
		text-decoration: none;
	}
	.file .doc {
		width: 36px;
		height: 42px;
		border-radius: var(--radius-small);
		display: grid;
		place-items: center;
		background: var(--chip);
		color: var(--text);
		font-weight: 700;
		font-size: var(--text-xs);
		flex: none;
	}
	.file .meta {
		display: flex;
		flex-direction: column;
		min-width: 0;
		flex: 1 1 12em;
	}
	.file .meta .muted {
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.plan {
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--surface);
		padding: var(--space-2-5) var(--space-4);
	}
	.plan header {
		display: flex;
		justify-content: space-between;
		gap: var(--space-2);
		flex-wrap: wrap;
		font-size: var(--text-base);
	}
	.plan ol {
		list-style: none;
		margin: var(--space-1-5) 0 0;
		padding: 0;
		display: grid;
		gap: var(--space-1);
	}
	.plan li {
		display: flex;
		gap: var(--space-2);
		align-items: flex-start;
		font-size: var(--text-base);
		overflow-wrap: anywhere;
	}
	.plan li.done > span:nth-child(2) {
		color: var(--muted);
		text-decoration: line-through;
	}
	.plan .box {
		flex: none;
		width: 1.1em;
		height: 1.1em;
		margin-top: 0.15em;
		border: 1.5px solid var(--muted);
		border-radius: 4px;
		display: inline-grid;
		place-items: center;
		font-size: 0.75em;
		line-height: 1;
	}
	.plan li.done .box {
		background: var(--accent);
		border-color: var(--accent);
		color: var(--on-accent);
	}
	.plan-card {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-2-5) var(--space-4);
		border-radius: var(--radius-tile);
		border: 1px solid var(--border);
		background: var(--surface);
	}
	.plan-ic {
		font-size: 20px;
		flex: none;
	}
	.plan-text {
		flex: 1;
		min-width: 0;
	}
	/* While the agent works: three dots, and a light that runs over the
	   line from left to right (owner 15 Nov 2026). "Animationen: Aus" and
	   "Weniger Bewegung" leave the line still. */
	.live-line,
	.approval,
	.steps-line,
	.user.fresh {
		animation: come 0.32s cubic-bezier(0.22, 1, 0.36, 1) both;
	}
	@keyframes come {
		from {
			opacity: 0;
			transform: translateY(6px);
		}
	}
	:global(.agent.calm) :is(.live-line, .approval, .steps-line, .user.fresh) {
		animation: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.live-line,
		.approval,
		.steps-line,
		.user.fresh {
			animation: none;
		}
	}
	/* What can be pressed changes colour softly, not at once (0.100: the
	   chat had no transition on any of its buttons). */
	.act,
	.steps-line,
	button.step,
	.choices :global(.btn),
	.md :global(.code-head button),
	.md :global(.table-foot button) {
		transition:
			background-color 0.16s ease,
			color 0.16s ease,
			border-color 0.16s ease;
	}
	:global(.agent.calm) :is(.act, .steps-line, button.step) {
		transition: none;
	}
	.live-line {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		margin: 0;
		min-height: 32px;
		font-size: 0.95em;
		color: var(--muted);
	}
	.ring {
		flex: none;
		width: 14px;
		height: 14px;
		box-sizing: border-box;
		border-radius: 50%;
		border: 2px solid var(--status-warn, var(--ag-warm));
		background: radial-gradient(circle, var(--status-warn, var(--ag-warm)) 38%, transparent 44%);
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
	/* What comes with a finished answer (copy, read aloud) fades in instead
	   of popping up under it. */
	.acts {
		animation: settle 0.35s ease-out both;
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
	.busy-word {
		color: var(--ag-warm, var(--accent));
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
		.acts,
		.live-line.done {
			animation: none;
		}
	}
	/* The step that waits for approval: one banner card. */
	.approval {
		border: 1px solid var(--warn-border);
		border-radius: 20px;
		background: var(--surface);
		overflow: hidden;
	}
	.ask {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-2-5) var(--space-3);
		padding: var(--space-3) var(--space-4);
	}
	.ask-ic {
		flex: none;
		font-size: 16px;
		color: var(--warn-text);
	}
	.ask-text {
		flex: 1 1 16rem;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.ask-text strong {
		font-size: 1.05em;
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.ask-acts .btn {
		border-radius: 999px;
		padding-left: var(--space-4);
		padding-right: var(--space-4);
	}
	.ask-text span {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.ask-acts {
		display: flex;
		gap: var(--space-2);
		margin-left: auto;
	}
	.ask-details {
		border-top: 1px solid var(--border);
		font-size: var(--text-sm);
	}
	.ask-details summary {
		cursor: pointer;
		padding: var(--space-1-5) var(--space-4);
		color: var(--muted);
	}
	.ask-details dl {
		display: grid;
		grid-template-columns: max-content 1fr;
		gap: 2px var(--space-3);
		margin: 0;
		padding: 0 var(--space-4) var(--space-2-5);
	}
	.ask-details dt {
		color: var(--muted);
	}
	.ask-details dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.ask-details pre {
		margin: 0 var(--space-4) var(--space-3);
		max-height: 320px;
		overflow: auto;
		background: var(--bg);
		padding: var(--space-2);
		border-radius: var(--radius-small);
		white-space: pre-wrap;
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
		.find {
			top: max(calc(var(--agent-top, 0px) + 56px), var(--agent-head, 0px));
		}
		.act {
			min-height: 44px;
			min-width: 44px;
		}
		.steps-line {
			min-height: 44px;
		}
		.user span {
			max-width: 86%;
			/* A long press opens the actions, it does not select text. */
			user-select: none;
			-webkit-user-select: none;
			-webkit-touch-callout: none;
		}
		.find-btn {
			min-height: 44px;
			min-width: 44px;
		}
		.find input {
			min-height: 44px;
			font-size: max(16px, 1rem);
		}
		.find {
			gap: 2px;
			padding-left: var(--space-2-5);
		}
		.edit-foot .btn {
			min-height: 44px;
		}
		.gist {
			display: none;
		}
		.md :global(.table :is(th, td)) {
			padding: var(--space-2-5) var(--space-2-5);
		}
		.ask-acts {
			width: 100%;
			justify-content: flex-end;
		}
	}
</style>
