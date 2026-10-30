<script lang="ts">
	import Dialog from '#lib/components/Dialog.svelte';

	// A plain question of the editor: one button keeps things as they are, the other acts.
	let {
		title,
		text,
		keep,
		confirm,
		onconfirm,
		onclose,
	}: {
		title: string;
		text: string;
		keep: string;
		confirm: string;
		onconfirm: () => void;
		onclose: () => void;
	} = $props();
</script>

<Dialog {title} {onclose}
	><p>{text}</p>
	<div class="dialog-actions">
		<button class="btn" onclick={onclose}>{keep}</button><button
			type="button"
			class="btn btn-warn"
			onclick={onconfirm}>{confirm}</button
		>
	</div></Dialog
>
