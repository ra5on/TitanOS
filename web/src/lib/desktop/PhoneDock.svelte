<script lang="ts">
	import { goto, afterNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { tick } from 'svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import PhoneCustomize from './PhoneCustomize.svelte';
	import PhoneDockHandle from './PhoneDockHandle.svelte';
	import { PHONE_DOCK_DEFAULT, phoneDockSize } from './phone-home.ts';
	import { phoneHold } from './phone-hold.ts';
	import { openPhoneProgram } from './phone-open.ts';
	import { phoneChrome } from './immersive-phone.svelte.ts';
	import { programs } from './programs.svelte.ts';
	import { desktop } from './wm.svelte.ts';
	import { watchSources } from './sources.svelte.ts';

	let { home, full = false }: { home: boolean; full?: boolean } = $props();
	let dialog: HTMLDialogElement;
	let homeButton: HTMLButtonElement;
	let scroll = $state<HTMLDivElement>();
	let current = $state(0);
	let count = $state(1);
	const pages = $derived(home && page.url.searchParams.get('phone') === 'pages');
	const resting = $derived(!full && !phoneChrome.immersive && !pages);
	function measure() {
		if (!scroll || !scroll.clientWidth) return;
		const range = scroll.scrollWidth - scroll.clientWidth;
		count = range < 2 ? 1 : Math.ceil((range - 1) / scroll.clientWidth) + 1;
		current = count === 1 ? 0 : Math.round((scroll.scrollLeft / range) * (count - 1));
	}
	$effect(() => {
		if (!scroll) return;
		void pins.length;
		void size;
		const observer = new ResizeObserver(measure);
		observer.observe(scroll);
		void tick().then(measure);
		return () => observer.disconnect();
	});
	function toPage(n: number) {
		if (scroll)
			scroll.scrollTo({
				left: ((scroll.scrollWidth - scroll.clientWidth) * n) / Math.max(1, count - 1),
				behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth',
			});
	}
	function reserve(node: HTMLElement) {
		const root = document.documentElement;
		const observer = new ResizeObserver(() =>
			root.style.setProperty('--phone-dock-height', node.offsetHeight + 'px'),
		);
		observer.observe(node);
		return {
			destroy() {
				observer.disconnect();
				root.style.removeProperty('--phone-dock-height');
			},
		};
	}
	const dock = $derived(desktop.prefs.phone.dock);
	const byKey = $derived(new Map(programs().map((p) => [p.key, p])));
	const pins = $derived(
		(dock.custom ? dock.apps : PHONE_DOCK_DEFAULT).flatMap((key) => {
			const p = byKey.get(key);
			return p ? [p] : [];
		}),
	);
	const size = $derived({ small: 33, medium: 44, large: 55 }[phoneDockSize(dock.size)]);
	$effect(() => {
		if (phoneChrome.dock && dock.show) {
			dialog?.showModal();
			void tick().then(() => homeButton?.focus({ preventScroll: true }));
		} else dialog?.close();
	});
	// Outside the home screen only the open dock/editor needs Docker and web-app icons.
	$effect(() => {
		if (
			!home &&
			(phoneChrome.dock ||
				phoneChrome.customize ||
				(dock.show && dock.apps.some((k) => !k.startsWith('app:'))))
		)
			return watchSources();
	});
	afterNavigate(() => {
		phoneChrome.dock = false;
		phoneChrome.customize = false;
	});
	function goHome() {
		phoneChrome.dock = false;
		void goto('/');
	}
	function edit() {
		phoneChrome.customizeDock = true;
		phoneChrome.customize = true;
	}
</script>

{#snippet tray(inDialog: boolean)}
	<nav
		class="tray"
		style:--icon="{size}px"
		aria-label={t('phone.nord.dock')}
		data-phone-dock
		data-no-edge-swipe
		use:phoneHold={edit}
	>
		<button
			type="button"
			class="home-button"
			bind:this={homeButton}
			onclick={goHome}
			aria-label={t('desktop.phone.home')}
		>
			<span class="home-icon"><Icon name="home" size={size * 0.54} /></span><span
				>{t('phone.nord.home')}</span
			>
		</button>
		<div class="scrolling" bind:this={scroll} onscroll={measure}>
			{#each pins as app (app.key)}
				<button
					type="button"
					data-dock-key={app.key}
					aria-label={app.label}
					onclick={(e) => {
						phoneChrome.dock = false;
						void openPhoneProgram(app, e.currentTarget);
					}}
				>
					<DesktopIcon
						glyph={app.glyph}
						gradient={app.gradient}
						image={app.image}
						name=""
						{size}
					/><span>{app.key === 'app:settings' ? t('phone.nord.system') : app.label}</span>
				</button>
			{/each}
		</div>
		{#if count > 1}<div class="indicators" aria-label={t('phone.nord.dockPages')}>
				{#each Array(count) as _, i (i)}
					<button
						type="button"
						aria-label={t('phone.page', { n: i + 1, of: count })}
						aria-current={i === current ? 'page' : undefined}
						onclick={() => toPage(i)}><span></span></button
					>
				{/each}
			</div>{/if}
		{#if inDialog}<button
				type="button"
				class="hide"
				onclick={() => (phoneChrome.dock = false)}
				aria-label={t('common.close')}><Icon name="chevronDown" size={16} /></button
			>{/if}
	</nav>
{/snippet}
{#if dock.show}
	{#if resting}
		<div class="resting" use:reserve>{@render tray(false)}</div>
	{:else if !phoneChrome.immersive && !pages}
		<div class="handle"><PhoneDockHandle /></div>
	{/if}
{/if}
<!-- A second native modal can open above an NVR player; keyboard focus and Escape belong to the dock until it closes. -->
<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<dialog
	class="overlay"
	bind:this={dialog}
	aria-label={t('phone.nord.dock')}
	onclose={() => (phoneChrome.dock = false)}
	onclick={(e) => {
		if (e.target === dialog) phoneChrome.dock = false;
	}}
>
	{#if phoneChrome.dock}{@render tray(true)}{/if}
</dialog>
{#if phoneChrome.customize}<PhoneCustomize />{/if}

<style>
	:global(body:has(.phone-bar [aria-haspopup='menu'][aria-expanded='true'])) .resting,
	:global(body:has(dialog[open])) .resting,
	:global(body:has(.phone-bar [aria-haspopup='menu'][aria-expanded='true'])) .handle,
	:global(body:has(dialog[open])) .handle {
		visibility: hidden;
	}

	.tray {
		position: relative;
	}
	.indicators {
		position: absolute;
		bottom: 100%;
		left: 50%;
		transform: translateX(-50%);
		display: flex;
		align-items: center;
		justify-content: center;
		max-width: 95%;
		overflow-x: auto;
		scrollbar-width: none;
	}
	.tray .indicators button {
		display: grid;
		place-items: center;
		width: 16px;
		min-width: 16px;
		min-height: 30px;
		height: 30px;
		padding: 0;
		background: none;
	}
	.tray .indicators button > span:last-child {
		width: 5px;
		height: 5px;
		border-radius: 7px;
		background: #cadadc;
		box-shadow: 0 0 2px #000;
	}
	.tray .indicators button[aria-current='page'] > span:last-child {
		width: 11px;
		background: #fff;
	}

	.resting,
	.overlay,
	.handle {
		position: fixed;
		margin: 0 auto;
		bottom: calc(10px + env(safe-area-inset-bottom));
		top: auto;
		color: white;
	}
	.resting,
	.overlay {
		left: max(12px, env(safe-area-inset-left));
		right: max(12px, env(safe-area-inset-right));
		max-width: 480px;
		z-index: 22;
	}
	.overlay {
		padding: 0;
		border: 0;
		background: none;
		width: calc(
			100% - max(12px, env(safe-area-inset-left)) - max(12px, env(safe-area-inset-right))
		);
		overflow: visible;
	}
	.overlay::backdrop {
		background: rgb(1 10 18 / 0.2);
	}
	.tray {
		display: flex;
		align-items: center;
		gap: 4px;
		padding: 7px 9px;
		border: 1px solid rgb(220 247 255 / 0.28);
		border-radius: clamp(23px, 6vw, 32px);
		background: var(--nord-chrome);
		-webkit-backdrop-filter: var(--nord-blur);
		backdrop-filter: var(--nord-blur);
		box-shadow:
			inset 0 1px 1px rgb(255 255 255 / 0.2),
			0 7px 24px rgb(1 9 17 / 0.38);
		-webkit-user-select: none;
		user-select: none;
		-webkit-touch-callout: none;
	}
	.tray button {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 3px;
		min-width: 44px;
		min-height: 44px;
		color: inherit;
		background: none;
		border: 0;
		cursor: pointer;
		font: inherit;
		padding: 2px;
	}
	.tray button > span:last-child {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		width: 100%;
		text-align: center;
		font-size: 0.74rem;
		font-weight: 600;
		line-height: 1.2;
	}
	.home-button {
		width: calc(var(--icon) + 20px);
		flex: none;
	}
	.home-icon {
		width: var(--icon);
		height: var(--icon);
		border-radius: 26%;
		display: grid;
		place-items: center;
		color: #eaffff;
		background: linear-gradient(150deg, #3a716d, #203f49);
		box-shadow: inset 0 1px 1px #ffffff31;
	}
	.scrolling {
		display: flex;
		min-width: 0;
		flex: 1;
		overflow-x: auto;
		border-inline-start: 1px solid rgb(220 246 255 / 0.2);
		padding-inline: 4px;
		scroll-snap-type: x proximity;
		scrollbar-width: none;
		overscroll-behavior-x: contain;
		touch-action: pan-x;
	}
	.scrolling::-webkit-scrollbar {
		display: none;
	}
	.scrolling button {
		flex: 0 0 max(calc(var(--icon) + 12px), 32%);
		min-width: 0;
		scroll-snap-align: start;
	}
	.tray .hide {
		min-width: 32px;
	}
	.handle {
		z-index: 22;
		left: auto;
		right: max(12px, env(safe-area-inset-right));
		border-radius: 18px;
		background: rgb(20 45 55 / 0.88);
		border: 1px solid #ffffff34;
	}
	@media (prefers-reduced-transparency: reduce) {
		.tray,
		.handle {
			background: #1a3038;
			backdrop-filter: none;
		}
	}
</style>
