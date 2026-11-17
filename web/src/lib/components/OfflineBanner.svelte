<script lang="ts">
	// 1.07: the NAS stopped answering while TitanOS is open (desktop, the
	// phone's home screen, the agent app – the other pages have their
	// ConnectionBanner). What is on screen stays; changes are not sent until
	// the NAS answers again (api.ts), and a typed text stays where it is.
	// "Jetzt prüfen" asks the NAS at once; it is also asked every 10 seconds.
	import { apiFetch } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { connection, markBack } from '#lib/offline.svelte.ts';

	let busy = $state(false);
	async function check() {
		if (busy) return;
		busy = true;
		try {
			const res = await apiFetch('/health', { signal: AbortSignal.timeout(8000) });
			if (res.status < 500) markBack();
		} catch {
			// Still not there.
		} finally {
			busy = false;
		}
	}
	$effect(() => {
		if (!connection.lost) return;
		const timer = setInterval(() => void check(), 10_000);
		return () => clearInterval(timer);
	});
</script>

{#if connection.lost}
	<div class="offline-banner notice-warn" role="status">
		<p>
			<strong>{t('offline.lostTitle')}</strong>
			{t('offline.lostText')}
		</p>
		<button type="button" class="btn" onclick={check} disabled={busy}
			>{busy ? t('offline.checking') : t('offline.check')}</button
		>
	</div>
{/if}

<style>
	/* Below the status bar of the phone and the agent's floating head. */
	.offline-banner {
		position: fixed;
		z-index: 900;
		top: calc(env(safe-area-inset-top, 0px) + var(--agent-top, 0px) + var(--space-2));
		left: max(var(--space-3), env(safe-area-inset-left, 0px));
		right: max(var(--space-3), env(safe-area-inset-right, 0px));
		margin: 0 auto;
		max-width: 640px;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2) var(--space-3);
		box-shadow: 0 8px 24px rgb(0 0 0 / 0.25);
		/* Opaque over the page: the warning's tint on the surface. */
		background: linear-gradient(var(--warn-bg), var(--warn-bg)), var(--surface);
	}
	p {
		margin: 0;
		flex: 1 1 14em;
		overflow-wrap: break-word;
	}
	.btn {
		min-height: 44px;
	}
</style>
