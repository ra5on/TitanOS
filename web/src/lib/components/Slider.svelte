<script lang="ts">
	// A labelled range input that shows its value.
	type Props = {
		id: string;
		label: string;
		value: number;
		limit: { min: number; max: number; step: number };
		unit: string;
	};
	let { id, label, value = $bindable(), limit, unit }: Props = $props();
</script>

<div class="slider">
	<div class="row">
		<label for={id}>{label}</label>
		<span class="mono">{value} {unit}</span>
	</div>
	<input {id} type="range" min={limit.min} max={limit.max} step={limit.step} bind:value />
</div>

<style>
	.slider {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
	}
	.row {
		display: flex;
		justify-content: space-between;
		gap: var(--space-3);
		font-size: var(--text-md);
	}
	.row span {
		font-size: var(--text-base);
	}
	input {
		width: 100%;
	}
</style>
