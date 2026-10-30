<script lang="ts">
	import { system, refreshSystem } from '#lib/system.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	let now = $state(Date.now());
	let retrying = $state(false);
	$effect(() => {
		const timer = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(timer);
	});
	const stale = $derived(
		!system.reachable || (system.lastUpdated > 0 && now - system.lastUpdated > 15000),
	);
	async function retry() {
		retrying = true;
		try {
			await refreshSystem();
		} finally {
			retrying = false;
		}
	}
</script>

{#if stale}
	<aside class="notice-warn connection" role="status">
		<div>
			<strong>{t('ui.connectionLost')}</strong>
			<p>
				{system.lastUpdated
					? t('ui.stale', { seconds: Math.max(0, Math.floor((now - system.lastUpdated) / 1000)) })
					: t('ui.neverConnected')}
			</p>
		</div>
		<button class="btn" type="button" disabled={retrying} onclick={retry}
			>{t(retrying ? 'ui.working' : 'ui.retry')}</button
		>
	</aside>
{/if}

<style>
	.connection {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	p {
		margin-top: 4px;
	}
</style>
