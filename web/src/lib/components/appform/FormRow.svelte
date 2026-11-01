<script lang="ts">
	// One line of the app form: the name on the left, the value on the right
	// and a grey hint below it (container path, variable, where the value
	// comes from). Installing and editing an app share these lines.
	import type { Snippet } from 'svelte';

	type Props = {
		label: string;
		/** The id of the field the name labels; without it the name is plain text. */
		id?: string;
		hint?: string;
		tone?: '' | 'ok' | 'warn' | 'error';
		/** Changed since the page was opened (edit form). */
		changed?: boolean;
		children: Snippet;
		/** A hint with links or buttons instead of plain text. */
		more?: Snippet;
	};
	let { label, id, hint = '', tone = '', changed = false, children, more }: Props = $props();
</script>

<div class="form-row" class:changed>
	{#if id}<label class="name" for={id}>{label}</label>{:else}<span class="name">{label}</span>{/if}
	<div class="value">
		{@render children()}
		{#if hint || more}<p class="hint {tone}" role={tone === 'error' ? 'alert' : undefined}>
				{hint}{@render more?.()}
			</p>{/if}
	</div>
</div>

<style>
	.form-row {
		display: grid;
		grid-template-columns: minmax(10rem, 17rem) minmax(0, 1fr);
		gap: 4px var(--space-4);
		align-items: start;
		padding: 7px var(--space-4);
	}
	.form-row:hover {
		background: color-mix(in srgb, var(--text) 2.5%, transparent);
	}
	.name {
		padding-top: 0.5rem;
		font-size: var(--text-md);
		font-weight: 500;
		text-align: right;
		overflow-wrap: anywhere;
	}
	.name::after {
		content: ':';
	}
	.changed .name {
		color: var(--accent);
	}
	.changed .name::before {
		content: '';
		display: inline-block;
		width: 6px;
		height: 6px;
		margin-right: 8px;
		border-radius: 50%;
		background: var(--accent);
		vertical-align: middle;
	}
	.value {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
	}
	.hint {
		margin: 0;
		color: var(--muted);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.hint.ok {
		color: var(--ok-text);
	}
	.hint.warn {
		color: var(--warn-text);
	}
	.hint.error {
		color: var(--error-text);
	}
	.value :global(.line) {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.value :global(.line > .field) {
		flex: 1 1 15rem;
		max-width: 34rem;
	}
	.value :global(.line > .field.short) {
		flex: 0 1 8rem;
	}
	.value :global(.text) {
		max-width: 46rem;
		margin: 0;
		padding-top: 0.5rem;
		font-size: var(--text-md);
		line-height: 1.55;
	}
	.value :global(code) {
		font-family: var(--font-mono);
		color: color-mix(in srgb, var(--text) 75%, var(--muted));
	}
	@media (max-width: 760px) {
		.form-row {
			grid-template-columns: minmax(0, 1fr);
			padding: 6px var(--space-3);
		}
		.name {
			padding-top: 4px;
			text-align: left;
		}
	}
</style>
