<script lang="ts">
	// What lies behind the whole UI: nothing, soft color shapes or the account's image.
	import type { Appearance } from '#lib/theme/themes.ts';

	type Props = {
		appearance: Appearance;
		/** Fill the positioned parent instead of the window (preview). */
		contained?: boolean;
		dashboard?: boolean;
		fit?: string;
		x?: number;
		y?: number;
		zoom?: number;
		fixed?: boolean;
	};
	let {
		appearance,
		contained = false,
		dashboard = false,
		fit = 'cover',
		x = 50,
		y = 50,
		zoom = 100,
		fixed = true,
	}: Props = $props();
</script>

{#if appearance.bg !== 'solid'}
	<div
		class="backdrop"
		class:contained
		class:dashboard
		class:scrolling={dashboard && !fixed}
		aria-hidden="true"
	>
		{#if appearance.bg === 'shapes'}
			<span class="shape one"></span>
			<span class="shape two"></span>
			<span class="shape three"></span>
		{:else if appearance.bgVersion > 0}
			<span
				class="image"
				style:background-size={fit}
				style:background-position={`${x}% ${y}%`}
				style:transform={fit === 'cover' ? `scale(${zoom / 100})` : undefined}
				style:background-image="url(/api/v1/background?v={appearance.bgVersion})"
			></span>
			<span class="dim" style:opacity={appearance.dim / 100}></span>
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
	.backdrop.dashboard {
		inset: 60px 0 34px;
	}
	.backdrop.scrolling {
		position: absolute;
		bottom: auto;
		height: calc(100svh - 94px);
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
		background-repeat: no-repeat;
	}
	.dim {
		background: var(--dim);
		opacity: var(--bg-dim);
	}
</style>
