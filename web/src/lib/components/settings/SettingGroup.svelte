<script lang="ts">
	// PLAN 78-1 (draft 78): a group of settings is a plain heading above its
	// rows – no box around it. Styles: app.css (.sgroup).
	import type { Snippet } from 'svelte';

	type Props = {
		/** Without a title the rows simply stand together. */
		title?: string;
		id?: string;
		/** A sentence below the heading. */
		desc?: string;
		/** Buttons of the group (e.g. "Hinzufügen"), at the right of the heading. */
		tools?: Snippet;
		children: Snippet;
	};
	let { title, id, desc, tools, children }: Props = $props();
	const uid = $props.id();
	const headId = $derived(id ? `${id}-title` : `${uid}-title`);
</script>

<section class="sgroup" {id} aria-labelledby={title ? headId : undefined}>
	{#if title || tools}
		<div class="sgroup-head">
			{#if title}<h2 class="sgroup-title" id={headId}>{title}</h2>{/if}
			{#if tools}<div class="sgroup-tools">{@render tools()}</div>{/if}
			{#if desc}<p class="sgroup-desc">{desc}</p>{/if}
		</div>
	{/if}
	{@render children()}
</section>
