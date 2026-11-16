<script lang="ts">
	// Opens a dialog from a button and closes it by leaving it out, as pages do.
	import Dialog from '#lib/components/Dialog.svelte';
	let open = $state(false);
</script>

<button type="button" onclick={() => (open = true)}>open</button>
{#if open}
	<Dialog title="Probe" onclose={() => (open = false)}>
		<div class="dialog-actions">
			<button type="button" onclick={() => (open = false)}>cancel</button>
		</div>
	</Dialog>
{/if}
