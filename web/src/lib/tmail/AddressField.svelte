<script lang="ts">
	// To, Cc or Bcc: the recipients as chips and a field with suggestions from
	// the address book (GET /mail/contacts, learned from sent mail). Arrow
	// keys pick a suggestion, Enter takes it; a typed address is taken at a
	// comma, a semicolon, Enter or when the field is left.
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import { mailApi } from './api.ts';
	import { addressText, addrOf, splitRecipients, unique, validAddress } from './text.ts';
	import type { Contact } from './types.ts';

	let {
		id,
		label,
		values = $bindable([]),
		onchange,
	}: {
		id: string;
		label: string;
		values: string[];
		onchange?: () => void;
	} = $props();

	let text = $state('');
	let suggestions = $state<Contact[]>([]);
	let active = $state(-1);
	let open = $state(false);
	let asked = 0;
	let timer: ReturnType<typeof setTimeout> | undefined;
	let input = $state<HTMLInputElement>();

	function add(entries: string[]) {
		const next = unique([...values, ...entries.map((e) => e.trim()).filter(Boolean)]);
		if (next.length !== values.length) {
			values = next;
			onchange?.();
		}
	}
	function remove(entry: string) {
		values = values.filter((v) => v !== entry);
		onchange?.();
		input?.focus();
	}
	function take() {
		const parts = splitRecipients(text);
		text = '';
		close();
		if (parts.length) add(parts);
	}
	function pick(c: Contact) {
		text = '';
		close();
		add([addressText({ name: c.name, addr: c.addr })]);
		input?.focus();
	}
	function close() {
		open = false;
		active = -1;
		suggestions = [];
	}

	function typed() {
		if (/[,;]/.test(text)) {
			take();
			return;
		}
		clearTimeout(timer);
		const q = text.trim();
		if (q.length < 1) {
			close();
			return;
		}
		timer = setTimeout(async () => {
			const n = ++asked;
			try {
				const found = await mailApi.contacts(q);
				if (n !== asked) return;
				const have = new Set(values.map((v) => addrOf(v).toLowerCase()));
				suggestions = (found ?? []).filter((c) => !have.has(c.addr.toLowerCase())).slice(0, 8);
				open = suggestions.length > 0;
				active = -1;
			} catch {
				// No suggestions; typing goes on as before.
				if (n === asked) close();
			}
		}, 200);
	}

	function keydown(e: KeyboardEvent) {
		if (e.key === 'ArrowDown' && open) {
			e.preventDefault();
			active = (active + 1) % suggestions.length;
		} else if (e.key === 'ArrowUp' && open) {
			e.preventDefault();
			active = active <= 0 ? suggestions.length - 1 : active - 1;
		} else if (e.key === 'Enter') {
			if (open && active >= 0) {
				e.preventDefault();
				pick(suggestions[active]);
			} else if (text.trim()) {
				e.preventDefault();
				take();
			}
		} else if (e.key === 'Escape' && open) {
			e.preventDefault();
			e.stopPropagation();
			close();
		} else if (e.key === 'Backspace' && !text && values.length) {
			remove(values[values.length - 1]);
		}
	}
	function blur() {
		// After a click on a suggestion (it keeps the focus in the list first).
		setTimeout(() => {
			if (document.activeElement?.closest(`#${id}-list`)) return;
			if (text.trim()) take();
			else close();
		}, 150);
	}
</script>

<div class="addr-field">
	<label for={id} class="lbl">{label}</label>
	<div class="box">
		{#each values as v (v)}
			{@const ok = validAddress(addrOf(v))}
			<span class="chip addr" class:bad={!ok}>
				{#if !ok}<Icon name="info" size={14} /><span class="sr-only"
						>{t('mail.compose.invalid')}</span
					>{/if}
				<span class="who">{v}</span>
				<button
					type="button"
					class="x"
					aria-label={t('mail.compose.removeRecipient', { address: v })}
					onclick={() => remove(v)}><Icon name="close" size={14} /></button
				>
			</span>
		{/each}
		<input
			{id}
			bind:this={input}
			bind:value={text}
			class="entry"
			type="text"
			inputmode="email"
			autocomplete="off"
			spellcheck="false"
			role="combobox"
			aria-autocomplete="list"
			aria-expanded={open}
			aria-controls="{id}-list"
			aria-activedescendant={open && active >= 0 ? `${id}-opt-${active}` : undefined}
			oninput={typed}
			onkeydown={keydown}
			onblur={blur}
		/>
	</div>
	{#if open}
		<ul class="suggest" id="{id}-list" role="listbox" aria-label={t('mail.compose.suggestions')}>
			{#each suggestions as c, i (c.addr)}
				<li
					id="{id}-opt-{i}"
					role="option"
					aria-selected={i === active}
					class:active={i === active}
					tabindex="-1"
					onmousedown={(e) => e.preventDefault()}
					onclick={() => pick(c)}
					onkeydown={(e) => e.key === 'Enter' && pick(c)}
				>
					{#if c.name}<span class="name">{c.name}</span>{/if}
					<span class="mail">{c.addr}</span>
				</li>
			{/each}
		</ul>
	{/if}
	{#if values.some((v) => !validAddress(addrOf(v)))}
		<p class="hint-bad" role="alert">
			<Icon name="info" size={14} />{t('mail.compose.invalidHint')}
		</p>
	{/if}
</div>

<style>
	.addr-field {
		position: relative;
		display: grid;
		grid-template-columns: 4.5rem minmax(0, 1fr);
		align-items: start;
		gap: var(--space-1) var(--space-2);
	}
	.lbl {
		padding-top: var(--space-2);
		color: var(--muted);
		font-size: var(--text-md);
	}
	.box {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1);
		min-height: var(--control-height);
		padding: 3px var(--space-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--bg);
	}
	.box:focus-within {
		border-color: var(--accent);
	}
	.entry {
		flex: 1 1 8rem;
		min-width: 6rem;
		min-height: 28px;
		border: 0;
		outline: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
	}
	.addr {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		max-width: 100%;
		min-width: 0;
	}
	.addr .who {
		overflow-wrap: anywhere;
		min-width: 0;
	}
	.addr.bad {
		border-color: var(--error-border);
		color: var(--error-text);
	}
	.x {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		flex: none;
		width: 24px;
		height: 24px;
		padding: 0;
		border: 0;
		border-radius: 50%;
		background: none;
		color: inherit;
		cursor: pointer;
	}
	.x:hover {
		background: var(--track);
	}
	.suggest {
		grid-column: 2;
		list-style: none;
		margin: 0;
		padding: var(--space-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--surface);
		box-shadow: var(--shadow-menu);
		z-index: 5;
	}
	.suggest li {
		display: flex;
		flex-direction: column;
		padding: var(--space-1-5) var(--space-2);
		border-radius: var(--radius-small);
		cursor: pointer;
		min-height: 36px;
		justify-content: center;
	}
	.suggest li.active,
	.suggest li:hover {
		background: color-mix(in srgb, var(--accent) 16%, transparent);
	}
	.name,
	.mail {
		overflow-wrap: anywhere;
	}
	.mail {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.hint-bad {
		grid-column: 2;
		display: flex;
		gap: var(--space-1);
		margin: 0;
		color: var(--error-text);
		font-size: var(--text-sm);
	}
	@media (max-width: 899px) {
		.addr-field {
			grid-template-columns: minmax(0, 1fr);
		}
		.lbl {
			padding-top: 0;
		}
		.suggest,
		.hint-bad {
			grid-column: 1;
		}
		.x {
			width: 44px;
			height: 44px;
		}
		.entry {
			min-height: 44px;
		}
		.suggest li {
			min-height: 48px;
		}
	}
</style>
