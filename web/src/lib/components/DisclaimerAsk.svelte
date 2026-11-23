<script lang="ts">
	// Once for the owner of a NAS set up before the disclaimer existed: confirm
	// the disclaimer (owner decision of 23 Nov 2026). Asked after the login;
	// other accounts are never asked. The NAS keeps the answer on the stick.
	import { api, ApiError } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import Dialog from './Dialog.svelte';
	import DisclaimerText from './DisclaimerText.svelte';

	type State = { version: number; required: boolean };
	let info = $state<State | null>(null);
	let read = $state(false);
	let busy = $state(false);
	let error = $state('');

	$effect(() => {
		api
			.get<State>('/system/disclaimer')
			.then((s) => (info = s))
			// An older NAS without the route, or no answer: nothing to ask.
			.catch(() => (info = null));
	});

	async function confirm() {
		if (!info || !read) return;
		busy = true;
		error = '';
		try {
			info = await api.post<State>('/system/disclaimer', { version: info.version });
		} catch (err) {
			if (err instanceof ApiError && err.code === 'disclaimer_changed') {
				info = await api.get<State>('/system/disclaimer').catch(() => info);
			}
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

{#if info?.required}
	<!-- Locked: it goes on only by confirming; nothing else is changed meanwhile. -->
	<Dialog title={t('disclaimer.askTitle')} locked onclose={() => {}}>
		<p class="intro">{t('disclaimer.askIntro')}</p>
		<div class="text"><DisclaimerText /></div>
		<label class="check"
			><input type="checkbox" bind:checked={read} />{t('disclaimer.accept')}</label
		>
		{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn btn-primary" onclick={confirm} disabled={!read || busy}
				>{t('disclaimer.continue')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.intro {
		margin: 0;
	}
	.text {
		max-height: 50vh;
		overflow-y: auto;
		padding: var(--space-2-5) var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
</style>
