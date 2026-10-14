<script lang="ts">
	// What is running, and under which licenses.
	import { api, ApiError } from '#lib/api.ts';
	import Logo from '#lib/components/Logo.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	type About = { version: string; kernel: string; license: string; thirdParty: string };

	let about = $state<About | null>(null);
	let error = $state('');

	$effect(() => {
		api.get<About>('/about').then(
			(res) => (about = res),
			(err) => (error = errorText(err instanceof ApiError ? err.code : 'internal'))
		);
	});

	const slot = $derived(system.info?.image?.slot.toUpperCase() ?? '');
</script>

<div class="title">
	<p class="muted crumbs"><a href="/settings">{t('nav.settings')}</a> › {t('about.title')}</p>
	<h1>{t('about.title')}</h1>
</div>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

{#if about}
	<section class="tile">
		<Logo size={40} />
		<dl>
			<div><dt>{t('about.version')}</dt><dd class="mono">{about.version}</dd></div>
			{#if slot}<div><dt>{t('about.slot')}</dt><dd class="mono">{slot}</dd></div>{/if}
			<div><dt>{t('about.kernel')}</dt><dd class="mono">{about.kernel || '–'}</dd></div>
			<div><dt>{t('about.base')}</dt><dd>{t('about.baseText')}</dd></div>
		</dl>
		<p><a href="https://github.com/ra5on/TitanOS" target="_blank" rel="noreferrer">github.com/ra5on/TitanOS</a></p>
	</section>

	<section class="tile" aria-labelledby="about-license">
		<h2 class="tile-title" id="about-license">{t('about.license')}</h2>
		<p>{t('about.licenseText')}</p>
		<p class="muted">{t('about.licenseLater')}</p>
		{#if about.license}
			<details>
				<summary>{t('about.licenseFull')}</summary>
				<pre>{about.license}</pre>
			</details>
		{/if}
	</section>

	<section class="tile" aria-labelledby="about-third">
		<h2 class="tile-title" id="about-third">{t('about.thirdParty')}</h2>
		<p class="muted">{t('about.thirdPartyText')}</p>
		{#if about.thirdParty}<pre class="scroll">{about.thirdParty}</pre>{/if}
	</section>
{/if}

<style>
	.title {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.crumbs {
		font-size: 0.8125rem;
	}
	h1 {
		font-size: 1.5rem;
		font-weight: 600;
	}
	section {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 18px;
		font-size: 0.875rem;
	}
	p {
		margin: 0;
		max-width: 60rem;
	}
	dl {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
		gap: 12px 24px;
		margin: 0;
	}
	dt {
		color: var(--muted);
		font-size: 0.75rem;
	}
	dd {
		margin: 2px 0 0;
		overflow-wrap: anywhere;
	}
	summary {
		cursor: pointer;
		color: var(--muted);
	}
	pre {
		margin: 10px 0 0;
		padding: 12px;
		background: var(--track);
		border-radius: var(--radius-ctl);
		font-family: var(--font-mono);
		font-size: 0.75rem;
		line-height: 1.5;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.scroll {
		margin: 0;
		max-height: 28rem;
		overflow: auto;
		white-space: pre;
		overflow-wrap: normal;
		scrollbar-width: thin;
		scrollbar-color: var(--muted) transparent;
	}
</style>
