<script lang="ts">
	// 1.11 (step 16): the picture of a contact (GET …/photo) or its
	// initials. A picture titand cannot show (HEIC from a device, gone
	// meanwhile) falls back to the initials.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { initialsOf } from '../contacts.ts';
	import { photoURL } from '../contacts-api.ts';

	type Props = {
		id: string;
		name: string;
		initials?: string;
		/** The company: a contact named after it gets one letter. */
		org?: string;
		hasPhoto: boolean;
		/** Loads the picture anew after a change (the card's etag). */
		version?: string;
		/** A picture chosen in the form, not saved yet. */
		preview?: string;
		size: number;
	};
	let {
		id,
		name,
		initials = '',
		org = '',
		hasPhoto,
		version = '',
		preview = '',
		size,
	}: Props = $props();

	let failed = $state('');
	const src = $derived(preview || (hasPhoto && id ? photoURL(id, version) : ''));
	const letters = $derived(initials || initialsOf(name, org));
</script>

{#if src && failed !== src}
	<img
		class="av"
		style:--s="{size}px"
		{src}
		alt={t('contacts.photoOf', { name: name || t('contacts.unnamed') })}
		width={size}
		height={size}
		loading="lazy"
		onerror={() => (failed = src)}
	/>
{:else}
	<span class="av" style:--s="{size}px" aria-hidden="true">{letters}</span>
{/if}

<style>
	.av {
		flex: none;
		display: inline-grid;
		place-items: center;
		width: var(--s);
		height: var(--s);
		border-radius: 50%;
		background: var(--chip);
		color: var(--text);
		font-weight: 600;
		font-size: calc(var(--s) * 0.36);
		line-height: 1;
		overflow: hidden;
		object-fit: cover;
	}
</style>
