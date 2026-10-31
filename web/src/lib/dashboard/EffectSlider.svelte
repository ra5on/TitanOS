<script lang="ts">
	let {
		label,
		value,
		min = 0,
		max = 100,
		step = 1,
		unit = '%',
		hint = '',
		onbegin,
		onchange,
	}: {
		label: string;
		value: number;
		min?: number;
		max?: number;
		step?: number;
		unit?: string;
		hint?: string;
		onbegin: () => void;
		onchange: (value: number) => void;
	} = $props();
</script>

<label class="effect-control"
	><span class="effect-label">{label}<output>{value} {unit}</output></span><input
		type="range"
		aria-label={label}
		{min}
		{max}
		{step}
		{value}
		onpointerdown={onbegin}
		onkeydown={(e) => {
			if (
				!e.repeat &&
				[
					'ArrowLeft',
					'ArrowRight',
					'ArrowUp',
					'ArrowDown',
					'Home',
					'End',
					'PageUp',
					'PageDown',
				].includes(e.key)
			)
				onbegin();
		}}
		oninput={(e) => onchange(Number(e.currentTarget.value))}
	/>{#if hint}<small>{hint}</small>{/if}</label
>

<style>
	.effect-control {
		display: block;
		margin: 0 0 20px;
	}
	.effect-label {
		display: flex;
		justify-content: space-between;
		font-size: var(--text-base);
		margin-bottom: 10px;
		align-items: center;
		gap: 8px;
	}
	output {
		font-family: var(--font-mono);
		background: var(--chip);
		color: var(--text);
		font-size: var(--text-sm);
		padding: 3px 7px;
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		min-width: 49px;
		text-align: right;
	}
	input {
		display: block;
		accent-color: var(--accent);
		width: 100%;
		height: 5px;
		cursor: pointer;
		margin: 0 0 9px;
	}
	small {
		display: block;
		color: var(--muted);
		font-size: var(--text-xs);
		line-height: 1.45;
	}
</style>
