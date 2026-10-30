<script lang="ts">
	// Symbol and wordmark, see docs/brand. Frame and "OS" follow --accent, the letters --text.
	type Props = {
		/** Height of the symbol in px; the wordmark is scaled to match. */
		size?: number;
		wordmark?: boolean;
		stacked?: boolean;
	};

	let { size = 28, wordmark = true, stacked = false }: Props = $props();
</script>

<span class="logo" class:stacked style:gap="{size * (stacked ? 0.22 : 0.32)}px">
	<svg
		width={size}
		height={size}
		viewBox="0 0 28 28"
		aria-hidden={wordmark}
		role={wordmark ? undefined : 'img'}
		aria-label={wordmark ? undefined : 'TitanOS'}
	>
		<path
			class="accent-stroke"
			d="M14 2 25 8.5v11L14 26 3 19.5v-11Z"
			fill="none"
			stroke-width="2"
			stroke-linejoin="round"
		/>
		<path class="text" d="M8.6 9.3h10.8v2.3H8.6zM12.85 12.6h2.3v7.4h-2.3z" />
	</svg>
	{#if wordmark}
		<svg
			height={size * (stacked ? 0.44 : 0.71)}
			viewBox="0 0 576 100"
			role="img"
			aria-label="TitanOS"
		>
			<path
				class="text"
				d="M0 0h80v22H0zM29 30h22v70H29zM92 0h22v100H92zM126 0h80v22h-80zM155 30h22v70h-22zM218 100 252 0h16l34 100h-23l-19-58-19 58zM314 0h22v100h-22zM370 0h22v100h-22zM314 0h24l54 100h-24z"
			/>
			<path class="accent" fill-rule="evenodd" d="M404 0h80v100h-80zM426 22v56h36V22z" />
			<path class="accent" d="M496 0h80v22h-58v17h58v61h-80V78h58V61h-58z" />
		</svg>
	{/if}
</span>

<style>
	.logo {
		display: inline-flex;
		align-items: center;
	}
	.logo.stacked {
		flex-direction: column;
	}
	.text {
		fill: var(--text);
	}
	.accent {
		fill: var(--accent);
	}
	.accent-stroke {
		stroke: var(--accent);
	}
</style>
