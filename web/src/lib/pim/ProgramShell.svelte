<script lang="ts">
	// 1.11 (step 11): the frame of T-Kalender and Kontakte (drafts 01 and 10):
	// the program's own column on the left – its entries from apps.ts, the
	// window shows no side bar on these pages (`page: true`) – and the view on
	// the right. Steps 15/16 put the month view and the list of contacts in
	// place of the body; the column grows the mini month and the books then.
	// While the data cannot be read the body says "nicht verfügbar", never an
	// empty list. On the phone the ⋯ menu has the entries, so no column.
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import Icon from '#lib/components/Icon.svelte';
	import { SYSTEM_APPS, sideFor } from '#lib/desktop/apps.ts';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import type { ShellState } from './shell.ts';

	type Props = { program: 'calendar' | 'contacts'; view: ShellState; children: Snippet };
	let { program, view, children }: Props = $props();

	const phone = $derived(phoneView());
	const role = $derived(session.data?.user?.role ?? 'user');
	const entries = $derived(
		sideFor(SYSTEM_APPS.find((app) => app.id === program)?.side ?? [], role).flatMap((item) =>
			'label' in item && 'icon' in item ? [item] : [],
		),
	);
	const here = $derived(page.url.pathname + page.url.search);
	const UNAVAILABLE: Record<Props['program'], { title: MessageKey; text: MessageKey }> = {
		calendar: { title: 'calendar.unavailable.title', text: 'calendar.unavailable.text' },
		contacts: { title: 'contacts.unavailable.title', text: 'contacts.unavailable.text' },
	};
</script>

<div class="pim" class:phone data-pim-shell={program}>
	{#if !phone}
		<nav
			class="column"
			aria-label={t(program === 'calendar' ? 'desktop.app.calendar' : 'desktop.app.contacts')}
		>
			{#each entries as item (item.href)}
				<a
					class="item"
					class:on={item.href === here}
					href={item.href}
					aria-current={item.href === here ? 'page' : undefined}
				>
					<Icon name={item.icon} size={17} />
					<span>{t(item.label)}</span>
				</a>
			{/each}
		</nav>
	{/if}
	<div class="body" data-pim-state={view.kind}>
		{#if view.kind === 'loading'}
			<p class="muted" role="status">{t('common.loading')}</p>
		{:else if view.kind === 'unavailable'}
			<div class="notice" role="status">
				<Icon name="disk" size={22} />
				<div>
					<p class="title">{t(UNAVAILABLE[program].title)}</p>
					<p class="text">{t(UNAVAILABLE[program].text)}</p>
				</div>
			</div>
		{:else if view.kind === 'error'}
			<p class="notice-error" role="alert">{apiErrorText(view.error)}</p>
		{:else}
			{@render children()}
		{/if}
	</div>
</div>

<style>
	.pim {
		display: flex;
		gap: var(--space-4);
		align-items: flex-start;
		min-width: 0;
	}
	.column {
		flex: none;
		width: 230px;
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: var(--space-2);
		border: 1px solid var(--panel-border, var(--border));
		border-radius: var(--radius-ctl, 10px);
		background: var(--panel-bg, var(--surface));
	}
	.body {
		flex: 1;
		min-width: 0;
	}
	.item {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		min-height: 36px;
		padding: var(--space-1-5) var(--space-2-5);
		border-radius: calc(9px * var(--radius-scale, 1));
		color: var(--text);
		text-decoration: none;
	}
	.item :global(svg) {
		color: var(--muted);
		flex: none;
	}
	.item:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.item.on {
		background: color-mix(in srgb, var(--accent) 18%, transparent);
		color: color-mix(in srgb, var(--accent) 65%, var(--text));
		font-weight: 600;
	}
	.item.on :global(svg) {
		color: var(--accent);
	}
	.item:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.notice {
		display: flex;
		gap: var(--space-3);
		align-items: flex-start;
		padding: var(--space-4);
		border: 1px solid var(--warn-border);
		border-radius: var(--radius-ctl, 10px);
		background: var(--warn-bg);
		color: var(--warn-text);
	}
	.notice :global(svg) {
		flex: none;
	}
	.notice p {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.notice .title {
		font-weight: 600;
		margin-bottom: var(--space-1);
	}
	/* A narrow window: the entries as a row above the view. */
	@media (max-width: 640px) {
		.pim {
			flex-direction: column;
			align-items: stretch;
		}
		.column {
			width: auto;
			flex-direction: row;
			flex-wrap: wrap;
		}
		.item {
			min-height: 44px;
		}
	}
</style>
