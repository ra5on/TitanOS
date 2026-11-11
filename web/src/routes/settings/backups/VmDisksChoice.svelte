<script lang="ts">
	// PLAN 79-9 (owner's draft Entwuerfe/79-sicherung-vms): how a readable
	// copy saves the disks of running VMs, and whether the owner wants that.
	// The list comes from GET /backups/vm-disks for the sources chosen in the
	// form; the group only shows when those sources hold disks of VMs.
	// The backend knows one setting for raw images per job (not per disk).
	import { api } from '#lib/api.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { baseName } from '#lib/backup-notes.ts';
	import { vmDisksQuery, type VMDisk, type VMDisks, type VMDisksAnswer } from './data-backups.ts';

	let {
		vmDisks = $bindable(),
		sources,
		volumes,
		places,
	}: {
		/** The job's choice; undefined while none was made. */
		vmDisks: VMDisks | undefined;
		sources: string[];
		volumes: string[];
		places: string[];
	} = $props();

	let answer = $state<VMDisksAnswer | null>(null);
	let failed = $state(false);
	const query = $derived(vmDisksQuery({ sources, volumes, places }, vmDisks));
	$effect(() => {
		const wanted = query;
		if (!wanted) {
			answer = null;
			return;
		}
		// Typing in the form must not ask the NAS for every keystroke.
		const timer = setTimeout(async () => {
			try {
				const next = await api.get<VMDisksAnswer>(`/backups/vm-disks?${wanted}`);
				if (wanted === query) {
					answer = next;
					failed = false;
				}
			} catch {
				if (wanted === query) failed = true;
			}
		}, 250);
		return () => clearTimeout(timer);
	});

	const disks = $derived(answer?.disks ?? []);
	const running = $derived(disks.filter((disk) => disk.state === 'running'));
	/** Raw images of running VMs: only for them the second choice matters. */
	const raw = $derived(
		running.some(
			(disk) =>
				disk.format === 'raw' || disk.reason === 'vm_disk_raw_image' || disk.way === 'pause',
		),
	);

	function wayText(disk: VMDisk): string {
		if (disk.way === 'libvirt_snapshot')
			return t(
				disk.agent ? 'backup.vm.way.libvirt_snapshot.agent' : 'backup.vm.way.libvirt_snapshot',
			);
		return t(`backup.vm.way.${disk.way}` as MessageKey);
	}
	const wayTone = (disk: VMDisk) =>
		disk.way === 'btrfs_snapshot' || (disk.way === 'libvirt_snapshot' && disk.agent)
			? 'ok'
			: disk.way === 'copy' || disk.way === 'pause' || disk.way === 'libvirt_snapshot'
				? 'info'
				: 'warn';
	const REASONS = [
		'vm_disk_busy',
		'vm_disk_passthrough',
		'vm_disk_backing_chain',
		'vm_disk_no_space',
		'vm_disk_snapshot_failed',
		'vm_disk_raw_image',
	];
	function reasonText(disk: VMDisk): string {
		if (!disk.reason) return '';
		const code = REASONS.includes(disk.reason) ? disk.reason : 'other';
		return t(`backup.vm.reason.${code}` as MessageKey, {
			need: formatBytes(disk.needBytes ?? 0),
			free: formatBytes(disk.freeBytes ?? 0),
		});
	}
	function describe(disk: VMDisk): string {
		const parts = [
			formatBytes(disk.sizeBytes),
			t(disk.state === 'running' ? 'backup.vm.state.running' : 'backup.vm.state.stopped'),
			disk.format === 'raw' ? t('backup.vm.format.raw') : disk.format,
		];
		if (disk.way === 'libvirt_snapshot' && disk.agent) parts.push(t('backup.vm.agent'));
		const reason = reasonText(disk);
		if (reason && disk.reason !== 'vm_disk_raw_image') parts.push(reason);
		else if (reason && disk.format !== 'raw') parts.push(reason);
		return parts.filter(Boolean).join(' · ');
	}
</script>

{#if disks.length}
	<div class="vm-disks">
		<SettingGroup id="backup-vm-disks" title={t('backup.vm.title')} desc={t('backup.vm.intro')}>
			{#if !vmDisks && (answer?.choiceNeeded || running.length > 0)}
				<p class="notice-warn" role="status">{t('backup.vm.choiceNeeded')}</p>
			{/if}
			<SettingRow
				id="vm-disks"
				icon="monitor"
				title={t('backup.vm.row')}
				desc={disks.length === 1
					? t('backup.vm.count.one')
					: t('backup.vm.count.many', { count: disks.length })}
				for="backup-vm-mode"
				open={true}
			>
				{#snippet control()}
					<select
						id="backup-vm-mode"
						class="field"
						value={vmDisks?.mode ?? ''}
						onchange={(e) => {
							const mode = e.currentTarget.value as VMDisks['mode'];
							vmDisks = mode === 'skip' ? { mode } : { mode, raw: vmDisks?.raw || 'skip' };
						}}
					>
						{#if !vmDisks}<option value="" disabled>{t('backup.vm.choose')}</option>{/if}
						<option value="consistent">{t('backup.vm.mode.consistent')}</option>
						<option value="skip">{t('backup.vm.mode.skip')}</option>
					</select>
				{/snippet}
				{#each disks as disk (disk.path)}
					<SettingSub title="{disk.vm} · {baseName(disk.file)}" desc={describe(disk)}>
						{#snippet control()}<span class="chip {wayTone(disk)}">{wayText(disk)}</span>{/snippet}
					</SettingSub>
				{/each}
				{#if raw && vmDisks?.mode === 'consistent'}
					<SettingSub title={t('backup.vm.raw')} desc={t('backup.vm.raw.desc')} for="backup-vm-raw">
						{#snippet control()}
							<select
								id="backup-vm-raw"
								class="field"
								value={vmDisks?.raw || 'skip'}
								onchange={(e) =>
									(vmDisks = {
										mode: 'consistent',
										raw: e.currentTarget.value as VMDisks['raw'],
									})}
							>
								<option value="skip">{t('backup.vm.raw.skip')}</option>
								<option value="pause">{t('backup.vm.raw.pause')}</option>
							</select>
						{/snippet}
					</SettingSub>
				{/if}
			</SettingRow>
			{#if vmDisks?.mode === 'skip'}
				<p class="notice-warn" role="status">{t('backup.vm.skipNote')}</p>
			{/if}
		</SettingGroup>
	</div>
{:else if failed}
	<p class="muted small">{t('backup.vm.loadFailed')}</p>
{/if}

<style>
	.vm-disks {
		min-width: 0;
		font-size: 0.875rem;
	}
	.vm-disks :global(.sgroup > p) {
		margin: var(--space-1) 0;
	}
	/* The way a disk is saved is a sentence: it wraps instead of being cut. */
	.vm-disks :global(.chip) {
		white-space: normal;
		text-align: left;
		height: auto;
	}
</style>
