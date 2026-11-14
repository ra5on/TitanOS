<script lang="ts">
	// One chat of the KI-Agent: the person's messages, the steps and thoughts
	// of each turn folded into one line ("✓ 5 Schritte · 6,1 s … Anzeigen"),
	// the answers (safe Markdown, tables in a card) with Vorlesen, Kopieren
	// and what the agent remembered, questions with buttons, the banner of a
	// step that waits for approval and, while it runs, what streams in.
	import Icon from '#lib/components/Icon.svelte';
	import BottomSheet from './BottomSheet.svelte';
	import { chatMarkdown } from '#lib/chat-markdown.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { canSpeak, speak, speech } from '#lib/agent-speech.svelte.ts';
	import { i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		approvalTitle,
		blocks,
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

	// ----- PLAN 90-4: tables – a button that copies one, cards on the phone -----
	// A table of three or more columns becomes cards to swipe on the phone:
	// one per row, named by its first cell. The cells are the ones
	// chatMarkdown made (escaped there).
	function tableText(table: HTMLTableElement): string {
		return [...table.rows]
			.map((r) => [...r.cells].map((c) => (c.textContent ?? '').trim()).join('\t'))
			.join('\n');
	}
	$effect(() => {
		void list;
		if (!column) return;
		for (const wrap of column.querySelectorAll<HTMLElement>(
			'.answer > .md .table:not([data-done])',
		)) {
			const table = wrap.querySelector('table');
			if (!table) continue;
			wrap.dataset.done = 'true';
			const heads = [...(table.tHead?.rows[0]?.cells ?? [])].map((c) => c.innerHTML);
			const rows = [...(table.tBodies[0]?.rows ?? [])];
			if (phone && heads.length >= 3 && rows.length) {
				const cards = document.createElement('div');
				cards.className = 'cards';
				cards.tabIndex = 0;
				cards.setAttribute('role', 'group');
				cards.setAttribute('aria-label', t('agent.tableCards'));
				for (const row of rows) {
					const card = document.createElement('div');
					card.className = 'card';
					const title = document.createElement('strong');
					title.innerHTML = row.cells[0]?.innerHTML ?? '';
					const dl = document.createElement('dl');
					[...row.cells].slice(1).forEach((cell, n) => {
						const dt = document.createElement('dt');
						dt.innerHTML = heads[n + 1] ?? '';
						const dd = document.createElement('dd');
						dd.innerHTML = cell.innerHTML;
						dl.append(dt, dd);
					});
					card.append(title, dl);
					cards.append(card);
				}
				const where = document.createElement('p');
				where.className = 'cards-where';
				const say = () => {
					const n = Math.round(cards.scrollLeft / Math.max(1, cards.scrollWidth / rows.length));
					where.textContent = t('agent.tableCardsWhere', {
						n: Math.min(rows.length, n + 1),
						count: rows.length,
					});
				};
				cards.addEventListener('scroll', say, { passive: true });
				say();
				wrap.classList.add('as-cards');
				wrap.after(cards, where);
			} else {
				const foot = document.createElement('div');
				foot.className = 'table-foot';
				const button = document.createElement('button');
				button.type = 'button';
				button.textContent = t('agent.tableCopy');
				button.addEventListener('click', () => {
					void copy('table', tableText(table)).then(() => {
						button.textContent = copied?.ok ? t('agent.copiedShort') : t('agent.copyFailed');
						setTimeout(() => (button.textContent = t('agent.tableCopy')), 1800);
					});
				});
				foot.append(button);
				wrap.after(foot);
			}
		}
	});

	let scroller = $state<HTMLDivElement>();
	let pinned = $state(true);
	const list = $derived(blocks(chat.entries));
	// Only the last answer offers its buttons; older questions are answered.
	const lastAnswer = $derived.by(() => {
		for (let i = list.length - 1; i >= 0; i--) {
			const b = list[i];
			if (b.kind === 'user') return '';
			if (b.kind === 'answer') return b.entry.id;
		}
		return '';
	});

	function scrolled() {
		if (!scroller) return;
		pinned = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 80;
	}
	// Stay at the bottom while text streams in, unless the person scrolled up.
	$effect(() => {
		void chat.entries.length;
		void live.content;
		void live.thinking;
		void chat.pending;
		if (pinned && scroller)
			queueMicrotask(() => scroller!.scrollTo({ top: scroller!.scrollHeight }));
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
		if (!ms) return '';
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
						<div class="user" use:hold={entry}><span>{entry.content}</span></div>
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
					<details class="steps" open={showSteps || (i === list.length - 1 && busy)}>
						<summary>
							{#if block.steps.length}
								<span class="ok" aria-hidden="true">✓</span>
								<span class="count">{t('agent.steps', { count: tools })}{seconds(block.steps)}</span
								>
								{#if stepsGist(block.steps)}<span class="gist">· {stepsGist(block.steps)}</span
									>{/if}
							{:else}
								<Icon name="bulb" size={14} />
								<span class="count">{t('agent.thoughts')}</span>
							{/if}
							<span class="toggle"
								><span class="show">{t('agent.stepsShow')}</span><span class="hide"
									>{t('agent.stepsHide')}</span
								><Icon name="chevronDown" size={14} /></span
							>
						</summary>
						{#if block.thinking}
							<div class="thinking">
								<strong>{t('agent.thoughts')}</strong>
								<p>{block.thinking}</p>
							</div>
						{/if}
						{#each block.steps as step (step.id)}
							<div class="step" class:failed={stepFailed(step)}>
								<span class="mark" aria-hidden="true">{stepFailed(step) ? '!' : '✓'}</span>
								<span class="what">{stepLabel(step)}</span>
								{#if stepDetail(step.args)}<code>{stepDetail(step.args)}</code>{/if}
								{#if stepFailed(step)}<span class="sr-only">{t('agent.stepFailed')}</span>{/if}
								{#if step.durationMs}<span class="time"
										>{(step.durationMs / 1000).toFixed(1).replace('.', ',')} s</span
									>{/if}
								{#if onundo && step.ok && step.tool && UNDOABLE.includes(step.tool) && !undone.has(step.tool + '|' + (step.summary ?? '')) && !busy}
									<button type="button" class="undo" onclick={() => onundo(step)}
										>{t('agent.undo')}</button
									>
								{/if}
							</div>
						{/each}
					</details>
				{/if}
			{:else if block.kind === 'answer'}
				{@const prev = list[i - 1]}
				{@const learned = learnedBefore.by.get(i) ?? []}
				<div class="answer">
					{#if block.entry.content}
						<!-- eslint-disable-next-line svelte/no-at-html-tags -- chatMarkdown escapes everything first -->
						<div class="md">{@html chatMarkdown(block.entry.content)}</div>
					{/if}
					{#if block.entry.choices?.length && block.entry.id === lastAnswer && !busy && !readonly}
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
									><Icon name={reading ? 'stop' : 'volume'} size={14} /><span
										>{reading ? t('agent.readStop') : t('agent.readAloud')}</span
									></button
								>
							{/if}
							{#if block.entry.content}
								{@const done = copied?.id === block.entry.id ? copied : null}
								<button
									type="button"
									class="act"
									onclick={() => copy(block.entry.id, block.entry.content ?? '')}
									><Icon name={done?.ok ? 'check' : 'copy'} size={14} /><span
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
								<button type="button" class="act" onclick={() => onresend?.(lastUser)}
									><Icon name="refresh" size={14} /><span>{t('agent.regenerate')}</span></button
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
				<a
					class="file"
					href={`/files?root=${encodeURIComponent(file.root)}&path=${encodeURIComponent(file.path.split('/').slice(0, -1).join('/'))}`}
				>
					<span class="doc" aria-hidden="true">{file.path.split('.').pop()?.toUpperCase()}</span>
					<span class="meta">
						<strong>{file.path.split('/').pop()}</strong>
						<span class="muted">{file.root.replace(/^share\//, '')} › {file.path}</span>
					</span>
					<span class="btn">{t('agent.openInFiles')}</span>
				</a>
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
			{#if live.thinking && !live.content}
				<p class="shimmer">{t('agent.thinking')}</p>
			{/if}
			{#if live.step}
				<div class="live-step">
					<span class="spin" aria-hidden="true"></span>
					{stepLabel(live.step)}
					{#if stepDetail(live.step.args)}<code>{stepDetail(live.step.args)}</code>{/if}
				</div>
			{/if}
			{#if live.content}
				<!-- eslint-disable-next-line svelte/no-at-html-tags -- chatMarkdown escapes everything first -->
				<div class="answer md">{@html chatMarkdown(live.content)}</div>
			{:else if !live.thinking && !live.step}
				<p class="shimmer">{t('agent.working')}</p>
			{/if}
		{/if}

		{#if chat.pending && !busy && !readonly}
			{@const p = chat.pending}
			{@const lines = argLines(p.args)}
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
	/* Under a table: copy it. On the phone a wide table is cards to swipe. */
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
	.md :global(.table.as-cards) {
		display: none;
	}
	.md :global(.cards) {
		display: flex;
		gap: var(--space-2-5);
		margin: var(--space-1) -14px 0 0;
		padding: 0 14px var(--space-1) 0;
		overflow-x: auto;
		scroll-snap-type: x mandatory;
		scrollbar-width: none;
	}
	.md :global(.card) {
		flex: 0 0 82%;
		scroll-snap-align: start;
		padding: var(--space-3) 14px;
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--surface);
		overflow-wrap: anywhere;
	}
	.md :global(.card dl) {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: var(--space-1-5) 14px;
		margin: var(--space-2) 0 0;
		font-size: 0.875rem;
	}
	.md :global(.card dt) {
		color: var(--muted);
	}
	.md :global(.card dd) {
		margin: 0;
	}
	.md :global(.cards-where) {
		margin: var(--space-1) 0 var(--space-3);
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.user span {
		max-width: 78%;
		padding: 9px 14px;
		border-radius: 14px 14px 4px 14px;
		background: color-mix(in srgb, var(--accent) 24%, var(--surface));
		color: var(--text);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.answer {
		line-height: 1.55;
		overflow-wrap: anywhere;
	}
	.md :global(p) {
		margin: 0 0 0.6em;
	}
	.md :global(.table) {
		margin: var(--space-1) 0 var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--surface);
		overflow-x: auto;
	}
	.md :global(.table table) {
		width: 100%;
		overflow-wrap: normal;
	}
	.md :global(.table :is(th, td)) {
		padding: 9px 14px;
		font-size: var(--text-base);
	}
	.md :global(.table th) {
		font-size: var(--text-sm);
		font-weight: 600;
	}
	.md :global(.table tr:last-child td) {
		border-bottom: 0;
	}
	.md :global(.table td:first-child) {
		color: var(--muted);
	}
	.md :global(code),
	code {
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
	/* Steps and thoughts of a turn: one line, opened on demand. */
	.steps {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--surface);
		font-size: var(--text-sm);
		overflow: hidden;
	}
	.steps > summary {
		list-style: none;
		cursor: pointer;
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		min-height: 34px;
		padding: var(--space-1-5) var(--space-3);
		color: var(--muted);
		min-width: 0;
	}
	.steps > summary::-webkit-details-marker {
		display: none;
	}
	.steps > summary:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.count {
		flex: none;
		color: var(--text);
	}
	.gist {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.toggle {
		margin-left: auto;
		flex: none;
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding-left: var(--space-2);
	}
	.steps .hide,
	.steps[open] .show {
		display: none;
	}
	.steps[open] .hide {
		display: inline;
	}
	.steps[open] .toggle :global(svg) {
		transform: rotate(180deg);
	}
	.thinking {
		padding: var(--space-2) var(--space-3);
		border-top: 1px solid var(--border);
	}
	.thinking strong {
		font-weight: 600;
		font-size: var(--text-xs);
		color: var(--muted);
	}
	.thinking p {
		margin: 2px 0 0;
		color: var(--muted);
		white-space: pre-wrap;
		max-height: 260px;
		overflow-y: auto;
	}
	.step {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-1-5) var(--space-3);
		border-top: 1px solid var(--border);
		flex-wrap: wrap;
	}
	.undo {
		border: 1px solid var(--border);
		background: none;
		color: var(--text);
		border-radius: 999px;
		padding: 1px var(--space-2);
		font: inherit;
		font-size: var(--text-xs);
		cursor: pointer;
	}
	.step .time {
		margin-left: auto;
		color: var(--muted);
		font-size: var(--text-xs);
	}
	.ok,
	.mark {
		color: var(--ok-text);
	}
	.failed .mark {
		color: var(--warn-text);
		font-weight: 700;
	}
	.failed .what {
		color: var(--warn-text);
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
		gap: var(--space-1) 14px;
		margin-top: 2px;
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.act {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		min-height: 28px;
		padding: 0 var(--space-1);
		border: 0;
		border-radius: var(--radius-small, 6px);
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
		flex: 1;
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
	.live-step {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--text-base);
		color: var(--muted);
	}
	.spin {
		width: 13px;
		height: 13px;
		border-radius: 50%;
		border: 2px solid var(--border);
		border-top-color: var(--accent);
		animation: spin 0.9s linear infinite;
		flex: none;
	}
	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
	.shimmer {
		margin: 0;
		color: var(--muted);
		animation: pulse 1.6s ease-in-out infinite;
	}
	@keyframes pulse {
		50% {
			opacity: 0.55;
		}
	}
	:global(.agent.calm) .shimmer,
	:global(.agent.calm) .spin {
		animation: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.shimmer,
		.spin {
			animation: none;
		}
	}
	/* The step that waits for approval: one banner card. */
	.approval {
		border: 1px solid var(--warn-border);
		border-radius: var(--radius-tile);
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
		font-weight: 600;
		overflow-wrap: anywhere;
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
		.messages {
			padding: var(--space-3) 14px;
		}
		.user span {
			max-width: 86%;
			/* A long press opens the actions, it does not select text. */
			user-select: none;
			-webkit-user-select: none;
			-webkit-touch-callout: none;
		}
		.act,
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
			padding: var(--space-2) var(--space-2);
		}
		.ask-acts {
			width: 100%;
			justify-content: flex-end;
		}
	}
</style>
