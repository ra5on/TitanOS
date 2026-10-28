<script lang="ts">
	// /login-preview: the real login page with the look being edited in
	// Settings › Appearance, without logging out. Owners and administrators only;
	// the form sends nothing.
	import { onMount } from 'svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import Login from '#lib/pages/Login.svelte';
	import Problem from '#lib/pages/Problem.svelte';
	import { session } from '#lib/session.svelte.ts';
	import { sanitizeLoginLook, type LoginLook } from './look.ts';
	import { loginLook, PREVIEW_KEY } from './state.svelte.ts';

	let draft = $state<{ look: LoginLook; imageURL: string } | null>(null);
	const role = $derived(session.data?.user?.role);
	const allowed = $derived(role === 'owner' || role === 'admin');

	function read() {
		try {
			const raw = localStorage.getItem(PREVIEW_KEY);
			if (!raw) return;
			const data = JSON.parse(raw) as { look?: unknown; imageURL?: unknown };
			const imageURL = typeof data.imageURL === 'string' && data.imageURL.startsWith('blob:' + location.origin) ? data.imageURL : '';
			draft = { look: sanitizeLoginLook(data.look), imageURL };
		} catch {
			draft = null;
		}
	}

	onMount(() => {
		read();
		const changed = (event: StorageEvent) => {
			if (event.key === PREVIEW_KEY) read();
		};
		window.addEventListener('storage', changed);
		return () => window.removeEventListener('storage', changed);
	});
</script>

{#if !allowed}
	<Problem title={t('loginLook.previewTitle')} text={t('loginLook.previewForbidden')} />
{:else}
	<p class="badge" role="status">{t('loginLook.previewBadge')}</p>
	<Login preview look={draft ? draft.look : loginLook.value} imageURL={draft?.imageURL ?? ''} />
{/if}

<style>
	.badge {
		position: fixed;
		top: 12px;
		left: 50%;
		transform: translateX(-50%);
		z-index: 2;
		margin: 0;
		padding: 4px 12px;
		border-radius: 999px;
		background: var(--accent);
		color: var(--on-accent);
		font-size: 0.75rem;
		font-weight: 600;
		white-space: nowrap;
	}
</style>
