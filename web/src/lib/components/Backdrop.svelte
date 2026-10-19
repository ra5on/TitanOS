<script lang="ts">
	// What lies behind the whole UI: nothing, soft color shapes or the account's image.
	import type { Appearance } from '#lib/theme/themes.ts';

	type Props = {
		appearance: Appearance;
		/** Fill the positioned parent instead of the window (preview). */
		contained?: boolean;
	};
	let { appearance, contained = false }: Props = $props();
</script>

{#if appearance.bg !== 'solid'}
	<div class="backdrop" class:contained aria-hidden="true">
		{#if appearance.bg === 'shapes'}
			<span class="shape one"></span>
			<span class="shape two"></span>
			<span class="shape three"></span>
		{:else if appearance.bgVersion > 0}
			<span class="image" style:background-image="url(/api/v1/background?v={appearance.bgVersion})"></span>
			<span class="dim"></span>
		{/if}
	</div>
{/if}

<style>
	.backdrop {
		position: fixed;
		inset: 0;
		z-index: 0;
		overflow: hidden;
		pointer-events: none;
	}
	.backdrop.contained {
		position: absolute;
	}
	.shape {
		position: absolute;
		border-radius: 50%;
		filter: blur(60px);
	}
	.one {
		left: -6%;
		top: 8%;
		width: 34vmax;
		height: 34vmax;
		max-width: 520px;
		max-height: 520px;
		background: var(--accent);
		opacity: 0.5;
	}
	.two {
		right: -4%;
		top: 36%;
		width: 30vmax;
		height: 30vmax;
		max-width: 460px;
		max-height: 460px;
		background: var(--shape-2);
		opacity: 0.42;
	}
	.three {
		left: 30%;
		bottom: -12%;
		width: 36vmax;
		height: 26vmax;
		max-width: 560px;
		max-height: 400px;
		background: var(--shape-3);
		opacity: 0.42;
	}
	.image,
	.dim {
		position: absolute;
		inset: 0;
	}
	.image {
		background-size: cover;
		background-position: center;
	}
	.dim {
		background: var(--dim);
		opacity: var(--bg-dim);
	}
</style>
