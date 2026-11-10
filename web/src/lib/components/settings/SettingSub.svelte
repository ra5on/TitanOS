<script lang="ts">
	// PLAN 78-1 (draft 78): one part of an open setting – name and explanation
	// left, its control right; free content (a list, a note) below it.
	import type { Snippet } from 'svelte';

	type Props = {
		title: string;
		desc?: string;
		/** The id of the control this part names (makes the name its label). */
		for?: string;
		id?: string;
		description?: Snippet;
		control?: Snippet;
		children?: Snippet;
	};
	let { title, desc, for: labelFor, id, description, control, children }: Props = $props();
</script>

<div class="srow-sub" {id}>
	<div class="srow-text">
		{#if labelFor}<label class="srow-title" for={labelFor}>{title}</label>{:else}<span
				class="srow-title">{title}</span
			>{/if}
		{#if description}<div class="srow-desc">{@render description()}</div>{:else if desc}<p
				class="srow-desc"
			>
				{desc}
			</p>{/if}
	</div>
	{#if control}<div class="srow-ctl">{@render control()}</div>{/if}
</div>
{#if children}<div class="srow-block">{@render children()}</div>{/if}
