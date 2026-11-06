<script lang="ts">
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import { session } from '#lib/session.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import ToolsSidebar from '#lib/components/ToolsSidebar.svelte';
	import { terminal } from '#lib/terminal.svelte.ts';
	// PLAN 61: in a window of the desktop the window's side bar lists the tools.
	import { inWindow } from '#lib/desktop/frame.ts';
	let { children }: { children: Snippet } = $props();
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const overview = $derived(page.url.pathname.replace(/\/$/, '') === '/tools');
	// The task manager (PLAN 61) is the one tools page users see: its
	// overview, read only, without the sidebar of the tools.
	const forUsers = $derived(page.url.pathname.replace(/\/$/, '') === '/tools/taskmanager');
	// As in the settings: the sidebar starts below banners and an open
	// terminal covers the bottom, so its height follows the space really visible.
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
		void aside;
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
</script>

<!-- Tools look like the settings (decision of the owner of 4 Nov 2026,
     variant B): a sidebar beside every tools page on the desktop; on phones
     the overview is the list and the pages behind it get a way back. -->
{#if admin}<div class="tools-shell" class:in-window={inWindow}>
		<div
			class="tools-aside tile"
			bind:this={aside}
			style:max-height={room ? `${room}px` : undefined}
		>
			<ToolsSidebar />
		</div>
		<div class="tools-main">
			{#if !overview && !inWindow}<a class="tools-back" href="/tools">‹ {t('nav.tools')}</a>{/if}
			<div class="settings-page column">{@render children()}</div>
		</div>
	</div>{:else if forUsers && session.data?.user}<div class="settings-page column">
		{@render children()}
	</div>{:else}<p class="notice-error" role="alert">
		{t('error.forbidden')}
	</p>{/if}

<style>
	.tools-shell.in-window {
		display: block;
	}
	.tools-shell.in-window > .tools-aside {
		display: none;
	}
	.tools-shell {
		display: grid;
		grid-template-columns: 15.5rem minmax(0, 1fr);
		gap: 28px;
		align-items: start;
		min-width: 0;
	}
	.tools-aside {
		position: sticky;
		top: calc(var(--header-height, 60px) + var(--page-pad));
		max-height: calc(100dvh - var(--header-height, 60px) - 2 * var(--page-pad));
		overflow-y: auto;
		/* A surface of its own keeps the list readable over a background image. */
		padding: 10px var(--space-2) var(--space-3);
	}
	.tools-main {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
		min-width: 0;
	}
	.tools-back {
		display: none;
	}
	/* The sidebar says where one is; the way back above the title only on phones. */
	.tools-main :global(.page-head .crumbs:has(> a[href='/tools'])) {
		display: none;
	}
	@media (max-width: 760px) {
		.tools-shell {
			display: block;
		}
		.tools-aside {
			display: none;
		}
		.tools-back {
			display: inline-flex;
			align-items: center;
			align-self: flex-start;
			min-height: 44px;
			margin: -10px 0 -6px;
			font-size: var(--text-md);
			text-decoration: none;
		}
	}
</style>
