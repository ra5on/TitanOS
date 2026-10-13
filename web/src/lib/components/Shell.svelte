<script lang="ts">
	// The frame around every page once someone is logged in.
	import type { Snippet } from 'svelte';
	import { session } from '#lib/session.svelte.ts';
	import { shell } from '#lib/shell.svelte.ts';
	import { watchStorage } from '#lib/storage.svelte.ts';
	import { api } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system, watchSystem } from '#lib/system.svelte.ts';
	import { terminal, toggleTerminal } from '#lib/terminal.svelte.ts';
	import Header from './Header.svelte';
	import StatusBar from './StatusBar.svelte';
	import TerminalPanel from './TerminalPanel.svelte';

	let { children }: { children: Snippet } = $props();

	// A shell is a way into the system: administrators only.
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));

	function hotkey(event: KeyboardEvent) {
		// Ctrl+` wherever the key is; layouts differ in what it types.
		if (admin && event.ctrlKey && !event.altKey && !event.metaKey && (event.key === '`' || event.code === 'Backquote')) {
			event.preventDefault();
			toggleTerminal();
		}
	}

	const pendingSeconds = $derived(system.info?.networkPendingSeconds ?? 0);
	let settling = $state(false);
	async function settle(action: 'confirm' | 'revert') {
		settling = true;
		try {
			await api.post(`/network/${action}`);
		} catch {
			// The next look at the system says what became of it.
		}
		settling = false;
	}

	$effect(() => watchSystem());
	$effect(() => watchStorage());
</script>

<svelte:window onkeydown={hotkey} />

<!-- The page ends above the open terminal, so that all of it can be scrolled into view. -->
{#if admin && pendingSeconds > 0}
	<!-- A network change on trial: whoever still reaches the NAS confirms it, from any page. -->
	<div class="trial" role="alert">
		<span>{t('netcfg.pending', { seconds: pendingSeconds })}</span>
		<button type="button" class="btn btn-primary" onclick={() => settle('confirm')} disabled={settling}>{t('netcfg.keep')}</button>
		<button type="button" class="btn" onclick={() => settle('revert')} disabled={settling}>{t('netcfg.back')}</button>
	</div>
{/if}
<div class="shell" style:padding-bottom={admin && terminal.open && !terminal.full ? `${terminal.height}px` : undefined}>
	<Header />
	<main>{@render children()}</main>
	<StatusBar note={shell.statusNote} />
</div>
{#if admin}<TerminalPanel />{/if}

<style>
	.trial {
		position: sticky;
		top: 0;
		z-index: 30;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: center;
		gap: 10px 16px;
		padding: 10px 16px;
		background: var(--warn-bg);
		border-bottom: 1px solid var(--warn-border);
		color: var(--warn-text);
		font-size: 0.875rem;
	}
	.shell {
		position: relative;
		z-index: 1;
		min-height: 100vh;
		display: flex;
		flex-direction: column;
	}
	main {
		flex: 1 1 auto;
		width: 100%;
		max-width: 1440px;
		margin: 0 auto;
		padding: 28px 32px 36px;
		display: flex;
		flex-direction: column;
		gap: 20px;
	}
	@media (max-width: 760px) {
		main {
			padding: 20px 16px 28px;
		}
	}
</style>
