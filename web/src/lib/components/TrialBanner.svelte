<script lang="ts">
	// A change that runs on trial (network, firewall): what is going on, how
	// long it still runs, and the two ways out. Anything else (a link to the
	// new address) comes as children.
	import type { Snippet } from 'svelte';
	let {
		title,
		hint,
		confirmLabel,
		revertLabel,
		busy = false,
		onconfirm,
		onrevert,
		children
	}: {
		title: string;
		hint: string;
		confirmLabel: string;
		revertLabel: string;
		busy?: boolean;
		onconfirm: () => void;
		onrevert: () => void;
		children?: Snippet;
	} = $props();
</script>

<section class="notice-warn trial" role="status">
	<strong>{title}</strong>
	<p>{hint}</p>
	{@render children?.()}
	<div class="row-actions">
		<button class="btn" type="button" disabled={busy} onclick={onrevert}>{revertLabel}</button
		><button class="btn btn-primary" type="button" disabled={busy} onclick={onconfirm}>{confirmLabel}</button>
	</div>
</section>

<style>
	.trial {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.trial p {
		margin: 0;
	}
	.row-actions {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		justify-content: flex-end;
	}
</style>
