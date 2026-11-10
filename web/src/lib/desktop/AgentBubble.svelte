<script lang="ts">
	// The helper bubble in the corner of the desktop: the agent in a small panel.
	import { untrack } from 'svelte';
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { wm } from './wm.svelte.ts';

	let { open }: { open: (key: string) => void } = $props();

	let bubble = $state(false);
	// The helper bubble knows the window in front: its question starts with it (the agent's "ask").
	let bubbleSrc = $state('/agent');
	$effect(() => {
		if (!bubble) return;
		const front = untrack(() => wm.windows.find((w) => w.id === wm.focused && !w.minimized));
		bubbleSrc = front
			? '/agent?ask=' +
				encodeURIComponent(
					t('desktop.agent.context', {
						name: front.title,
						page: front.app.startsWith('docker:') ? front.title : front.href,
					}),
				)
			: '/agent';
	});
</script>

<button
	type="button"
	class="bubble"
	class:on={bubble}
	onclick={() => (bubble = !bubble)}
	aria-label={t('desktop.agent.bubble')}
	title={t('desktop.agent.bubble')}
	aria-expanded={bubble}
>
	<Icon name="sparkle" size={26} />
</button>
{#if bubble}
	<div class="bubble-pop glass-panel" role="dialog" aria-label={t('desktop.app.agent')}>
		<div class="bubble-head">
			<DesktopIcon glyph="agent" gradient="violet" size={24} />
			<b>{t('desktop.app.agent')}</b>
			<span class="space"></span>
			<button
				type="button"
				class="link"
				onclick={() => {
					bubble = false;
					open('app:agent');
				}}>{t('desktop.agent.openWindow')}</button
			>
			<button
				type="button"
				class="pb"
				onclick={() => (bubble = false)}
				aria-label={t('desktop.window.close')}><Icon name="close" size={15} /></button
			>
		</div>
		<iframe src={bubbleSrc} title={t('desktop.app.agent')}></iframe>
	</div>
{/if}

<style>
	.bubble:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.bubble {
		position: absolute;
		right: 22px;
		bottom: 22px;
		width: 58px;
		height: 58px;
		border: 0;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: linear-gradient(160deg, #a78bfa, #6d3fe0);
		color: #fff;
		box-shadow:
			0 10px 30px rgb(109 63 224 / 0.55),
			0 0 0 6px color-mix(in srgb, #a78bfa 22%, transparent);
		cursor: pointer;
		z-index: 9300;
	}
	.bubble-pop {
		position: absolute;
		right: 22px;
		bottom: 92px;
		width: min(440px, calc(100% - 44px));
		height: min(620px, calc(100% - 110px));
		border-radius: 20px;
		display: flex;
		flex-direction: column;
		overflow: hidden;
		z-index: 9300;
		animation: pop 0.18s ease-out;
	}
	@keyframes pop {
		from {
			opacity: 0;
			transform: translateY(10px) scale(0.97);
		}
	}
	.bubble-head {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		padding: var(--space-2) var(--space-2) var(--space-2) 14px;
		border-bottom: 1px solid var(--border);
	}
	.bubble-pop iframe {
		flex: 1;
		border: 0;
		width: 100%;
	}
	@media (prefers-reduced-motion: reduce) {
		.bubble-pop {
			animation: none;
		}
	}
	.pb {
		width: var(--pb);
		height: var(--pb);
		flex: none;
		display: grid;
		place-items: center;
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	.pb:hover {
		background: color-mix(in srgb, var(--text) 9%, transparent);
	}
	.pb:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.space {
		flex: 1;
	}
	.link {
		border: 0;
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
	}
</style>
