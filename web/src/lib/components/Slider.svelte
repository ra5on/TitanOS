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
		gap: 6px;
	}
	.row {
		display: flex;
		justify-content: space-between;
		gap: 12px;
		font-size: 0.875rem;
	}
	.row span {
		font-size: 0.8125rem;
	}
	input {
		width: 100%;
	}
</style>
