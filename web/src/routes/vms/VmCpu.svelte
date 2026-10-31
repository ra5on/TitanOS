<script lang="ts">
	// The section "Processor and memory" of the VM form: cores, pinning to
	// host CPUs, memory and firmware. The values belong to the form.
	import type { VMHost, VMStatus } from '#lib/api.ts';
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

<fieldset class="content" disabled={running}>
	<!-- Only once the limits are known: a slider made with a smaller maximum would show the wrong position. -->
	{#if status}
		<div class="line">
			<span
				>{t('vms.form.cores')}{#if machine.cpuModel}
					<span class="muted">· {machine.cpuModel}</span>{/if}</span
			>
			<div class="segments" role="radiogroup" aria-label={t('vms.form.cores')}>
				<button type="button" role="radio" aria-checked={!pinning} onclick={() => (pinning = false)}
					>{t('vms.form.auto')}</button
				>
				<button type="button" role="radio" aria-checked={pinning} onclick={() => (pinning = true)}
					>{t('vms.form.pin')}</button
				>
			</div>
		</div>
		{#if !pinning}
			<div class="slider">
				<div class="line">
					<label for="vm-vcpus">{t('vms.new.vcpus')}</label><span class="mono"
						>{t('vms.new.of', { value: vcpus, max: machine.cpus })}</span
					>
				</div>
				<input id="vm-vcpus" type="range" min="1" max={machine.cpus} step="1" bind:value={vcpus} />
				<small>{t('vms.form.autoBrief')}</small>
			</div>
		{:else}
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
			<small
				>{t('vms.form.pinSummary', {
					count: pinned.length,
					cpus: pinned.join(', ') || '–',
				})}</small
			>
			<small>{t('vms.form.pinNote')}</small>
			<label class="check"
				><input type="checkbox" bind:checked={exclusive} /><span>{t('vms.form.exclusive')}</span
				></label
			>
			<small>{t('vms.form.exclusiveNote')}</small>
			{#each cpuWarnings as warning (warning)}<small class="warn" role="status">{warning}</small
				>{/each}
		{/if}
		<div class="slider">
			<div class="line">
				<label for="vm-memory">{t('vms.new.memory')}</label><span class="mono"
					>{t('vms.new.memoryValue', {
						value: formatDecimal((memoryMiB * MIB) / GB),
						free: formatBytes(freeMemory),
					})}</span
				>
			</div>
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
			{#if memoryNote}<small class="warn">{memoryNote}</small>{/if}
			{#if template === 'windows' && memoryMiB < 4096}<small class="warn"
					>{t('vms.new.memoryWindows')}</small
				>{/if}
		</div>
	{/if}
	<details class="advanced">
		<summary>{t('vms.form.firmware')} · {firmware === 'uefi' ? 'UEFI' : 'BIOS'}</summary>
		<div class="advanced-body">
			<label for="vm-firmware">{t('vms.form.firmware')}</label>
			<select id="vm-firmware" class="field" bind:value={firmware} disabled={editing || busy}>
				<option value="uefi">{t('vms.form.uefi')}</option>
				<option value="bios">{t('vms.form.bios')}</option>
			</select>
			<small>{t(editing ? 'vms.form.firmwareFixed' : 'vms.form.firmwareNote')}</small>
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
	</details>
</fieldset>

<style>
	.advanced {
		border-top: 1px solid var(--panel-border);
		padding-top: 10px;
		min-width: 0;
	}
	summary {
		cursor: pointer;
		font-size: 0.8125rem;
	}
	.advanced-body {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding-top: 12px;
		min-width: 0;
	}
	.content {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 16px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	small {
		font-size: 0.75rem;
		color: var(--muted);
	}
	.warn {
		color: var(--warn-text);
	}
	.slider {
		display: flex;
		flex-direction: column;
		gap: 8px;
		font-size: 0.8125rem;
	}
	.line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px 12px;
	}
	.line label {
		flex-direction: row;
		gap: 6px;
		font-size: 0.875rem;
	}
	input[type='range'] {
		width: 100%;
		accent-color: var(--accent);
	}
	.facts {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.segments {
		display: inline-flex;
		flex-wrap: wrap;
		gap: 4px;
		padding: 3px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.segments button,
	.presets button {
		height: 2rem;
		padding: 0 12px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: 0.8125rem;
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}
	.presets {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.presets button {
		border: 1px solid var(--border);
	}
	fieldset {
		min-width: 0;
		margin: 0;
		border: 0;
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
		flex: none;
	}
	.cpus {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(118px, 1fr));
		gap: 8px;
	}
	.cpus button {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-height: 3.75rem;
		padding: 8px 10px;
		background: transparent;
		border: 2px solid var(--border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: 0.8125rem;
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
		gap: 6px;
	}
	.cpu-note {
		font-size: 0.6875rem;
		line-height: 1.3;
	}
</style>
