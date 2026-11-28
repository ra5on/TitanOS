<script lang="ts">
	// 1.40 (§125, owner 28.11.2026: "Das Menü vom Agent können wir noch mehr
	// in iOS-Design gestalten"): on the phone the menu behind ☰ comes in from
	// the left like the side panel of an iPhone app – round buttons at the
	// top, a large title, a search field and rounded groups with inset lines.
	import type { Snippet } from 'svelte';
	import { fly, fade } from 'svelte/transition';
	import { cubicOut } from 'svelte/easing';
	import { dim } from '#lib/agent-motion.ts';
	import { trapFocus } from '#lib/focus-trap.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { msg } from './labels.ts';
	let {
		title,
		onclose,
		onnew,
		calm = false,
		children,
	}: {
		title: string;
		onclose: () => void;
		onnew: () => void;
		calm?: boolean;
		children: Snippet;
	} = $props();
</script>

<button
	class="pa-scrim pa-drawer-scrim"
	tabindex="-1"
	aria-label={msg('close')}
	onclick={onclose}
	transition:fade={dim(calm)}
></button>
<div
	class="pa-drawer"
	role="dialog"
	aria-modal="true"
	aria-label={title}
	tabindex="-1"
	use:trapFocus={{ onescape: onclose }}
	transition:fly={{ x: -340, opacity: 1, duration: calm ? 0 : 320, easing: cubicOut }}
>
	<header class="pa-drawer-top">
		<button class="pa-round" onclick={onclose} aria-label={msg('close')}
			><Icon name="close" /></button
		>
		<button class="pa-round" onclick={onnew} aria-label={msg('newChat')}
			><Icon name="edit" /></button
		>
	</header>
	<h2 class="pa-drawer-title">{title}</h2>
	<div class="pa-drawer-body">
		{@render children()}
	</div>
</div>
