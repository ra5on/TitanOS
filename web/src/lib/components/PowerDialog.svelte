<script lang="ts">
	// Confirmation before a restart or shutdown, and the waiting screen after it.
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Props = { action: 'restart' | 'shutdown'; onclose: () => void };
	let { action, onclose }: Props = $props();

	let dialog: HTMLDialogElement;
	let phase = $state<'confirm' | 'working' | 'done'>('confirm');
	let error = $state('');

	$effect(() => {
		dialog.showModal();
	});

	async function confirm() {
		error = '';
		phase = 'working';
		try {
			await api.post('/system/power', { action: action === 'restart' ? 'reboot' : 'poweroff' });
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
			phase = 'confirm';
			return;
		}
		phase = 'done';
		if (action === 'restart') waitForRestart();
	}

	// The NAS first has to go down, then come back: wait for an answer that
	// follows at least one failed request (or half a minute of silence).
	async function waitForRestart() {
		const started = Date.now();
		let wasDown = false;
		for (;;) {
			await new Promise((resolve) => setTimeout(resolve, 3000));
			try {
				const res = await fetch('/api/v1/health', { cache: 'no-store' });
				if (res.ok && (wasDown || Date.now() - started > 30000)) {
					location.reload();
					return;
				}
			} catch {
				wasDown = true;
			}
		}
	}

	function cancel(event: Event) {
		// Once the NAS is going down there is nothing to return to.
		if (phase !== 'confirm') event.preventDefault();
	}
</script>

<dialog bind:this={dialog} class="tile" aria-labelledby="power-title" onclose={onclose} oncancel={cancel}>
	{#if phase !== 'done'}
		<h2 id="power-title">{t(action === 'restart' ? 'power.restartTitle' : 'power.shutdownTitle')}</h2>
		<ul>
			<li><span class="dot" aria-hidden="true"></span>{t('power.servicesNote')}</li>
			<li class="muted"><span class="dot" aria-hidden="true"></span>{t(action === 'restart' ? 'power.restartNote' : 'power.shutdownNote')}</li>
		</ul>
		{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
		<div class="actions">
			<button type="button" class="btn" onclick={() => dialog.close()} disabled={phase === 'working'}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" onclick={confirm} disabled={phase === 'working'}>
				{t(action === 'restart' ? 'power.restartAction' : 'power.shutdownAction')}
			</button>
		</div>
	{:else}
		<h2 id="power-title">{t(action === 'restart' ? 'power.restarting' : 'power.shuttingDown')}</h2>
		<p class="muted" role="status">{t(action === 'restart' ? 'power.restartingText' : 'power.shutDownText')}</p>
	{/if}
</dialog>

<style>
	dialog {
		width: calc(100% - 48px);
		max-width: 520px;
		padding: 24px;
		background: var(--surface);
		border-color: var(--border);
		color: var(--text);
		box-shadow: var(--shadow-menu);
	}
	dialog[open] {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	dialog::backdrop {
		background: var(--overlay);
	}
	h2 {
		font-size: 1.1875rem;
		font-weight: 600;
	}
	ul {
		margin: 0;
		padding: 0;
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: 10px;
		font-size: 0.875rem;
	}
	li {
		display: flex;
		gap: 10px;
	}
	li .dot {
		margin-top: 7px;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: 8px;
	}
</style>
