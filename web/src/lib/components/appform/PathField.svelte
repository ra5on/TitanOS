<script lang="ts">
	// A folder of the NAS for an app: typed in or picked with the folder
	// browser. The row's hint says where it lies (where.ts).
	import FolderPicker from '#lib/components/FolderPicker.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { volumeOf } from '#lib/appform.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import type { Snippet } from 'svelte';
	type Props = {
		value: string;
		id?: string;
		label: string;
		invalid?: boolean;
		/** Shares the browser marks: name and volume. */
		shares?: { name: string; volume: string }[];
		oninput?: () => void;
		/** More buttons after the folder button (edit, remove). */
		extra?: Snippet;
	};
	let {
		value = $bindable(),
		id,
		label,
		invalid = false,
		shares = [],
		oninput,
		extra,
	}: Props = $props();
	let picking = $state(false);
	const volumes = $derived(
		(storage.status?.volumes ?? []).filter((volume) => volume.mounted && volume.mountPoint),
	);
	const volume = $derived(volumeOf(value, volumes));
	const start = $derived(
		volume
			? {
					volume: volume.name,
					path: value
						.trim()
						.slice(volume.mountPoint.length)
						.replace(/^\/+|\/+$/g, ''),
				}
			: undefined,
	);
</script>

<div class="line">
	<input
		{id}
		class="field mono"
		type="text"
		bind:value
		{oninput}
		spellcheck="false"
		aria-invalid={invalid}
		aria-label={id ? undefined : label}
	/>
	<button
		type="button"
		class="btn"
		disabled={volumes.length === 0}
		aria-label={t('appform.pickFolderFor', { label })}
		onclick={() => (picking = true)}
		><Icon name="folder" size={15} />{t('appform.pickFolder')}</button
	>
	{@render extra?.()}
</div>

{#if picking}
	<FolderPicker
		volumes={volumes.map((entry) => entry.name)}
		{shares}
		{start}
		onpick={(chosen) => {
			const root = volumes.find((entry) => entry.name === chosen.volume)?.mountPoint ?? '';
			value = chosen.path ? `${root.replace(/\/+$/, '')}/${chosen.path}` : root;
			picking = false;
			oninput?.();
		}}
		onclose={() => (picking = false)}
	/>
{/if}
