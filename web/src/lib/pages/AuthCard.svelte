<script lang="ts">
	// Frame of the pages shown before anyone is logged in or when the NAS
	// cannot be used (first setup, "not reachable"). PLAN 67: in the style of
	// the login page – the desktop's wallpaper (or the login page's picture)
	// and a glass card – so no way to the login ever shows the old page.
	import type { Snippet } from 'svelte';
	import { loginWallpaper } from '#lib/login/wallpaper.ts';
	import { wallpaperCss } from '#lib/desktop/wallpapers.ts';
	import { resolvePalette } from '#lib/theme/themes.ts';
	import Logo from '#lib/components/Logo.svelte';
	import LanguageSwitch from '#lib/components/LanguageSwitch.svelte';
	import { i18n } from '#lib/i18n/index.svelte.ts';
	import {
		loginAppearance,
		loginBackgroundURL,
		loginTexts,
		type LoginLook,
	} from '#lib/login/look.ts';
	import { loginLook } from '#lib/login/state.svelte.ts';
	import { appearance } from '#lib/theme/appearance.svelte.ts';
	import { cssVariables, dataAttributes } from '#lib/theme/themes.ts';

	type Props = {
		title: string;
		hostname?: string;
		children: Snippet;
		/** The look to show; the NAS's look when left out. */
		look?: LoginLook | null;
		/** Use the configured title and notice (the login page). */
		texts?: boolean;
		/** Drawn inside its parent instead of the window (editor preview). */
		preview?: boolean;
		/** URL of a picked image that is not saved yet. */
		imageURL?: string;
	};
	let {
		title,
		hostname = '',
		children,
		look,
		texts = false,
		preview = false,
		imageURL = '',
	}: Props = $props();

	const current = $derived(look === undefined ? loginLook.value : look);
	const resolved = $derived(loginAppearance(current, appearance.current));
	// The editor's preview always draws its own background.
	const effective = $derived({ ...resolved, scoped: resolved.scoped || preview });
	const style = $derived(
		effective.scoped
			? Object.entries(cssVariables(effective.appearance, appearance.systemLight))
					.map(([name, value]) => `${name}: ${value}`)
					.join('; ')
			: undefined,
	);
	const attributes = $derived(
		effective.scoped ? dataAttributes(effective.appearance, appearance.systemLight) : {},
	);
	const picture = $derived(loginBackgroundURL(current, imageURL));
	const dark = $derived(resolvePalette(effective.appearance, appearance.systemLight).dark);
	const wallpaper = $derived(`${wallpaperCss(loginWallpaper(), !dark)} center / cover`);
	const words = $derived(texts ? loginTexts(current, i18n.locale, title) : { title, notice: '' });
	const position = $derived(current?.position ?? 'center');

	let now = $state(new Date());
	$effect(() => {
		if (!current?.clock) return;
		now = new Date();
		const timer = setInterval(() => (now = new Date()), 1000);
		return () => clearInterval(timer);
	});
	const time = $derived(
		new Intl.DateTimeFormat(i18n.locale, { hour: '2-digit', minute: '2-digit' }).format(now),
	);
	const date = $derived(
		new Intl.DateTimeFormat(i18n.locale, { weekday: 'long', day: 'numeric', month: 'long' }).format(
			now,
		),
	);
</script>

<main
	class="auth"
	class:scoped={effective.scoped}
	class:preview
	class:left={position === 'left'}
	class:right={position === 'right'}
	{style}
	{...attributes}
>
	{#if effective.scoped && picture}
		<div class="login-background" aria-hidden="true">
			<div
				class="picture"
				style:background-image={`url("${picture}")`}
				style:background-size={current!.background.fit}
				style:background-position={`${current!.background.x}% ${current!.background.y}%`}
				style:transform={current!.background.fit === 'cover'
					? `scale(${current!.background.zoom / 100})`
					: undefined}
			></div>
			<div class="dim" style:opacity={current!.background.dim / 100}></div>
		</div>
	{:else}
		<div class="login-background" aria-hidden="true">
			<div class="picture wallpaper" style:background={wallpaper}></div>
		</div>
	{/if}
	<section class="card" class:wide={current?.width === 'wide'}>
		{#if current?.clock}
			<p class="clock"><time class="mono">{time}</time><span class="muted">{date}</span></p>
		{/if}
		{#if !current?.hideLogo}<Logo size={52} stacked />{/if}
		<div class="head">
			<h1>{words.title}</h1>
			{#if hostname && !current?.hideHost}<span class="mono host">{hostname}</span>{/if}
		</div>
		{#if words.notice}<p class="welcome">{words.notice}</p>{/if}
		{@render children()}
	</section>
	<LanguageSwitch />
</main>

<style>
	main {
		position: relative;
		z-index: 1;
		min-height: 100vh;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 20px;
		padding: var(--space-5);
	}
	main {
		color: var(--text);
		isolation: isolate;
		overflow: hidden;
	}
	.login-background {
		background: #070d1c;
	}
	main.preview {
		min-height: 100%;
		height: 100%;
	}
	main.left {
		align-items: flex-start;
		padding-left: max(var(--space-5), 8vw);
	}
	main.right {
		align-items: flex-end;
		padding-right: max(var(--space-5), 8vw);
	}
	main.preview.left {
		padding-left: 8%;
	}
	main.preview.right {
		padding-right: 8%;
	}
	/* The language switch stays under the card. */
	main.left :global(.switch),
	main.right :global(.switch) {
		align-self: auto;
	}
	.login-background {
		position: fixed;
		inset: 0;
		z-index: -1;
		overflow: hidden;
		pointer-events: none;
	}
	main.preview .login-background,
	main.preview :global(.backdrop) {
		position: absolute;
	}
	main.scoped :global(.backdrop) {
		z-index: -1;
	}
	.picture,
	.dim {
		position: absolute;
		inset: 0;
	}
	.picture {
		background-repeat: no-repeat;
		transform-origin: center;
	}
	.dim {
		background: var(--dim);
	}
	.wallpaper {
		filter: var(--wallpaper-filter, none);
	}
	.card {
		color: var(--text);
		border-radius: calc(var(--radius-tile) + 4px);
		background: var(--desk-surface, color-mix(in srgb, var(--surface) 72%, transparent));
		-webkit-backdrop-filter: blur(var(--desk-blur, 22px)) saturate(1.4);
		backdrop-filter: blur(var(--desk-blur, 22px)) saturate(1.4);
		border: 1px solid color-mix(in srgb, #fff 14%, transparent);
		box-shadow: 0 20px 50px rgb(0 0 0 / 0.35);
		width: 100%;
		max-width: 440px;
		display: flex;
		flex-direction: column;
		align-items: stretch;
		gap: 22px;
		padding: 36px 32px 32px;
	}
	.card.wide {
		max-width: 560px;
	}
	.card :global(.logo) {
		align-self: center;
	}
	.head {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-1-5);
		text-align: center;
	}
	h1 {
		font-size: var(--text-xl);
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.host {
		padding: 3px var(--space-2);
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.welcome {
		font-size: var(--text-md);
		text-align: center;
		overflow-wrap: anywhere;
	}
	.clock {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 2px;
		margin: 0;
	}
	.clock time {
		font-size: 2rem;
		font-weight: 500;
		line-height: 1.1;
	}
	.clock span {
		font-size: var(--text-base);
	}
	/* Phones: less frame around the card, more room for the fields. */
	@media (max-width: 480px) {
		main {
			padding: var(--space-4);
		}
		.card {
			padding: 28px 20px var(--space-5);
		}
	}
	@media (max-width: 760px) {
		main.left,
		main.right {
			align-items: center;
			padding: var(--space-5) var(--space-4);
		}
	}
</style>
