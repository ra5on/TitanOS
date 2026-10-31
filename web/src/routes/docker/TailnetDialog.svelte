<script lang="ts">
	// "Im Tailnet veröffentlichen" with one click, like Unraid does it for
	// containers: the app becomes a device of its own in the tailnet with
	// https://<name>.<tailnet>.ts.net and a certificate from Tailscale
	// (backend appproxy/oneclick.go). The dialog asks only for what is
	// missing (auth key), explains what is not there (plugin, HTTPS in the
	// tailnet) and follows the device until its address answers.
	// mode "remove" is the simple confirmation for taking it out again.
	import { poll } from '#lib/poll.ts';
	import { api, ApiError, type AppProxyRoute, type AppProxyStatus } from '#lib/api.ts';
	import {
		suggestedProxyName,
		tailnetName,
		tailnetStep,
		TAILSCALE_DNS_URL,
		TAILSCALE_KEYS_URL,
	} from '#lib/app-proxy.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		app: string;
		mode: 'publish' | 'remove';
		/** The WebUI port of the app's main service; 0 lets the NAS choose. */
		port: number;
		onclose: () => void;
		/** Called after a change, so the page can load the links again. */
		onchanged?: () => void;
	};
	let { app, mode, port, onclose, onchanged }: Props = $props();

	type Plugin = { active: boolean; https?: { state: string; enableUrl?: string } };
	type Check = { url: string; reachable: boolean; status?: number; error?: string };
	const CHECKS = 12;

	let status = $state<AppProxyStatus | null>(null);
	let plugin = $state<Plugin | null>(null);
	let loadError = $state('');
	let key = $state('');
	let busy = $state(false);
	let error = $state('');
	// After the click: the page follows the device.
	let following = $state(false);
	let check = $state<Check | null>(null);
	let failed = $state(0);
	let copied = $state(false);

	const route = $derived<AppProxyRoute | null>(
		status?.apps?.find((entry) => entry.app === app) ?? null,
	);
	const step = $derived(tailnetStep(route, check, failed, CHECKS));
	const deviceName = $derived(
		route?.tailscale ? tailnetName(route) : suggestedProxyName(app) || 'app',
	);
	const available = $derived(status?.tailnet?.available ?? false);
	const keySet = $derived(status?.tailnet?.keySet ?? false);
	const url = $derived(route?.tailnet?.url ?? check?.url ?? '');

	async function loadPlugin() {
		try {
			plugin = await api.get<Plugin>('/plugins/tailscale');
		} catch {
			plugin = null;
		}
	}
	async function load() {
		loadError = '';
		try {
			status = await api.get<AppProxyStatus>('/docker/proxy');
		} catch (err) {
			loadError = apiErrorText(err);
		}
	}
	$effect(() => {
		void load();
		void loadPlugin();
	});

	// While following: the state every two seconds, the address once the
	// device runs (every five seconds, a dozen times), the plugin (for the
	// one-click link to HTTPS in the tailnet) every twenty.
	$effect(() => {
		if (!following) return;
		let stop = false;
		let asking = false;
		let asked = 0;
		const tick = async () => {
			if (stop) return;
			await load();
			if (
				stop ||
				tailnetStep(route, check, failed, CHECKS) !== 'check' ||
				asking ||
				Date.now() - asked < 5000
			)
				return;
			asking = true;
			asked = Date.now();
			try {
				const next = await api.post<Check>(
					`/docker/proxy/apps/${encodeURIComponent(app)}/tailnet/check`,
				);
				if (stop) return;
				check = next;
				if (!next.reachable) failed += 1;
			} catch {
				if (!stop) failed += 1;
			} finally {
				asking = false;
			}
		};
		const fast = poll(tick, { interval: 2000 });
		const slow = poll(() => step === 'https-off' && loadPlugin(), {
			interval: 20000,
			immediate: false,
		});
		return () => {
			stop = true;
			fast();
			slow();
		};
	});
	// Done: the page loads its links again once.
	$effect(() => {
		if (step === 'ready') onchanged?.();
	});

	async function publish() {
		if (busy) return;
		busy = true;
		error = '';
		try {
			status = await api.post<AppProxyStatus>(
				`/docker/proxy/apps/${encodeURIComponent(app)}/tailnet`,
				{
					port,
					...(key.trim() ? { tailscaleKey: key.trim() } : {}),
				},
			);
			key = '';
			check = null;
			failed = 0;
			following = true;
			onchanged?.();
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			error = code === 'proxy_invalid' && err instanceof ApiError ? err.message : errorText(code);
			if (code === 'tailnet_key_missing' || code === 'tailnet_unavailable') void load();
		} finally {
			busy = false;
		}
	}

	async function remove() {
		if (busy) return;
		busy = true;
		error = '';
		try {
			await api.delete(`/docker/proxy/apps/${encodeURIComponent(app)}/tailnet`);
			onchanged?.();
			onclose();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	async function copy() {
		try {
			await navigator.clipboard.writeText(url);
			copied = true;
			setTimeout(() => (copied = false), 1500);
		} catch {
			// No clipboard (plain HTTP): the address is there to select.
		}
	}
</script>

{#if mode === 'remove'}
	<Dialog title={t('tailnetPublish.removeTitle', { name: app })} locked={busy} {onclose}>
		<p>
			{t('tailnetPublish.removeText', {
				name: app,
				device: route ? tailnetName(route) : deviceName,
			})}
		</p>
		{#if route?.tailnetOnly}<p class="muted">{t('tailnetPublish.removeRoute')}</p>{/if}
		<p class="muted">{t('tailnetPublish.removeAdmin')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={onclose}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={remove}
				>{t(busy ? 'tailnetPublish.removing' : 'tailnetPublish.remove')}</button
			>
		</div>
	</Dialog>
{:else}
	<Dialog title={t('tailnetPublish.title', { name: app })} locked={busy} {onclose}>
		{#if loadError && !status}
			<div class="notice-error row" role="alert">
				<span>{loadError}</span><button type="button" class="btn small" onclick={load}
					>{t('common.retry')}</button
				>
			</div>
		{:else if !status}
			<p class="muted" role="status">{t('common.loading')}</p>
		{:else if !following}
			<p>{t('tailnetPublish.intro', { name: deviceName })}</p>
			<p class="muted small-text">{t('tailnetPublish.introMore')}</p>
			{#if !available}
				<div class="notice-warn stack-note" role="alert">
					<span>{t('tailnetPublish.noPlugin')}</span>
					<a class="btn small" href="/plugins">{t('tailnetPublish.toPlugins')}</a>
				</div>
			{:else if status.off}
				<div class="notice-warn stack-note" role="alert">
					<span>{t('tailnetPublish.proxyOff')}</span>
					<a class="btn small" href="/settings/proxy">{t('proxy.settingsTitle')}</a>
				</div>
			{:else if !keySet}
				<label class="stack">
					<span>{t('proxy.tailscaleKey')}</span>
					<input
						class="field mono"
						type="password"
						autocomplete="off"
						spellcheck="false"
						placeholder="tskey-auth-…"
						bind:value={key}
						disabled={busy}
					/>
				</label>
				<p class="muted small-text">
					{t('tailnetPublish.keyHint')}
					<a href={TAILSCALE_KEYS_URL} target="_blank" rel="noreferrer"
						>{t('tailnetPublish.keysLink')}</a
					>
				</p>
			{/if}
			{#if plugin?.https?.state === 'disabled'}
				<p class="muted small-text">{t('tailnetPublish.httpsOffBefore')}</p>
			{/if}
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={onclose}
					>{t('common.cancel')}</button
				>
				<button
					type="button"
					class="btn btn-primary"
					disabled={busy ||
						!available ||
						status.off ||
						(!keySet && !key.trim().startsWith('tskey-'))}
					onclick={publish}
					>{t(busy ? 'tailnetPublish.publishing' : 'tailnetPublish.publish')}</button
				>
			</div>
		{:else}
			<ol class="steps" aria-live="polite">
				<li class:done={step !== 'start'} class:now={step === 'start'}>
					{t('tailnetPublish.stepStart')}
				</li>
				<li
					class:done={['https-off', 'check', 'ready', 'unreachable'].includes(step)}
					class:now={step === 'login'}
				>
					{t('tailnetPublish.stepJoin')}
				</li>
				<li
					class:done={['check', 'ready', 'unreachable'].includes(step)}
					class:now={step === 'https-off'}
				>
					{t('tailnetPublish.stepCert')}
				</li>
				<li class:done={step === 'ready'} class:now={step === 'check' || step === 'unreachable'}>
					{t('tailnetPublish.stepCheck')}
				</li>
			</ol>
			{#if step === 'login'}
				<p class="notice-warn">{t('tailnetPublish.login')}</p>
			{:else if step === 'https-off'}
				<div class="notice-warn stack-note" role="alert">
					<span>{t('tailnetPublish.httpsOff')}</span>
					{#if plugin?.https?.enableUrl}
						<a class="btn small" href={plugin.https.enableUrl} target="_blank" rel="noreferrer"
							>{t('tailscale.httpsEnable')}</a
						>
					{:else}
						<a class="btn small" href={TAILSCALE_DNS_URL} target="_blank" rel="noreferrer"
							>{t('tailnetPublish.dnsLink')}</a
						>
					{/if}
					<span class="muted small-text">{t('tailnetPublish.httpsOffWait')}</span>
				</div>
			{:else if step === 'error'}
				<p class="notice-error" role="alert">
					{t('tailnetPublish.failed', { error: route?.tailnet?.error ?? '' })}
				</p>
			{:else if step === 'off'}
				<p class="notice-warn">{t('tailnetPublish.stopped')}</p>
			{:else if step === 'unreachable'}
				<p class="notice-warn">{t('tailnetPublish.unreachable', { error: check?.error ?? '' })}</p>
			{:else if step === 'ready' && check?.status === 502}
				<p class="notice-warn">{t('tailnetPublish.appDown')}</p>
			{:else if step === 'ready'}
				<p class="notice-ok" role="status">{t('tailnetPublish.ready')}</p>
			{:else}
				<p class="muted" role="status">
					{t(step === 'check' ? 'tailnetPublish.checking' : 'tailnetPublish.starting')}
				</p>
			{/if}
			{#if url && (step === 'ready' || step === 'unreachable' || step === 'check')}
				<div class="address">
					<a class="mono" href={url} target="_blank" rel="noreferrer">{url}</a>
					<button type="button" class="btn small" onclick={copy}
						><Icon name={copied ? 'check' : 'copy'} size={14} />{t(
							copied ? 'tailnetPublish.copied' : 'tailnetPublish.copy',
						)}</button
					>
					<a class="btn small" href={url} target="_blank" rel="noreferrer"
						><Icon name="external" size={14} />{t('tailnetPublish.open')}</a
					>
				</div>
			{/if}
			<p class="muted small-text">{t('tailnetPublish.later')}</p>
			<div class="dialog-actions">
				<button type="button" class="btn btn-primary" onclick={onclose}
					>{t(step === 'ready' ? 'common.done' : 'tailnetPublish.background')}</button
				>
			</div>
		{/if}
	</Dialog>
{/if}

<style>
	p {
		margin: 0 0 12px;
	}
	.small-text {
		font-size: var(--text-base);
	}
	.stack-note,
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		margin-bottom: 12px;
	}
	.stack {
		margin-bottom: 8px;
	}
	.steps {
		display: flex;
		flex-direction: column;
		gap: 6px;
		margin: 0 0 14px;
		padding-left: 1.4rem;
		color: var(--muted);
	}
	.steps .done {
		color: var(--text);
	}
	.steps .done::marker {
		color: var(--ok-text);
	}
	.steps .now {
		color: var(--text);
		font-weight: 600;
	}
	.address {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		margin-bottom: 12px;
		overflow-wrap: anywhere;
	}
	.small {
		min-height: 30px;
		padding: 4px 8px;
		font-size: var(--text-sm);
	}
</style>
