<script lang="ts">
	// Removing an installed plugin.
	import Dialog from '#lib/components/Dialog.svelte';
	import PluginSteps from '#lib/components/PluginSteps.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { PluginProgress } from '#lib/plugin-progress.ts';
	import ProblemNotice from './ProblemNotice.svelte';
	import type { Problem } from './plugins.ts';

	type Props = {
		title: string;
		busy: boolean;
		steps: PluginProgress | undefined;
		error: Problem | null;
		onclose: () => void;
		onremove: () => void;
	};
	let { title, busy, steps, error, onclose, onremove }: Props = $props();
</script>

<Dialog title={t('plugins.removeTitle', { name: title })} locked={busy} {onclose}>
	<p class="text">{t('plugins.removeText')}</p>
	{#if steps && (busy || steps.state !== 'done')}<PluginSteps progress={steps} />{/if}
	{#if error}<ProblemNotice problem={error} />{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose} disabled={busy}>{t('common.cancel')}</button
		>
		<button type="button" class="btn btn-warn" onclick={onremove} disabled={busy}
			>{t('plugins.removeSubmit')}</button
		>
	</div>
</Dialog>

<style>
	.text {
		font-size: 0.875rem;
	}
</style>
