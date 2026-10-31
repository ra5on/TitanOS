<script lang="ts" module>
	export type HttpsStatus = {
		name?: string;
		state: string;
		enabled?: boolean;
		notAfter?: string;
		error?: string;
		enableUrl?: string;
	};
</script>

<script lang="ts">
	// The certificate of the NAS's own name in the tailnet (backend
	// tailscale/cert.go): a switch, off by default, with what Tailscale
	// needs for it and what it makes public.
	import { api } from '#lib/api.ts';
	import { formatTime } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		https: HttpsStatus | undefined;
		disabled: boolean;
		/** Gets the whole Tailscale status after switching. */
		onupdate: (status: unknown) => void;
	};
	let { https, disabled, onupdate }: Props = $props();

	let busy = $state(false),
		error = $state('');
	const enabled = $derived(!!https?.enabled);

	async function change(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		busy = true;
		error = '';
		try {
			onupdate(await api.put<unknown>('/plugins/tailscale/https', { enabled: input.checked }));
		} catch (err) {
			input.checked = enabled;
			error = apiErrorText(err);
		}
		busy = false;
	}
</script>

<div class="https-box">
	<h4>{t('tailscale.httpsTitle')}</h4>
	<label class="check"
		><input type="checkbox" checked={enabled} disabled={disabled || busy} onchange={change} />{t(
			'tailscale.httpsSwitch',
			{ name: https?.name || t('tailscale.httpsTheName') },
		)}</label
	>
	<p class="muted">{t('tailscale.httpsWhat')}</p>
	<div class="needs">
		<p>{t('tailscale.httpsNeeds')}</p>
		<ul>
			<li>{t('tailscale.httpsNeedMagicDNS')}</li>
			<li>{t('tailscale.httpsNeedCertificates')}</li>
		</ul>
	</div>
	<p class="notice-warn">{t('tailscale.httpsPublic')}</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if enabled && https}
		{#if https.state === 'ok' && https.name}<p class="https" role="status">
				<span>{t('tailscale.httpsReady')}</span>
				<a class="mono address" href="https://{https.name}">https://{https.name}</a
				>{#if https.notAfter}<span class="muted">
						· {t('tailscale.httpsUntil', {
							date: formatTime(new Date(https.notAfter)),
						})}</span
					>{/if}
			</p>
		{:else if https.state === 'disabled'}<div class="notice-warn https-off">
				<p>
					{t(https.enableUrl ? 'tailscale.httpsOffLink' : 'tailscale.httpsDisabled', {
						name: https.name ?? '',
					})}
				</p>
				{#if https.enableUrl}<a
						class="btn btn-primary"
						href={https.enableUrl}
						target="_blank"
						rel="noopener noreferrer">{t('tailscale.httpsEnable')}</a
					>{/if}
			</div>
		{:else if https.state === 'error'}<p class="notice-warn">
				{t('tailscale.httpsError')}
				{https.error ?? ''}
			</p>
		{:else}<p class="muted" role="status">{t('tailscale.httpsWaiting')}</p>{/if}
	{/if}
</div>

<style>
	.https-box {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 14px 16px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-tile);
	}
	h4 {
		margin: 0;
		font-size: 0.9375rem;
		font-weight: 600;
	}
	p {
		margin: 0;
	}
	.check {
		display: flex;
		align-items: center;
		gap: 10px;
		font-weight: 500;
	}
	.needs ul {
		margin: 4px 0 0;
		padding-left: 20px;
	}
	.mono {
		font-family: var(--font-mono);
	}
	.address {
		overflow-wrap: anywhere;
	}
	.https-off {
		display: flex;
		flex-direction: column;
		gap: 10px;
		align-items: flex-start;
	}
</style>
