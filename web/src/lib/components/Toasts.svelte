<script lang="ts">
	// The short notices of lib/toast.svelte.ts at the bottom of the screen.
	// Both live regions are always there, empty when nothing is shown: a
	// region that appears together with its text is often not read out.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { dismissToast, holdToast, releaseToast, toasts, type Toast } from '#lib/toast.svelte.ts';

	const info = $derived(toasts.list.filter((x) => x.tone === 'info'));
	const errors = $derived(toasts.list.filter((x) => x.tone === 'error'));
</script>

{#snippet item(toast: Toast)}
	<div
		class="toast"
		class:error={toast.tone === 'error'}
		data-toast={toast.tone}
		role="group"
		onpointerenter={() => holdToast(toast.id)}
		onpointerleave={() => releaseToast(toast.id)}
		onfocusin={() => holdToast(toast.id)}
		onfocusout={() => releaseToast(toast.id)}
	>
		<!-- The sign says what kind of notice it is, not the colour alone. -->
		<span class="sign" aria-hidden="true">{toast.tone === 'error' ? '!' : 'i'}</span>
		<span class="text">{toast.text}</span>
		<button
			type="button"
			class="close"
			aria-label={t('toast.close')}
			title={t('toast.close')}
			onclick={() => dismissToast(toast.id)}>✕</button
		>
	</div>
{/snippet}

<div class="toasts">
	<div class="stack" role="status">
		{#each info as toast (toast.id)}{@render item(toast)}{/each}
	</div>
	<div class="stack" role="alert">
		{#each errors as toast (toast.id)}{@render item(toast)}{/each}
	</div>
</div>

<style>
	.toasts {
		position: fixed;
		left: 50%;
		/* Above the dock of the desktop and the phone, above the home bar. */
		bottom: calc(env(safe-area-inset-bottom, 0px) + 92px);
		transform: translateX(-50%);
		/* Windows count their z up from 1; dialogs and menus use the top layer. */
		z-index: 100000;
		width: min(34rem, calc(100vw - 32px));
		display: flex;
		flex-direction: column;
		pointer-events: none;
	}
	.stack {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.stack:not(:empty) ~ .stack:not(:empty) {
		margin-top: var(--space-2);
	}
	.toast {
		pointer-events: auto;
		display: flex;
		align-items: flex-start;
		gap: var(--space-2-5);
		padding: var(--space-2-5) var(--space-2) var(--space-2-5) 14px;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		box-shadow: var(--shadow-menu);
		color: var(--text);
		font-size: var(--text-md);
		overflow-wrap: anywhere;
	}
	.toast.error {
		background: var(--error-bg);
		border-color: var(--error-border);
		color: var(--error-text);
	}
	.sign {
		flex: none;
		display: inline-grid;
		place-items: center;
		width: 1.4em;
		height: 1.4em;
		margin-top: 0.1em;
		border-radius: 50%;
		border: 1.5px solid currentColor;
		font-weight: 700;
		font-size: var(--text-sm);
		line-height: 1;
	}
	.text {
		flex: 1;
		min-width: 0;
		align-self: center;
	}
	.close {
		flex: none;
		min-width: 2rem;
		min-height: 2rem;
		margin: -0.25rem 0;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: inherit;
		font: inherit;
		cursor: pointer;
	}
	.close:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	/* Every target on the phone and by touch is at least 44 px. */
	@media (max-width: 760px), (pointer: coarse) {
		.close {
			min-width: 44px;
			min-height: 44px;
			margin: -0.5rem 0;
		}
	}
	@media (prefers-reduced-motion: no-preference) {
		.toast {
			animation: rise 160ms ease-out;
		}
	}
	@keyframes rise {
		from {
			opacity: 0;
			transform: translateY(8px);
		}
	}
</style>
