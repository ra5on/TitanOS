<script lang="ts">
	import type { Snippet } from 'svelte';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { session } from '#lib/session.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { userSettingsPaths } from '#lib/settings-search.ts';
	import { loadSettingsStates } from '#lib/settings-states.svelte.ts';
	import { markAreaSeen } from '#lib/settings-seen.svelte.ts';
	import SettingsSidebar from '#lib/components/SettingsSidebar.svelte';
	import { terminal } from '#lib/terminal.svelte.ts';
	let { children }: { children: Snippet } = $props();
	// The sidebar starts below banners and the tasks row, not right under the
	// header, and an open terminal covers the bottom: its height follows the
	// space that is really visible, so the last entry can always be reached.
	let aside = $state<HTMLElement>();
	let room = $state(0);
	function measure() {
		if (!aside) return;
		const covered = terminal.open && !terminal.full ? terminal.height : 0;
		const pad = parseFloat(getComputedStyle(aside).getPropertyValue('--page-pad')) || 20;
		room = Math.max(
			160,
			window.innerHeight - Math.max(aside.getBoundingClientRect().top, 0) - covered - pad,
		);
	}
	$effect(() => {
		void terminal.open;
		void terminal.height;
		void terminal.full;
		measure();
		let frame = 0;
		const later = () => {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(measure);
		};
		window.addEventListener('scroll', later, { passive: true });
		window.addEventListener('resize', later);
		return () => {
			cancelAnimationFrame(frame);
			window.removeEventListener('scroll', later);
			window.removeEventListener('resize', later);
		};
	});
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const allowed = $derived(
		admin || userSettingsPaths.includes(page.url.pathname.replace(/\/$/, '')),
	);
	const overview = $derived(page.url.pathname.replace(/\/$/, '') === '/settings');
	// The states beside some areas are read once when the settings open, not
	// on every page change inside them and never on a timer.
	onMount(() => loadSettingsStates(admin));
	// Opening an area once takes its "new" badge away.
	$effect(() => markAreaSeen(page.url.pathname));
</script>

<!-- A sidebar beside every settings page (desktop); on phones the overview is
     the list and the pages behind it get a way back instead. -->
<div class="settings-shell" class:overview>
	<div
		class="settings-aside tile"
		bind:this={aside}
		style:max-height={room ? `${room}px` : undefined}
	>
		<SettingsSidebar />
	</div>
	<div class="settings-main">
		{#if !overview}<a class="settings-back" href="/settings">‹ {t('nav.settings')}</a>{/if}
		{#if allowed}<div class="settings-page column">{@render children()}</div>{:else}<p
				class="notice-error"
				role="alert"
			>
				{t('error.forbidden')}
			</p>{/if}
	</div>
</div>

<style>
	.settings-shell {
		display: grid;
		grid-template-columns: 15.5rem minmax(0, 1fr);
		gap: 28px;
		align-items: start;
		min-width: 0;
	}
	.settings-aside {
		position: sticky;
		top: calc(var(--header-height, 60px) + var(--page-pad));
		max-height: calc(100dvh - var(--header-height, 60px) - 2 * var(--page-pad));
		overflow-y: auto;
		/* At its end the wheel moves the page on (no overscroll-behavior: contain). */
		/* A surface of its own keeps the list readable over a background image. */
		padding: 10px 8px 12px;
	}
	.settings-main {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
		min-width: 0;
	}
	.settings-back {
		display: none;
	}
	/* The sidebar says where one is; the way back above the title only on phones. */
	.settings-main :global(.page-head .crumbs:has(> a[href='/settings'])) {
		display: none;
	}
	/* Settings are dense forms: buttons and fields a little smaller than
	   elsewhere, still following font size and density. Touch keeps 44 px. */
	@media (min-width: 761px) and (pointer: fine) {
		.settings-shell {
			--control-height: 1.875rem;
		}
		:global([data-density='compact']) .settings-shell {
			--control-height: 1.75rem;
		}
		.settings-shell :global(:is(.btn, .field)) {
			font-size: 0.8125rem;
		}
		.settings-shell :global(.btn) {
			padding: 0 0.75rem;
		}
	}
	@media (max-width: 760px) {
		.settings-shell {
			display: block;
		}
		.settings-aside {
			display: none;
		}
		.settings-back {
			display: inline-flex;
			align-items: center;
			align-self: flex-start;
			min-height: 44px;
			margin: -10px 0 -6px;
			font-size: 0.875rem;
			text-decoration: none;
		}
	}
</style>
