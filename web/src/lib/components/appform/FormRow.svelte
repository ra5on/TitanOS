<script lang="ts">
	// One setting of the app form. PLAN 78-5: a flat row like every setting
	// (styles: app.css, .srow) – the name and a grey hint below it on the left
	// (container path, variable, where the value comes from), the value on the
	// right; `wide` puts content that needs the row's width (a picker, a list,
	// a text area) below the name. Installing and editing an app share these
	// rows. Inside a dialog (`dialog`) it stays the line of a form: name left,
	// value right, hint below the value.
	import type { Snippet } from 'svelte';

	type Props = {
		label: string;
		/** The id of the field the name labels; without it the name is plain text. */
		id?: string;
		hint?: string;
		tone?: '' | 'ok' | 'warn' | 'error';
		/** Changed since the page was opened (edit form). */
		changed?: boolean;
		/** The value below the name, over the row's width. */
		wide?: boolean;
		/** A line of a form inside a dialog instead of a row. */
		dialog?: boolean;
		/** The value; a row without it only says something (a note, an error). */
		children?: Snippet;
		/** A hint with links or buttons instead of plain text. */
		more?: Snippet;
	};
	let {
		label,
		id,
		hint = '',
		tone = '',
		changed = false,
		wide = false,
		dialog = false,
		children,
		more,
	}: Props = $props();
</script>

{#if dialog}
	<div class="form-row" class:changed>
		{#if id}<label class="name" for={id}>{label}</label>{:else}<span class="name">{label}</span
			>{/if}
		<div class="value">
			{@render children?.()}
			{#if hint || more}<p class="hint {tone}" role={tone === 'error' ? 'alert' : undefined}>
					{hint}{@render more?.()}
				</p>{/if}
		</div>
	</div>
{:else}
	<div class="srow plain form-row" class:changed class:wide>
		<div class="srow-head">
			<div class="srow-text">
				{#if id}<label class="srow-title" for={id}>{label}</label>{:else}<span class="srow-title"
						>{label}</span
					>{/if}
				{#if hint || more}<p
						class="srow-desc hint {tone}"
						role={tone === 'error' ? 'alert' : undefined}
					>
						{hint}{@render more?.()}
					</p>{/if}
			</div>
			{#if children && !wide}<div class="srow-ctl value">{@render children()}</div>{/if}
		</div>
		{#if children && wide}<div class="srow-body">
				<div class="srow-block value">{@render children()}</div>
			</div>{/if}
	</div>
{/if}

<style>
	/* The line of a form inside a dialog. */
	.form-row:not(.srow) {
		display: grid;
		grid-template-columns: minmax(10rem, 17rem) minmax(0, 1fr);
		gap: var(--space-1) var(--space-4);
		align-items: start;
		padding: 7px var(--space-4);
	}
	.form-row:not(.srow):hover {
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
	.changed .name,
	.changed .srow-title {
		color: var(--accent);
	}
	.changed .name::before,
	.changed .srow-title::before {
		content: '';
		display: inline-block;
		width: 6px;
		height: 6px;
		margin-right: var(--space-2);
		border-radius: 50%;
		background: var(--accent);
		vertical-align: middle;
	}
	.form-row:not(.srow) .value {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
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
	/* The row: the fields of the form share one column at the right, so they
	   line up; switches and short fields stand at its right edge. */
	.srow-head {
		min-height: 52px;
		padding-block: var(--space-2);
	}
	.srow-ctl.value {
		flex: 0 0 min(34rem, 56%);
		flex-direction: column;
		flex-wrap: nowrap;
		align-items: flex-end;
		justify-content: center;
		gap: var(--space-1-5);
		max-width: none;
		color: var(--text);
	}
	.srow-ctl.value :global(.line) {
		align-self: stretch;
		justify-content: flex-end;
	}
	.srow-block.value {
		gap: var(--space-2);
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
	/* In a row the fields give way before a line of them wraps. */
	.srow .value :global(.line > .field) {
		flex-basis: 9rem;
		width: auto;
		min-width: 0;
	}
	.value :global(.line > .field.short) {
		flex: 0 1 8rem;
	}
	.value :global(.text) {
		max-width: 46rem;
		margin: 0;
		font-size: var(--text-md);
		line-height: 1.55;
		overflow-wrap: anywhere;
	}
	.form-row:not(.srow) .value :global(.text) {
		padding-top: 0.5rem;
	}
	.value :global(code) {
		font-family: var(--font-mono);
		color: color-mix(in srgb, var(--text) 75%, var(--muted));
	}
	@media (max-width: 760px) {
		.form-row:not(.srow) {
			grid-template-columns: minmax(0, 1fr);
			padding: var(--space-1-5) var(--space-3);
		}
		.name {
			padding-top: var(--space-1);
			text-align: left;
		}
	}
	/* Narrow: the value below the name, from the row's left edge. */
	@media (max-width: 639px) {
		.srow-ctl.value {
			flex: 1 1 100%;
			align-items: flex-start;
		}
		.srow-ctl.value :global(.line) {
			justify-content: flex-start;
		}
	}
</style>
