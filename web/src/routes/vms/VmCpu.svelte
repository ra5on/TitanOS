<script lang="ts">
	// The section "Processor and memory" of the VM form: cores, pinning to
	// host CPUs, memory and firmware. The values belong to the form.
	import type { VMHost, VMStatus } from '#lib/api.ts';
	import FormRow from '#lib/components/appform/FormRow.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import RetryNotice from '#lib/components/RetryNotice.svelte';
	import { GB, MIB, formatBytes, formatDecimal } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';

	let {
		status,
		cpus,
		vmName,
		editing,
		running,
		busy,
		template,
		secure,
		hostError,
		onretryhost,
		vcpus = $bindable(),
		memoryMiB = $bindable(),
		pinning = $bindable(),
		pinned = $bindable(),
		exclusive = $bindable(),
		firmware = $bindable(),
	}: {
		status: VMStatus | null;
		cpus: VMHost['cpus'];
		/** The VM being edited, whose own pinning does not count as taken. */
		vmName?: string;
		editing: boolean;
		running: boolean;
		busy: boolean;
		template: string;
		secure: boolean;
		hostError?: string;
		onretryhost: () => void;
		vcpus: number;
		/** As titand and QEMU take it; shown and set in decimal GB. */
		memoryMiB: number;
		pinning: boolean;
		pinned: number[];
		exclusive: boolean;
		firmware: 'uefi' | 'bios';
	} = $props();

	const machine = $derived(
		status?.host ?? {
			availableMemoryBytes: 0,
			cpus: 1,
			memoryBytes: 0,
			usedMemoryBytes: 0,
			cpuModel: '',
		},
	);
	const maxMemory = $derived(Math.max(1, Math.floor(machine.memoryBytes / GB)));
	const freeMemory = $derived(
		Math.max(0, machine.availableMemoryBytes ?? machine.memoryBytes - machine.usedMemoryBytes),
	);
	const hybrid = $derived(cpus.some((cpu) => cpu.kind !== ''));
	const memoryNote = $derived(
		memoryMiB * MIB > freeMemory && !running
			? t('vms.new.memoryOver', { free: formatBytes(freeMemory) })
			: '',
	);

	/** Threads of one core go to a VM together. */
	function togglePin(id: number) {
		const core = cpus.find((cpu) => cpu.id === id)?.siblings ?? [id];
		pinned = pinned.includes(id)
			? pinned.filter((cpu) => !core.includes(cpu))
			: [...new Set([...pinned, ...core])].sort((a, b) => a - b);
	}
	const others = (cpu: { usedBy: string[] }) => cpu.usedBy.filter((vm) => vm !== vmName);
	const preset = (pick: (cpu: VMHost['cpus'][number]) => boolean) =>
		(pinned = cpus.filter(pick).map((cpu) => cpu.id));
	function cpuNote(cpu: VMHost['cpus'][number]): string {
		if (others(cpu).length > 0) return t('vms.form.cpuUsedBy', { vms: others(cpu).join(', ') });
		if (cpu.id === 0) return t('vms.form.cpuZero');
		const sibling = cpu.siblings.filter((id) => id !== cpu.id);
		return sibling.length > 0 ? t('vms.form.cpuSibling', { cpu: sibling.join(', ') }) : '';
	}
	const cpuWarnings = $derived.by(() => {
		if (!pinning) return [];
		const chosen = cpus.filter((cpu) => pinned.includes(cpu.id));
		const warnings: string[] = [];
		if (chosen.length === 0) warnings.push(t('vms.form.warnNone'));
		if (new Set(chosen.map((cpu) => cpu.kind)).size > 1) warnings.push(t('vms.form.warnMixed'));
		if (pinned.includes(0)) warnings.push(t('vms.form.warnZero'));
		if (chosen.some((cpu) => others(cpu).length > 0)) warnings.push(t('vms.form.warnShared'));
		return warnings;
	});
</script>

{#snippet memoryNotes()}
	{#if memoryNote}<span class="warn">{memoryNote}</span>{/if}
	{#if template === 'windows' && memoryMiB < 4096}<span class="warn"
			>{t('vms.new.memoryWindows')}</span
		>{/if}
{/snippet}

<fieldset class="srows" disabled={running}>
	<!-- Only once the limits are known: a slider made with a smaller maximum would show the wrong position. -->
	{#if status}
		<FormRow label={t('vms.form.cores')} hint={machine.cpuModel}>
			<div class="segments" role="radiogroup" aria-label={t('vms.form.cores')}>
				<button type="button" role="radio" aria-checked={!pinning} onclick={() => (pinning = false)}
					>{t('vms.form.auto')}</button
				>
				<button type="button" role="radio" aria-checked={pinning} onclick={() => (pinning = true)}
					>{t('vms.form.pin')}</button
				>
			</div>
		</FormRow>
		{#if !pinning}
			<FormRow label={t('vms.new.vcpus')} id="vm-vcpus" hint={t('vms.form.autoBrief')}>
				<span class="mono">{t('vms.new.of', { value: vcpus, max: machine.cpus })}</span>
				<input id="vm-vcpus" type="range" min="1" max={machine.cpus} step="1" bind:value={vcpus} />
			</FormRow>
		{:else}
			<FormRow
				label={t('vms.form.pin')}
				hint={t('vms.form.pinSummary', {
					count: pinned.length,
					cpus: pinned.join(', ') || '–',
				})}
				wide
			>
				<div class="presets">
					{#if hybrid}
						<button
							type="button"
							onclick={() =>
								preset((cpu) => cpu.kind === 'p' && cpu.id !== 0 && !cpu.siblings.includes(0))}
							>{t('vms.form.presetP')}</button
						>
						<button
							type="button"
							onclick={() => preset((cpu) => cpu.kind === 'e' && others(cpu).length === 0)}
							>{t('vms.form.presetE')}</button
						>
					{/if}
					<button type="button" onclick={() => (pinned = [])}>{t('vms.form.presetNone')}</button>
				</div>
				{#if hostError}<RetryNotice text={hostError} onretry={onretryhost} />{/if}
				<div class="cpus" role="group" aria-label={t('vms.form.pin')}>
					{#each cpus as cpu (cpu.id)}
						<button
							type="button"
							aria-pressed={pinned.includes(cpu.id)}
							onclick={() => togglePin(cpu.id)}
						>
							<span class="cpu-head"
								><span class="mono">CPU {cpu.id}</span>{#if cpu.kind}<span class="chip"
										>{t(cpu.kind === 'p' ? 'vms.form.pCore' : 'vms.form.eCore')}</span
									>{/if}</span
							>
							<span class="muted cpu-note">{cpuNote(cpu)}</span>
						</button>
					{/each}
				</div>
				<small>{t('vms.form.pinNote')}</small>
				{#each cpuWarnings as warning (warning)}<small class="warn" role="status">{warning}</small
					>{/each}
			</FormRow>
			<FormRow label={t('vms.form.exclusive')} id="vm-exclusive" hint={t('vms.form.exclusiveNote')}>
				<input
					id="vm-exclusive"
					class="switch"
					type="checkbox"
					role="switch"
					bind:checked={exclusive}
				/>
			</FormRow>
		{/if}
		<FormRow
			label={t('vms.new.memory')}
			id="vm-memory"
			more={memoryNote || (template === 'windows' && memoryMiB < 4096) ? memoryNotes : undefined}
		>
			<span class="mono"
				>{t('vms.new.memoryValue', {
					value: formatDecimal((memoryMiB * MIB) / GB),
					free: formatBytes(freeMemory),
				})}</span
			>
			<input
				id="vm-memory"
				type="range"
				min="1"
				max={maxMemory}
				step="1"
				bind:value={
					// The slider moves in whole GB; an untouched value keeps its MiB exactly.
					() => (memoryMiB * MIB) / GB,
					(gb: number) => (memoryMiB = Math.round((Number(gb) * GB) / MIB))
				}
			/>
		</FormRow>
	{/if}
</fieldset>
<!-- Outside the locked rows: the row opens also while the VM runs. -->
<SettingRow
	id="vm-firmware-row"
	title={t('vms.form.firmware')}
	summary={firmware === 'uefi' ? 'UEFI' : 'BIOS'}
>
	<SettingSub
		title={t('vms.form.firmware')}
		desc={t(editing ? 'vms.form.firmwareFixed' : 'vms.form.firmwareNote')}
		for="vm-firmware"
	>
		{#snippet control()}
			<select id="vm-firmware" class="field" bind:value={firmware} disabled={editing || busy}>
				<option value="uefi">{t('vms.form.uefi')}</option>
				<option value="bios">{t('vms.form.bios')}</option>
			</select>
		{/snippet}
	</SettingSub>
	<div class="srow-block">
		{#if firmware === 'bios' && template === 'windows'}<small class="warn"
				>{t('vms.form.biosWindows')}</small
			>{/if}
		<div class="facts">
			<span class="chip">{t(firmware === 'bios' ? 'vms.form.bios' : 'vms.form.uefi')}</span>
			<span class="chip">{t('vms.new.machine')}</span>
			<span class="chip">{t('vms.new.cpuMode')}</span>
			{#if secure}<span class="chip">{t('vms.form.secureChip')}</span>{/if}
		</div>
	</div>
</SettingRow>

<style>
	small {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.warn {
		display: block;
		color: var(--warn-text);
	}
	input[type='range'] {
		align-self: stretch;
		width: 100%;
		accent-color: var(--accent);
	}
	.facts {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
	}
	.segments {
		display: inline-flex;
		flex-wrap: wrap;
		gap: var(--space-1);
		padding: 3px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.segments button,
	.presets button {
		height: 2rem;
		padding: 0 var(--space-3);
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}
	.presets {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
	}
	.presets button {
		border: 1px solid var(--border);
	}
	.cpus {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(118px, 1fr));
		gap: var(--space-2);
	}
	.cpus button {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		min-height: 3.75rem;
		padding: var(--space-2) var(--space-2-5);
		background: transparent;
		border: 2px solid var(--border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		text-align: left;
		cursor: pointer;
	}
	.cpus button[aria-pressed='true'] {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 12%, transparent);
	}
	.cpu-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-1-5);
	}
	.cpu-note {
		font-size: var(--text-xs);
		line-height: 1.3;
	}
</style>
