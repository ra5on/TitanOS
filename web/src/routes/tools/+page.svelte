<script lang="ts">
	import { apiPath } from '#lib/cluster-node.ts';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import DiskTests from './DiskTests.svelte';

	const tools: { href: string; label: MessageKey; icon: IconName }[] = [
		{ href: '/tools/tasks', label: 'ops.tasks', icon: 'list' },
		{ href: '/tools/maintenance', label: 'ops.maintenance', icon: 'tool' },
		{ href: '/tools/monitoring', label: 'ops.monitoring', icon: 'activity' },
		{ href: '/tools/activity', label: 'ops.activity', icon: 'clock' },
		{ href: '/tools/logs', label: 'tools.logs', icon: 'fileText' },
		{ href: '/tools/hardware', label: 'hwcheck.title', icon: 'chip' },
	];

	// Saved links to the journal pointed at a section of this page.
	onMount(() => {
		if (location.hash === '#tools-logs')
			void goto(`/tools/logs${page.url.search}`, { replaceState: true });
	});

	let packing = $state(false);
	let packError = $state('');
	// The bundle is made while it is being downloaded; the browser saves it.
	async function pack() {
		packing = true;
		packError = '';
		try {
			const res = await fetch(apiPath('/diagnostics/bundle'), { credentials: 'same-origin' });
			if (!res.ok) throw new Error(String(res.status));
			const blob = await res.blob();
			const name =
				/filename="([^"]+)"/.exec(res.headers.get('Content-Disposition') ?? '')?.[1] ??
				'titanos-diagnose.zip';
			const link = document.createElement('a');
			link.href = URL.createObjectURL(blob);
			link.download = name;
			link.click();
			setTimeout(() => URL.revokeObjectURL(link.href), 10_000);
		} catch {
			packError = t('tools.bundleFailed');
		}
		packing = false;
	}
</script>

<PageHead title={t('nav.tools')} />

<nav class="operation-links" aria-label={t('nav.tools')}>
	{#each tools as tool (tool.href)}<a class="tile" href={tool.href}
			><span class="tool-icon"><Icon name={tool.icon} size={22} /></span>{t(tool.label)}</a
		>{/each}
</nav>
<DiskTests />

<section class="tile" aria-labelledby="tools-bundle">
	<h2 class="tile-title" id="tools-bundle">{t('tools.bundle')}</h2>
	<p class="muted">{t('tools.bundleText')}</p>
	<div class="lists">
		<div>
			<p class="name">{t('tools.bundleIn')}</p>
			<p class="muted">{t('tools.bundleInText')}</p>
		</div>
		<div>
			<p class="name">{t('tools.bundleOut')}</p>
			<p class="muted">{t('tools.bundleOutText')}</p>
		</div>
	</div>
	<p class="notice-warn">{t('tools.bundleCheck')}</p>
	{#if packError}<p class="notice-error" role="alert">{packError}</p>{/if}
	<div>
		<button type="button" class="btn btn-primary" disabled={packing} onclick={pack}
			>{t(packing ? 'tools.bundlePacking' : 'tools.bundleMake')}</button
		>
	</div>
</section>

<style>
	.operation-links {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
		gap: var(--gap);
		margin-bottom: var(--gap);
	}
	.operation-links a {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 18px;
		color: var(--text);
		text-decoration: none;
		font-weight: 600;
	}

	.operation-links a:hover,
	.operation-links a:focus-visible {
		border-color: var(--accent);
	}
	.tool-icon {
		color: var(--accent);
		display: flex;
	}

	section {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 18px;
		font-size: 0.875rem;
	}
	section p {
		margin: 0;
	}
	.lists {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
		gap: 12px 24px;
	}
	.name {
		font-weight: 600;
	}
</style>
