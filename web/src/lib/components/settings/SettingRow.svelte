<script lang="ts" module>
	// Which rows a browser left open or closed, per page and row (like the
	// groups of style E3 before): { "/settings/energy#schedule": true }.
	const KEY = 'titanos.settings.rows';
	function stored(): Record<string, boolean> {
		try {
			return JSON.parse(localStorage.getItem(KEY) ?? '{}') as Record<string, boolean>;
		} catch {
			return {};
		}
	}
	function remember(key: string, open: boolean) {
		try {
			const all = stored();
			delete all[key];
			all[key] = open;
			const keys = Object.keys(all);
			for (const old of keys.slice(0, Math.max(0, keys.length - 300))) delete all[old];
			localStorage.setItem(KEY, JSON.stringify(all));
		} catch {
			/* private window: only for this visit */
		}
	}
</script>

<script lang="ts">
	// PLAN 78-1 (owner 10.11.2026, draft 78): one setting is one flat row –
	// symbol, name, a short explanation, on the right its value, switch,
	// choice or button. A setting with parts opens downward inside its row
	// (chevron on the right); closed, it says its state as a summary on the
	// right. Open or closed is remembered per row in this browser. With `href`
	// the row is a link to another page. Styles: app.css (.srow).
	import type { Snippet } from 'svelte';
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		title: string;
		/** A short explanation below the name. */
		desc?: string;
		icon?: IconName;
		/** Names the row for its remembered state and as an anchor (#id). */
		id?: string;
		/** The id of the control this row names (makes the name its label). */
		for?: string;
		/** The state in a few words, at the right (e.g. "An · 2 Zeiten"). */
		summary?: string;
		/** The row leads to another page. */
		href?: string;
		/** Open at first; the browser's remembered state wins. */
		open?: boolean;
		/** Other anchors on this page that open the row (parts inside it). */
		anchors?: string[];
		/** After the name: a badge ("neu"), a help tip. */
		badge?: Snippet;
		/** Instead of `desc` when it needs markup. */
		description?: Snippet;
		/** Value, switch, choice or button at the right. */
		control?: Snippet;
		/** The parts: SettingSub rows and .srow-block content. */
		children?: Snippet;
	};
	let {
		title,
		desc,
		icon,
		id,
		for: labelFor,
		summary,
		href,
		open = $bindable(false),
		anchors = [],
		badge,
		description,
		control,
		children,
	}: Props = $props();

	const uid = $props.id();
	const bodyId = $derived(`${id ?? uid}-parts`);
	const key = $derived(id ? `${page.url.pathname.replace(/\/$/, '')}#${id}` : '');
	// The remembered state, once the page runs in the browser.
	$effect(() => {
		if (!key || !children) return;
		const was = stored()[key];
		if (typeof was === 'boolean') untrack(() => (open = was));
	});
	// A link to this row (search, a notification) opens it and brings it into view.
	let el = $state<HTMLElement>();
	$effect(() => {
		const hash = page.url.hash.slice(1);
		if (!hash || !children || !(hash === id || anchors.includes(hash))) return;
		untrack(() => (open = true));
		requestAnimationFrame(() =>
			(document.getElementById(hash) ?? el)?.scrollIntoView({ block: 'start' }),
		);
	});
	// PLAN 86: the settings search opens the row it leads to (settings-jump.ts);
	// that is no choice of the user, so it is not remembered.
	$effect(() => {
		const row = el;
		if (!row || !children) return;
		const opener = () => (open = true);
		row.addEventListener('titanos-open', opener);
		return () => row.removeEventListener('titanos-open', opener);
	});
	function toggle() {
		open = !open;
		if (key) remember(key, open);
	}
	// A click on the row opens it too; its own controls keep their click.
	function headClick(event: MouseEvent) {
		if (!children) return;
		if ((event.target as Element).closest('a, button, input, select, textarea, label')) return;
		toggle();
	}
</script>

{#snippet text()}
	<div class="srow-text">
		{#if labelFor}<label class="srow-title" for={labelFor}
				>{title}{#if badge}{@render badge()}{/if}</label
			>{:else if children}<h3 class="srow-title">
				{title}{#if badge}{@render badge()}{/if}
			</h3>{:else}<span class="srow-title"
				>{title}{#if badge}{@render badge()}{/if}</span
			>{/if}
		{#if description}<div class="srow-desc">{@render description()}</div>{:else if desc}<p
				class="srow-desc"
			>
				{desc}
			</p>{/if}
	</div>
{/snippet}

{#if href}
	<a class="srow" class:plain={!icon} {href} {id}>
		<div class="srow-head">
			{#if icon}<span class="srow-ico" aria-hidden="true"><Icon name={icon} size={19} /></span>{/if}
			{@render text()}
			{#if summary || control}<span class="srow-ctl"
					>{#if summary}<span class="srow-summary">{summary}</span
						>{/if}{#if control}{@render control()}{/if}</span
				>{/if}
			<span class="srow-chev" aria-hidden="true"><Icon name="chevronRight" size={16} /></span>
		</div>
	</a>
{:else}
	<div class="srow" class:open={!!children && open} class:plain={!icon} {id} bind:this={el}>
		<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
		<div class="srow-head" class:can-open={!!children} onclick={headClick}>
			{#if icon}<span class="srow-ico" aria-hidden="true"><Icon name={icon} size={19} /></span>{/if}
			{@render text()}
			{#if summary || control}<div class="srow-ctl">
					{#if summary}<span class="srow-summary">{summary}</span
						>{/if}{#if control}{@render control()}{/if}
				</div>{/if}
			{#if children}<button
					type="button"
					class="srow-chev"
					aria-expanded={open}
					aria-controls={bodyId}
					aria-label="{title}: {t(open ? 'settings.groupClose' : 'settings.groupOpen')}"
					onclick={toggle}><Icon name="chevronDown" size={16} /></button
				>{/if}
		</div>
		{#if children && open}<div class="srow-body" id={bodyId}>{@render children()}</div>{/if}
	</div>
{/if}
