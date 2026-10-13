<script lang="ts">
	// Shown instead of the UI when the NAS cannot be used at all.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { refreshSession } from '#lib/session.svelte.ts';
	import AuthCard from './AuthCard.svelte';

	type Props = { title: string; text: string };
	let { title, text }: Props = $props();

	let busy = $state(false);

	async function retry() {
		busy = true;
		await refreshSession();
		busy = false;
	}
</script>

<AuthCard {title}>
	<p class="notice-warn" role="alert">{text}</p>
	<button class="btn" type="button" onclick={retry} disabled={busy}>{t('common.retry')}</button>
</AuthCard>
