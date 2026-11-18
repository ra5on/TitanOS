<script lang="ts">
	// 1.11 (step 16): a learned address of T-Mail with "Als Kontakt
	// übernehmen" (draft 13). Not a contact: it never goes to the iPhone.
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { markParts, type Suggestion } from '../contacts.ts';
	import { sentText } from '../contacts-text.ts';

	type Props = {
		suggestion: Suggestion;
		query?: string;
		busy?: boolean;
		phone?: boolean;
		ontake: (s: Suggestion) => void;
	};
	let { suggestion, query = '', busy = false, phone = false, ontake }: Props = $props();
</script>

{#snippet hl(text: string)}{#each markParts(text, query) as [part, hit], i (i)}{#if hit}<mark
				>{part}</mark
			>{:else}{part}{/if}{/each}{/snippet}

<div class="srow" class:phone data-suggestion={suggestion.addr}>
	<span class="av" aria-hidden="true"><Icon name="mail" size={16} /></span>
	<span class="ct"
		><b>{@render hl(suggestion.name || suggestion.addr)}</b><small
			>{#if suggestion.name}{@render hl(suggestion.addr)}{' · '}{/if}{sentText(
				suggestion.count,
			)}</small
		></span
	>
	<button type="button" class="btn" disabled={busy} onclick={() => ontake(suggestion)}
		>{t('contacts.sug.take')}</button
	>
</div>

<style>
	.srow {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-1-5) var(--space-2);
		border-radius: var(--radius-ctl);
		background: var(--srow-bg);
	}
	.av {
		flex: none;
		display: inline-grid;
		place-items: center;
		width: 36px;
		height: 36px;
		border-radius: 50%;
		background: var(--chip);
		color: var(--text);
	}
	.ct {
		flex: 1 1 10rem;
		min-width: 0;
		display: flex;
		flex-direction: column;
		overflow-wrap: anywhere;
	}
	.ct b {
		font-weight: 600;
	}
	.ct small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.phone .btn {
		min-height: 44px;
	}
	mark {
		background: color-mix(in srgb, var(--accent) 28%, transparent);
		color: inherit;
		border-radius: 2px;
	}
</style>
