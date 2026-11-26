<script lang="ts">
	import { i18n, apiErrorText } from '#lib/i18n/index.svelte.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import OfficeTools from '#lib/personal-agent/OfficeTools.svelte';
	import { openPersonalAgent } from '#lib/personal-agent/open.ts';
	import type { Source } from '#lib/personal-agent/api.ts';
	import { baseName, type DocRef } from './protocol.ts';
	let { doc, dirty }: { doc: DocRef; dirty: boolean } = $props();
	const de = $derived(i18n.locale === 'de');
	let open = $state(false),
		working = $state(false),
		valid = $state(false),
		error = $state('');
	let selection = $state<Record<string, unknown>>({});
	const source = $derived<Source>({
		kind: 'file',
		id: doc.path,
		label: baseName(doc.path),
		ref: { root: doc.root },
	});
	function select(args: Record<string, unknown>, ok: boolean) {
		selection = args;
		valid = ok;
	}
	async function handoff() {
		if (!valid || working || dirty) return;
		working = true;
		error = '';
		try {
			const ref: Record<string, string> = { root: doc.root };
			for (const key of ['paragraph', 'slide', 'page', 'sheet', 'range'])
				if (selection[key] !== undefined) ref[key] = String(selection[key]);
			await openPersonalAgent({ sources: [{ ...source, ref }], tool: 'office.inspect' });
			open = false;
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			working = false;
		}
	}
</script>

<div class="agent-strip">
	<button
		class="btn"
		onclick={() => {
			error = '';
			open = true;
		}}>Agent</button
	>
</div>
{#if open}
	<Dialog title={de ? 'Mit Agent bearbeiten' : 'Edit with Agent'} onclose={() => (open = false)}>
		<p>{baseName(doc.path)}</p>
		{#if dirty}<p role="status">
				{de
					? 'Speichere die Änderungen, damit der Agent diesen Stand lesen kann.'
					: 'Save your changes to select this version.'}
			</p>
		{:else}<OfficeTools tool="office.inspect" sources={[source]} onchange={select} />{/if}
		{#if error}<p role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button class="btn" onclick={() => (open = false)}>{de ? 'Schließen' : 'Close'}</button>
			<button class="btn btn-primary" disabled={dirty || !valid || working} onclick={handoff}
				>{de ? 'Auswahl im Agent öffnen' : 'Open selection in Agent'}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.agent-strip {
		flex: none;
		display: flex;
		justify-content: flex-end;
		align-items: center;
		padding: 2px 8px;
		background: var(--surface);
		border-bottom: 1px solid var(--border);
	}
	.agent-strip button {
		min-height: 36px;
	}
	@media (pointer: coarse) {
		.agent-strip button {
			min-height: 44px;
		}
	}
</style>
