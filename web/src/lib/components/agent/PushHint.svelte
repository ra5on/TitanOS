<script lang="ts">
	// 1.07: on the iPhone the KI-Agent on the home screen is an app of its
	// own with its own subscription for notifications; until it has one, a
	// tap on a message of the agent opens TitanOS. When the agent runs as
	// its installed app and this device has no subscription yet, it asks
	// once. The browser's question comes only after a tap (iOS wants that).
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { currentSubscription, deviceName, pushSupported, subscribe } from '#lib/web-push.ts';

	/** The agent runs as its installed app on a phone (?app=1 or standalone). */
	let { active = false }: { active?: boolean } = $props();

	const KEY = 'titanos.agent.pushAsked';
	let show = $state(false);
	let busy = $state(false);
	let error = $state('');
	let done = $state('');

	function asked(): boolean {
		try {
			return localStorage.getItem(KEY) === '1';
		} catch {
			return true;
		}
	}
	function remember() {
		try {
			localStorage.setItem(KEY, '1');
		} catch {
			// It may ask again next time.
		}
	}

	onMount(async () => {
		if (!active || !pushSupported() || asked()) return;
		if (Notification.permission === 'denied') return;
		if (await currentSubscription().catch(() => null)) return;
		show = true;
	});

	async function allow() {
		busy = true;
		error = '';
		try {
			const { publicKey } = await api.get<{ publicKey: string }>('/notifications/push');
			await subscribe(
				publicKey,
				deviceName(navigator.userAgent, navigator.maxTouchPoints),
				'agent',
			);
			remember();
			done = t('agent.pushHint.done');
			setTimeout(() => (show = false), 2500);
		} catch (err) {
			error =
				err instanceof Error && err.message === 'denied' ? t('push.denied') : apiErrorText(err);
			if (Notification.permission === 'denied') remember();
		} finally {
			busy = false;
		}
	}
	function later() {
		remember();
		show = false;
	}
</script>

{#if show}
	<div class="push-hint" role="dialog" aria-labelledby="push-hint-title">
		<p>
			<strong id="push-hint-title">{t('agent.pushHint.title')}</strong>
			{done || t('agent.pushHint.text')}
		</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if !done}
			<div class="actions">
				<button type="button" class="btn" onclick={later} disabled={busy}
					>{t('agent.pushHint.later')}</button
				>
				<button type="button" class="btn btn-primary" onclick={allow} disabled={busy}
					>{t('agent.pushHint.allow')}</button
				>
			</div>
		{/if}
	</div>
{/if}

<style>
	.push-hint {
		position: fixed;
		z-index: 850;
		left: max(var(--space-3), env(safe-area-inset-left, 0px));
		right: max(var(--space-3), env(safe-area-inset-right, 0px));
		bottom: calc(env(safe-area-inset-bottom, 0px) + 140px);
		margin: 0 auto;
		max-width: 520px;
		padding: var(--space-3) var(--space-4);
		border: 1px solid var(--border);
		border-radius: 18px;
		background: var(--surface);
		color: var(--text);
		box-shadow: 0 12px 32px rgb(0 0 0 / 0.35);
	}
	p {
		margin: 0;
		line-height: 1.45;
		overflow-wrap: break-word;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: var(--space-2);
		margin-top: var(--space-3);
	}
	.btn {
		min-height: 44px;
		border-radius: 999px;
	}
	.notice-error {
		margin-top: var(--space-2);
	}
</style>
