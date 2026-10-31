<script lang="ts">
	// Adding a community plugin from a link: the manifest is read first, the
	// image is downloaded when installing.
	import Dialog from '#lib/components/Dialog.svelte';
	import PluginSteps from '#lib/components/PluginSteps.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import type { PluginProgress } from '#lib/plugin-progress.ts';
	import ProblemNotice from './ProblemNotice.svelte';
	import { localized, type Plugin, type Problem } from './plugins.ts';

	type Props = {
		url: string;
		volume: string;
		activate: boolean;
		preview: Plugin | null;
		/** No volume holds plugins yet: one is chosen here. */
		chooseVolume: boolean;
		volumes: string[];
		busy: boolean;
		checking: boolean;
		steps: PluginProgress | undefined;
		error: Problem | null;
		onclose: () => void;
		oncheck: () => void;
		oninstall: () => void;
		onedit: () => void;
	};
	let {
		url = $bindable(),
		volume = $bindable(),
		activate = $bindable(),
		preview,
		chooseVolume,
		volumes,
		busy,
		checking,
		steps,
		error,
		onclose,
		oncheck,
		oninstall,
		onedit,
	}: Props = $props();
	const text = (texts: Record<string, string> | null) => localized(texts, i18n.locale);
</script>

<Dialog title={t('plugins.fromLinkTitle')} locked={busy} {onclose}>
	<p class="text">{t('plugins.fromLinkText')}</p>
	<form class="stack" onsubmit={(event) => (event.preventDefault(), oncheck())}>
		<label class="stack"
			><span>{t('plugins.fromLinkURL')}</span>
			<input
				class="field mono"
				type="url"
				placeholder="https://example.org/plugins/name.plugin.json"
				bind:value={url}
				disabled={busy}
				oninput={onedit}
			/>
		</label>
		<div class="row-actions">
			<button type="submit" class="btn" disabled={busy || checking || !url.trim()}
				>{t(checking ? 'plugins.fromLinkChecking' : 'plugins.fromLinkCheck')}</button
			>
		</div>
	</form>
	{#if preview}
		<div class="stack">
			<p>
				<strong>{preview.title}</strong>
				<span class="muted mono"
					>{preview.name} · {preview.version || '–'} · {formatBytes(preview.size)}</span
				>
			</p>
			{#if text(preview.description)}<p class="muted">{text(preview.description)}</p>{/if}
			<p class="notice-warn">
				{t(preview.official ? 'plugins.trustOfficial' : 'plugins.trustLink')}
			</p>
			{#if preview.problem === 'kernel'}<p class="notice-warn">
					{t('plugins.incompatible')}
				</p>{/if}
			{#if chooseVolume}
				<label class="stack"
					><span>{t('plugins.volume')}</span>
					<select class="field" bind:value={volume}
						>{#each volumes as entry (entry)}<option value={entry}>{entry}</option>{/each}</select
					>
				</label>
			{/if}
			<label class="check"
				><input type="checkbox" bind:checked={activate} disabled={busy} /><span
					>{t('plugins.activateNow')}</span
				></label
			>
			{#if steps}<PluginSteps progress={steps} />{/if}
		</div>
	{/if}
	{#if error}<ProblemNotice problem={error} />{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose} disabled={busy}>{t('common.cancel')}</button
		>
		<!-- The API wants the plugin name as confirmation for an unsigned plugin; the click is that confirmation. -->
		<button
			type="button"
			class="btn btn-primary"
			onclick={oninstall}
			disabled={busy || !preview || preview.problem === 'kernel' || !volume}
			>{t(busy ? 'plugins.downloading' : 'plugins.downloadInstall')}</button
		>
	</div>
</Dialog>

<style>
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: 0.875rem;
	}
	.check span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.text {
		font-size: 0.875rem;
	}
</style>
