<script lang="ts">
	// Updating an installed plugin from a share or the catalog.
	import Dialog from '#lib/components/Dialog.svelte';
	import PluginSteps from '#lib/components/PluginSteps.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { PluginProgress } from '#lib/plugin-progress.ts';
	import ProblemNotice from './ProblemNotice.svelte';
	import type { Problem } from './plugins.ts';

	type Props = {
		target: { title: string; version: string; official: boolean; download?: boolean; path: string };
		busy: boolean;
		steps: PluginProgress | undefined;
		error: Problem | null;
		onclose: () => void;
		onupdate: () => void;
	};
	let { target, busy, steps, error, onclose, onupdate }: Props = $props();
</script>

<Dialog
	title={t('plugins.updateTitle', { name: target.title, version: target.version })}
	locked={busy}
	{onclose}
>
	<p class="text">{t('plugins.updateText')}</p>
	{#if !target.official}<p class="notice-warn">{t('plugins.trustCommunity')}</p>{/if}
	{#if target.download}<p class="text">{t('plugins.updateDownloadText')}</p>{:else}<p
			class="muted mono path"
		>
			{target.path}
		</p>{/if}
	{#if steps && (busy || steps.state !== 'done')}<PluginSteps progress={steps} />{/if}
	{#if error}<ProblemNotice problem={error} />{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose} disabled={busy}>{t('common.cancel')}</button
		>
		<!-- The API wants the plugin name as confirmation for an unsigned version; the click is that confirmation. -->
		<button type="button" class="btn btn-primary" onclick={onupdate} disabled={busy}
			>{t(busy ? 'plugins.updating' : 'plugins.update')}</button
		>
	</div>
</Dialog>

<style>
	.text {
		font-size: 0.875rem;
	}
	.path {
		font-size: 0.75rem;
		overflow-wrap: anywhere;
	}
</style>
