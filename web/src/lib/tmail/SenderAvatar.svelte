<script lang="ts">
	// 1.12 (owner 18 Nov 2026, "Absenderbild"): the sender's initials in a
	// colour of their own, or the picture of a contact with this address
	// (the contacts' own route, with its headers; never a picture from the
	// internet). A picture that does not load (no volume: 503) falls back
	// to the initials. Decorative: the row names the sender in words.
	import { photoURL } from '../pim/contacts-api.ts';
	import { avatarColor, avatarInitials } from './avatar.ts';
	import { avatars } from './store.svelte.ts';
	import type { Address } from './types.ts';

	let { from, size = 36 }: { from: Address | null | undefined; size?: number } = $props();

	const addr = $derived((from?.addr ?? '').trim().toLowerCase());
	const contact = $derived(
		addr && Object.hasOwn(avatars.photos, addr) ? avatars.photos[addr] : undefined,
	);
	const src = $derived(contact ? photoURL(contact) : '');
	let failed = $state('');
</script>

{#if src && failed !== src}
	<img
		class="av"
		style:--s="{size}px"
		{src}
		alt=""
		aria-hidden="true"
		width={size}
		height={size}
		loading="lazy"
		draggable="false"
		onerror={() => (failed = src)}
	/>
{:else}
	<span class="av letters" style:--s="{size}px" style:--c={avatarColor(addr)} aria-hidden="true"
		>{avatarInitials(from?.name ?? '', from?.addr ?? '')}</span
	>
{/if}

<style>
	.av {
		flex: none;
		display: inline-grid;
		place-items: center;
		width: var(--s);
		height: var(--s);
		border-radius: 50%;
		object-fit: cover;
		overflow: hidden;
		font-weight: 600;
		font-size: calc(var(--s) * 0.38);
		line-height: 1;
	}
	.letters {
		background: var(--c);
		color: var(--on-icon-square);
	}
</style>
