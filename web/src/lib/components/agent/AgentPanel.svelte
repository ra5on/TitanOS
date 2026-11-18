<script lang="ts" module>
	/** A context action of a program: a chip that sends its prompt. */
	export type PanelAction = {
		id: string;
		label: string;
		prompt: string;
		/** Only with something open (a mail, a contact, an event). */
		needsContext?: boolean;
		/** Puts the prompt into the input to be completed (a text to paste,
		 *  the event in plain words) instead of sending it. */
		fill?: boolean;
	};
</script>

<script lang="ts">
	// 1.13 (owner 18 Nov 2026): the agent's panel in Mail, Kontakte and
	// Kalender – the same tools and the same one chat through the three
	// programs ("Neues Gespräch" starts another), context actions as chips,
	// what is open in the program as a reference with each message. Right
	// of the content as a column (ColumnFrame's aside) or, compact, in a
	// sheet from below (the caller decides). Shown only with the agent
	// (session.services.agent); the note that a provider in the internet
	// gets what the agent reads comes before the first use (Q30).
	import { onMount, onDestroy } from 'svelte';
	import Icon from '#lib/components/Icon.svelte';
	import ChatView from './ChatView.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { AgentContext } from '#lib/agent.ts';
	import { AgentPanelChat } from '#lib/agent/panel.svelte.ts';
	import { openInProgram } from '#lib/agent/open.ts';

	let {
		app,
		context = null,
		actions = [],
		phone = false,
		onclose,
	}: {
		app: 'mail' | 'contacts' | 'calendar';
		context?: AgentContext | null;
		actions?: PanelAction[];
		phone?: boolean;
		onclose: () => void;
	} = $props();

	const panel = new AgentPanelChat();
	let text = $state('');
	onMount(() => void panel.load());
	onDestroy(() => panel.detach());

	const needsNote = $derived(!!panel.state?.cloud && !panel.state.cloudNoted);
	const ready = $derived(!!panel.state && !needsNote);

	async function send(message: string) {
		if (!ready) return;
		if (await panel.send(message, context)) text = '';
	}
	function keydown(e: KeyboardEvent) {
		if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
			e.preventDefault();
			void send(text);
		}
	}
	const where = $derived(t(`agentpim.panel.where.${app}`));
</script>

<section class="agent-panel" class:phone aria-label={t('agentpim.panel.title')}>
	<header>
		<span class="ic" aria-hidden="true"><Icon name="sparkle" size={18} /></span>
		<h2>{t('agentpim.panel.title')}</h2>
		<button
			type="button"
			class="btn btn-sm"
			disabled={!panel.chat || panel.busy}
			onclick={() => void panel.fresh()}
			><Icon name="plus" size={15} /><span>{t('agentpim.panel.new')}</span></button
		>
		{#if panel.chat}
			<button
				type="button"
				class="btn btn-sm"
				onclick={() => openInProgram('/agent?chat=' + encodeURIComponent(panel.chat!.id))}
				>{t('agentpim.panel.inApp')}</button
			>
		{/if}
		<button type="button" class="btn btn-sm icon" aria-label={t('common.close')} onclick={onclose}
			><Icon name="close" size={16} /></button
		>
	</header>

	{#if needsNote}
		<div class="notice-warn cloud" role="note">
			<p><strong>{t('agentpim.cloud.title')}</strong></p>
			<p>{t('agentpim.cloud.text')}</p>
			<button type="button" class="btn btn-primary" onclick={() => void panel.noteCloud()}
				>{t('agentpim.cloud.ok')}</button
			>
		</div>
	{/if}

	{#if actions.length}
		<div class="chips" role="group" aria-label={t('agentpim.panel.actions')}>
			{#each actions as a (a.id)}
				<button
					type="button"
					class="chip-btn"
					disabled={!ready || panel.sending || panel.busy || (a.needsContext && !context)}
					onclick={() => {
						if (a.fill) {
							text = a.prompt + ' ';
							document.getElementById('agent-panel-input')?.focus();
						} else void send(a.prompt);
					}}>{a.label}</button
				>
			{/each}
		</div>
	{/if}

	<div class="talk">
		{#if panel.chat}
			<ChatView
				chat={panel.chat}
				live={panel.live}
				busy={panel.busy}
				deciding={panel.deciding}
				{phone}
				calm
				ondecide={panel.decide}
				onedit={panel.edit}
				onchoice={(c) => void send(c)}
			/>
		{:else if panel.state}
			<p class="muted empty">{t('agentpim.panel.empty', { where })}</p>
		{:else if !panel.error}
			<p class="muted empty" role="status">{t('common.loading')}</p>
		{/if}
	</div>

	{#if panel.error}<p class="notice-error err" role="alert">{panel.error}</p>{/if}

	<form
		class="ask"
		onsubmit={(e) => {
			e.preventDefault();
			void send(text);
		}}
	>
		<label class="sr-only" for="agent-panel-input">{t('agentpim.panel.input')}</label>
		<textarea
			id="agent-panel-input"
			bind:value={text}
			rows="2"
			placeholder={t('agentpim.panel.placeholder')}
			disabled={!ready}
			onkeydown={keydown}></textarea>
		{#if panel.busy}
			<button type="button" class="btn" onclick={() => void panel.stop()}
				>{t('agentpim.panel.stop')}</button
			>
		{:else}
			<button
				type="submit"
				class="btn btn-primary"
				disabled={!ready || !text.trim() || panel.sending}>{t('agentpim.panel.send')}</button
			>
		{/if}
	</form>
</section>

<style>
	.agent-panel {
		display: flex;
		flex-direction: column;
		min-height: 0;
		height: 100%;
		background: var(--surface);
		color: var(--text);
		/* The agent app's colors for what the agent does and its links. */
		--ag-warm: #d9734a;
		--ag-link: color-mix(in srgb, #5b8fe0 78%, var(--text));
		--ag-chat-width: none;
	}
	/* Narrow: no room for the agent app's floating head above the chat. */
	.agent-panel .talk :global(.messages) {
		padding: var(--space-3);
	}
	.agent-panel.phone {
		height: 78dvh;
	}
	header {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1-5);
		padding: var(--space-2) var(--space-3);
		border-bottom: 1px solid var(--border);
	}
	header h2 {
		flex: 1 1 auto;
		margin: 0;
		font-size: 1rem;
		overflow-wrap: anywhere;
	}
	.ic {
		display: inline-flex;
		color: var(--accent);
	}
	header .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		min-height: 36px;
	}
	header .icon {
		width: 36px;
		justify-content: center;
		padding: 0;
	}
	.cloud {
		margin: var(--space-2) var(--space-3) 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		align-items: flex-start;
	}
	.cloud p {
		margin: 0;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
		padding: var(--space-2) var(--space-3);
		border-bottom: 1px solid var(--border);
	}
	.chip-btn {
		min-height: 36px;
		padding: var(--space-1) var(--space-3);
		border: 1px solid var(--border);
		border-radius: 999px;
		background: color-mix(in srgb, var(--accent) 8%, var(--surface));
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		text-align: left;
		overflow-wrap: anywhere;
		cursor: pointer;
	}
	.chip-btn:hover:not(:disabled) {
		border-color: var(--accent);
	}
	.chip-btn:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.chip-btn:disabled {
		opacity: 0.55;
		cursor: default;
	}
	.talk {
		flex: 1 1 auto;
		min-height: 0;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}
	.talk :global(.messages) {
		flex: 1 1 auto;
		min-height: 0;
		overflow-y: auto;
	}
	.empty {
		margin: var(--space-4) var(--space-3);
	}
	.muted {
		color: var(--muted);
	}
	.err {
		margin: 0 var(--space-3) var(--space-2);
	}
	.ask {
		display: flex;
		align-items: flex-end;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3) var(--space-3);
		border-top: 1px solid var(--border);
	}
	.ask textarea {
		flex: 1 1 auto;
		min-width: 0;
		min-height: 44px;
		max-height: 10rem;
		resize: vertical;
		font: inherit;
	}
	.ask .btn {
		min-height: 44px;
	}
</style>
