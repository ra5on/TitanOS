<script lang="ts">
	import type { Snippet } from 'svelte';
	import { fly, fade } from 'svelte/transition';
	import { sheetUp, dim } from '#lib/agent-motion.ts';
	import { trapFocus } from '#lib/focus-trap.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { msg } from './labels.ts';
	let {
		title,
		onclose,
		calm = false,
		children,
	}: { title: string; onclose: () => void; calm?: boolean; children: Snippet } = $props();
</script>

<button
	class="pa-scrim"
	tabindex="-1"
	aria-label={msg('close')}
	onclick={onclose}
	transition:fade={dim(calm)}
></button>
<div
	class="pa-sheet"
	role="dialog"
	aria-modal="true"
	aria-label={title}
	tabindex="-1"
	use:trapFocus={{ onescape: onclose }}
	transition:fly={sheetUp(calm)}
>
	<header>
		<h2>{title}</h2>
		<button class="pa-icon" onclick={onclose} aria-label={msg('close')}
			><Icon name="close" /></button
		>
	</header>
	{@render children()}
</div>
