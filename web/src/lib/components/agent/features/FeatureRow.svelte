<script lang="ts">
	// One function in the list (PLAN 54): icon, name, what it does, a switch
	// for owners and administrators, a tap opens its settings.
	type Props = {
		icon: string;
		color: string;
		title: string;
		sub?: string;
		on?: boolean;
		/** Without a switch (e.g. for accounts that cannot change it). */
		toggle?: boolean;
		ontoggle?: (on: boolean) => void;
		onopen?: () => void;
	};
	let { icon, color, title, sub, on = false, toggle = true, ontoggle, onopen }: Props = $props();
</script>

<div class="frow">
	<button type="button" class="open" onclick={onopen} disabled={!onopen}>
		<span class="ic" style:--c={color} aria-hidden="true">{icon}</span>
		<span class="t"
			><strong>{title}</strong>{#if sub}<small>{sub}</small>{/if}</span
		>
	</button>
	{#if toggle}
		<label class="sw"
			><input
				type="checkbox"
				role="switch"
				checked={on}
				aria-label={title}
				onchange={(e) => ontoggle?.(e.currentTarget.checked)}
			/><span aria-hidden="true"></span></label
		>
	{/if}
</div>

<style>
	.frow {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 10px 12px;
		border-top: 1px solid color-mix(in srgb, var(--border) 70%, transparent);
	}
	.frow:first-child {
		border-top: 0;
	}
	.open {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 0;
		border: 0;
		background: none;
		color: inherit;
		text-align: left;
		cursor: pointer;
		font: inherit;
	}
	.open:disabled {
		cursor: default;
	}
	.ic {
		width: 30px;
		height: 30px;
		flex-shrink: 0;
		display: grid;
		place-items: center;
		border-radius: 9px;
		font-size: 16px;
		background: color-mix(in srgb, var(--c) 18%, transparent);
	}
	.t {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.t small {
		color: var(--muted);
		line-height: 1.35;
	}
	.sw {
		position: relative;
		flex-shrink: 0;
		width: 44px;
		height: 26px;
	}
	.sw input {
		position: absolute;
		inset: 0;
		opacity: 0;
		margin: 0;
		cursor: pointer;
	}
	.sw span {
		position: absolute;
		inset: 0;
		border-radius: 13px;
		background: color-mix(in srgb, var(--text) 18%, transparent);
		transition: background 0.15s;
		pointer-events: none;
	}
	.sw span::after {
		content: '';
		position: absolute;
		top: 3px;
		left: 3px;
		width: 20px;
		height: 20px;
		border-radius: 50%;
		background: #fff;
		transition: left 0.15s;
	}
	.sw input:checked + span {
		background: var(--accent);
	}
	.sw input:checked + span::after {
		left: 21px;
	}
	.sw input:focus-visible + span {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
</style>
