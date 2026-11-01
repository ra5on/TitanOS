<script lang="ts">
	// An on/off switch of the app form with its state as text beside it.
	import { t } from '#lib/i18n/index.svelte.ts';
	type Props = {
		checked: boolean;
		label: string;
		/** Text beside the switch; "On"/"Off" if left out. */
		text?: string;
		disabled?: boolean;
		onchange?: (checked: boolean) => void;
	};
	let { checked = $bindable(), label, text, disabled = false, onchange }: Props = $props();
</script>

<button
	type="button"
	class="toggle"
	role="switch"
	aria-checked={checked}
	aria-label={label}
	{disabled}
	onclick={() => {
		checked = !checked;
		onchange?.(checked);
	}}
	><span class="track" aria-hidden="true"><span class="knob"></span></span><span
		>{text ?? t(checked ? 'appform.on' : 'appform.off')}</span
	></button
>

<style>
	.toggle {
		display: inline-flex;
		align-items: center;
		gap: 10px;
		min-height: var(--control-height);
		padding: 0;
		background: none;
		border: 0;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		text-align: left;
		cursor: pointer;
	}
	.toggle:disabled {
		opacity: 0.55;
		cursor: default;
	}
	.track {
		position: relative;
		flex: none;
		width: 34px;
		height: 19px;
		border-radius: 10px;
		background: var(--track);
	}
	.knob {
		position: absolute;
		top: 2px;
		left: 2px;
		width: 15px;
		height: 15px;
		border-radius: 50%;
		background: var(--muted);
		transition: left 0.15s;
	}
	[aria-checked='true'] .track {
		background: var(--accent);
	}
	[aria-checked='true'] .knob {
		left: 17px;
		background: var(--on-accent);
	}
	.toggle:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
		border-radius: var(--radius-small);
	}
</style>
