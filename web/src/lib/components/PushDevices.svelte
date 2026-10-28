<script lang="ts">
	// Notifications on this device (Web Push): switch on here, then this
	// browser or the app on the home screen shows what the bell shows, from
	// the level set under "send from". iPhones only from the home screen.
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { formatTime } from '#lib/format.ts';
	import Dialog from './Dialog.svelte';
	import { appleMobile, currentSubscription, deviceName, pushSupported, standalone, subscribe, type PushDevice } from '#lib/web-push.ts';

	let devices = $state<PushDevice[] | null>(null);
	let publicKey = $state('');
	let thisDevice = $state('');
	let busy = $state(false);
	let message = $state('');
	let error = $state('');
	let removing = $state<PushDevice | null>(null);
	let supported = $state(true);
	let needsHomeScreen = $state(false);
	let denied = $state(false);

	function fail(err: unknown) {
		error = err instanceof Error && err.message === 'denied' ? t('push.denied') : errorText(err instanceof ApiError ? err.code : 'internal');
	}

	async function load() {
		try {
			const answer = await api.get<{ publicKey: string; devices: PushDevice[] }>('/notifications/push');
			publicKey = answer.publicKey;
			devices = answer.devices;
			const subscription = await currentSubscription().catch(() => null);
			const id = subscription ? await idOf(subscription) : '';
			thisDevice = devices.some((d) => d.id === id) ? id : '';
		} catch (err) {
			fail(err);
		}
	}

	// The NAS names a device by a hash of its endpoint (first 8 bytes of SHA-256).
	async function idOf(subscription: PushSubscription): Promise<string> {
		const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(subscription.endpoint));
		return [...new Uint8Array(digest).slice(0, 8)].map((b) => b.toString(16).padStart(2, '0')).join('');
	}

	onMount(() => {
		supported = pushSupported();
		needsHomeScreen = appleMobile(navigator.userAgent, navigator.maxTouchPoints) && !standalone();
		denied = supported && Notification.permission === 'denied';
		void load();
	});

	async function enable() {
		busy = true;
		error = message = '';
		try {
			const device = await subscribe(publicKey, deviceName(navigator.userAgent, navigator.maxTouchPoints));
			thisDevice = device.id;
			await load();
			message = t('push.enabled');
		} catch (err) {
			fail(err);
			denied = Notification.permission === 'denied';
		} finally {
			busy = false;
		}
	}

	async function test(device: PushDevice) {
		busy = true;
		error = message = '';
		try {
			await api.post(`/notifications/push/${device.id}/test`, {});
			message = t('push.testSent', { name: device.name });
		} catch (err) {
			fail(err);
			await load();
		} finally {
			busy = false;
		}
	}

	async function remove(device: PushDevice) {
		busy = true;
		error = message = '';
		try {
			await api.delete(`/notifications/push/${device.id}`);
			if (device.id === thisDevice) {
				const subscription = await currentSubscription().catch(() => null);
				await subscription?.unsubscribe();
				thisDevice = '';
			}
			removing = null;
			await load();
		} catch (err) {
			fail(err);
		} finally {
			busy = false;
		}
	}
</script>

<section class="tile panel" id="push" aria-labelledby="push-title">
	<h2 class="tile-title" id="push-title">{t('push.title')}</h2>
	<p class="muted">{t('push.intro')}</p>
	{#if !supported && !needsHomeScreen}
		<p class="notice-warn">{t(window.isSecureContext ? 'push.unsupported' : 'push.needsHttps')}</p>
	{:else if needsHomeScreen}
		<p class="notice-warn">{t('push.homeScreen')}</p>
	{:else if denied}
		<p class="notice-warn">{t('push.blocked')}</p>
	{:else if !thisDevice}
		<div class="row-actions"><button type="button" class="btn btn-primary" disabled={busy || !publicKey} onclick={enable}>{t('push.enable')}</button></div>
	{/if}
	{#if message}<p class="muted" role="status">{message}</p>{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if devices?.length}
		<ul class="devices divided">
			{#each devices as device (device.id)}
				<li>
					<span class="name">{device.name || device.service}{#if device.id === thisDevice}<span class="chip ok">{t('push.thisDevice')}</span>{/if}</span>
					<span class="muted small">{t('push.since', { date: formatTime(new Date(device.created)) })}</span>
					<span class="row-actions">
						<button type="button" class="btn small" disabled={busy} onclick={() => test(device)}>{t('push.test')}</button>
						<button type="button" class="btn small" disabled={busy} onclick={() => (removing = device)}>{t('push.remove')}</button>
					</span>
				</li>
			{/each}
		</ul>
	{:else if devices}
		<p class="muted">{t('push.none')}</p>
	{/if}
</section>

{#if removing}
	<Dialog title={t('push.remove')} onclose={() => (removing = null)}>
		<p>{t('push.removeText', { name: removing.name || removing.service })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={() => removing && remove(removing)}>{t('push.remove')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.devices {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.devices li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 12px;
		padding: 8px 0;
	}
	.name {
		display: inline-flex;
		gap: 8px;
		align-items: center;
		font-weight: 500;
	}
	.devices .row-actions {
		margin-left: auto;
	}
</style>
