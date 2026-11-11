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
	import SettingSub from './settings/SettingSub.svelte';
	import { formatDateTime } from '#lib/format.ts';
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

<!-- PLAN 78-5: a part of the Tailscale row – the switch beside its name and
     explanation, what it needs and its state below; no box of its own. -->
<SettingSub title={t('tailscale.httpsTitle')} for="ts-https">
	{#snippet description()}
		{t('tailscale.httpsSwitch', { name: https?.name || t('tailscale.httpsTheName') })}
		<span class="what">{t('tailscale.httpsWhat')}</span>
	{/snippet}
	{#snippet control()}
		<input
			id="ts-https"
			class="switch"
			type="checkbox"
			role="switch"
			aria-label={t('tailscale.httpsSwitch', {
				name: https?.name || t('tailscale.httpsTheName'),
			})}
			checked={enabled}
			disabled={disabled || busy}
			onchange={change}
		/>
	{/snippet}
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
						{' '}· {t('tailscale.httpsUntil', {
							date: formatDateTime(new Date(https.notAfter)),
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
</SettingSub>

<style>
	p {
		margin: 0;
	}
	.what {
		display: block;
	}
	.needs ul {
		margin: var(--space-1) 0 0;
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
		gap: var(--space-2-5);
		align-items: flex-start;
	}
</style>
