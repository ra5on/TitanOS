<script lang="ts">
	// A list of disks to choose from, one (radio) or several (checkboxes).
	import type { Disk } from '#lib/api.ts';
	import { formatDiskBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { diskType, isUsable } from './labels.ts';

	type Props = { disks: Disk[]; selected: string[]; single?: boolean; name: string };
	let { disks, selected = $bindable(), single = false, name }: Props = $props();

	function toggle(disk: string, checked: boolean) {
		if (single) selected = checked ? [disk] : [];
		else selected = checked ? [...selected, disk] : selected.filter((other) => other !== disk);
	}
</script>

<div class="disks">
	{#each disks as disk (disk.name)}
		<label class:picked={selected.includes(disk.name)}>
			<input type={single ? 'radio' : 'checkbox'} {name} disabled={!isUsable(disk)} checked={selected.includes(disk.name)} onchange={(event) => toggle(disk.name, event.currentTarget.checked)} />
			<span class="mono dev">{disk.name}</span>
			<span class="what">
				<span>{diskType(disk)} · {disk.model || '–'}{disk.serial ? ` · ${disk.serial}` : ''}</span>
				{#if disk.unavailableReason}<span class="has-data">{t(`disk.blocked.${disk.unavailableReason}`)}</span>{/if}
				{#if disk.role === 'used'}
					<span class="has-data">{t('disk.usedRole')}{disk.contents?.length ? ` – ${t('disk.contents', { contents: disk.contents.join(', ') })}` : ''}</span>
				{/if}
			</span>
			<span class="mono size">{formatDiskBytes(disk.sizeBytes)}</span>
		</label>
	{:else}
		<p class="muted">{t('create.disksNone')}</p>
	{/each}
</div>

<style>
	.disks {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	label {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 10px 12px;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		font-size: 0.875rem;
		cursor: pointer;
	}
	label.picked {
		border-color: var(--accent);
	}
	input {
		width: 18px;
		height: 18px;
		flex: none;
		margin: 0;
	}
	.dev {
		width: 5.5rem;
		flex: none;
		font-weight: 500;
	}
	.what {
		flex: 1 1 auto;
		min-width: 0;
		display: flex;
		flex-direction: column;
		font-size: 0.8125rem;
		color: var(--muted);
	}
	.has-data {
		color: var(--warn-text);
	}
	.size {
		flex: none;
	}
	p {
		font-size: 0.875rem;
	}
</style>
