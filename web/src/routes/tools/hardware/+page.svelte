<script lang="ts">
	// Hardware prüfen: a read-only compatibility report that can be saved
	// as text or JSON (for a bug report or a forum question).
	import { api } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import UnboundDevices from '#lib/components/UnboundDevices.svelte';
	import VirtCheck from '#lib/components/VirtCheck.svelte';
	import { formatBytes, formatDateTime, formatDiskBytes } from '#lib/format.ts';
	import {
		attention,
		needList,
		virtAdvice,
		type HardwareReport,
		type HwDisk,
		type Hint,
	} from '#lib/hwcheck.ts';
	import { apiErrorText, t, tOr, type MessageKey } from '#lib/i18n/index.svelte.ts';

	let report = $state<HardwareReport | null>(null);
	let running = $state(false);
	let error = $state('');
	let saveError = $state('');
	// usb-storage instead of uas, set on the owner's click (PLAN §45).
	type UASStatus = { devices: string[]; active: string[]; pending: string[]; writable: boolean };
	let quirks = $state<UASStatus | null>(null);
	let quirkBusy = $state('');
	let quirkError = $state('');

	async function check() {
		running = true;
		error = '';
		try {
			report = await api.get<HardwareReport>('/hardware/report');
		} catch (err) {
			error = apiErrorText(err);
		}
		try {
			quirks = await api.get<UASStatus>('/hardware/uas-quirks');
		} catch {
			quirks = null;
		}
		running = false;
	}

	async function setQuirk(device: string, on: boolean) {
		quirkBusy = device;
		quirkError = '';
		try {
			quirks = await api.put<UASStatus>('/hardware/uas-quirks', { device, on });
		} catch (err) {
			quirkError = apiErrorText(err);
		}
		quirkBusy = '';
	}

	// The server writes the file; the browser saves it.
	async function save(format: 'text' | 'json' | 'snapshot') {
		saveError = '';
		try {
			if (format === 'snapshot') {
				await api.download('/hardware/snapshot', 'titanos-hardware-snapshot.tar.gz', {
					serverName: true,
				});
				return;
			}
			await api.download(
				`/hardware/report?format=${format}`,
				`titanos-hardware.${format === 'text' ? 'txt' : 'json'}`,
				{ serverName: true },
			);
		} catch {
			saveError = t('hwcheck.exportFailed');
		}
	}

	// A text by a code from the server; an unknown code stays as it is.
	function byCode(key: string, fallback: string, params: Record<string, string>): string {
		return tOr(key, '\u0000') === '\u0000' ? fallback : t(key as MessageKey, params);
	}

	function hintText(hint: Hint, disk?: HwDisk): string {
		if (hint.code === 'smr')
			return t('hwcheck.hint.smr', { how: t(`hwcheck.smr.${disk?.smr ?? 'known_model'}`) });
		return byCode(`hwcheck.hint.${hint.code}`, hint.code, { setting: hint.setting ?? '' });
	}
	const driver = (name: string) =>
		name ? t('hwcheck.driver', { driver: name }) : t('hwcheck.noDriver');

	// What the rows of the report say at their right edge.
	const virtWarn = $derived(
		!!report && virtAdvice(report.virtualization).some((entry) => entry.level === 'warn'),
	);
	const shown = $derived(report ? attention(report.unbound) : []);

	// PLAN 67-1: the menu bar of the desktop – run the check, export the result.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	registerMenuCommands({
		'monitor.check': { run: () => void check(), enabled: () => !running },
		'monitor.export': { run: () => void save('text'), enabled: () => !!report },
		'monitor.exportJson': { run: () => void save('json'), enabled: () => !!report },
	});
</script>

<PageHead title={t('hwcheck.title')}>
	<p class="muted">{t('hwcheck.intro')}</p>
</PageHead>

<div class="actions">
	<button type="button" class="btn btn-primary" disabled={running} onclick={check}
		>{t(running ? 'hwcheck.running' : report ? 'hwcheck.again' : 'hwcheck.run')}</button
	>
	{#if report}
		<button type="button" class="btn" onclick={() => save('text')}>{t('hwcheck.exportText')}</button
		>
		<button type="button" class="btn" onclick={() => save('json')}>{t('hwcheck.exportJson')}</button
		>
		<button
			type="button"
			class="btn"
			title={t('hwcheck.exportSnapshotHint')}
			onclick={() => save('snapshot')}>{t('hwcheck.exportSnapshot')}</button
		>
	{/if}
</div>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if saveError}<p class="notice-error" role="alert">{saveError}</p>{/if}

{#if report}
	<p class="muted small">
		{t('hwcheck.generated', { time: formatDateTime(new Date(report.generated)) })} · {t(
			'hwcheck.exportNote',
		)}
	</p>

	<!-- PLAN 78-5: every part of the report is a plain heading with one flat
	     row per finding, device or fact. -->
	<SettingGroup title={t('hwcheck.summary')} id="hw-summary">
		{#if report.summary.length === 0}
			<p class="notice-ok">{t('hwcheck.allGood')}</p>
		{:else}
			{#each report.summary as finding, i (i)}
				<SettingRow
					title={byCode(`hwcheck.finding.${finding.code}`, finding.code, {
						subject: finding.subject ?? '',
					})}
				>
					{#snippet control()}<span class="chip {finding.level}"
							>{t(`hwcheck.level.${finding.level}`)}</span
						>{/snippet}
				</SettingRow>
			{/each}
		{/if}
	</SettingGroup>

	<SettingGroup title={t('hwcheck.cpu')} id="hw-cpu">
		<SettingRow icon="chip" title={report.cpu.model || '–'}>
			{#snippet description()}
				<p>
					{t('hwcheck.cpuLine', {
						cores: report?.cpu.cores || '–',
						threads: report?.cpu.threads ?? '',
						level: report?.cpu.level ?? '',
					})}
				</p>
				<p>{t('hwcheck.features', { list: needList(report?.cpu.features ?? []) || '–' })}</p>
			{/snippet}
		</SettingRow>
		{#if report.board}
			<SettingRow icon="grid" title={t('hwcheck.board', { board: report.board })} />
		{/if}
		<SettingRow
			icon="terminal"
			title={t('hwcheck.kernel', { kernel: report.kernel, boot: t(`hwcheck.boot.${report.boot}`) })}
		/>
		<SettingRow id="hw-virt" icon="monitor" title={t('hwcheck.virt.title')} open>
			{#snippet control()}<span class="chip {virtWarn ? 'warn' : 'ok'}"
					>{t(virtWarn ? 'hwcheck.look' : 'hwcheck.fine')}</span
				>{/snippet}
			<div class="srow-block"><VirtCheck virt={report.virtualization} /></div>
		</SettingRow>
	</SettingGroup>

	<SettingGroup title={t('hwcheck.memory')} id="hw-memory">
		<SettingRow
			icon="memory"
			title={formatBytes(report.memory.totalBytes)}
			desc={report.memory.ecc
				? t('hwcheck.ecc', {
						corrected: report.memory.corrected,
						uncorrected: report.memory.uncorrected,
					})
				: t('hwcheck.noEcc')}
		/>
	</SettingGroup>

	<SettingGroup title={t('hwcheck.sensors')} id="hw-sensors">
		{#each report.sensors as sensor, i (i)}
			<SettingRow
				icon="activity"
				title={sensor.chip}
				desc={t('hwcheck.sensorLine', { temps: sensor.temps, fans: sensor.fans })}
			/>
		{:else}
			<p class="muted">{t('hwcheck.none')}</p>
		{/each}
	</SettingGroup>

	<SettingGroup title={t('hwcheck.controllers')} id="hw-controllers">
		{#each report.controllers as controller (controller.address)}
			<SettingRow icon="chip" title={controller.name}>
				{#snippet description()}
					<p class="mono">
						{controller.address} · {controller.id} · {controller.kind.toUpperCase()} · {driver(
							controller.driver,
						)}
					</p>
					{#each controller.hints as hint (hint.code)}
						<p class="notice-warn">{hintText(hint)}</p>
					{/each}
				{/snippet}
			</SettingRow>
		{:else}
			<p class="muted">{t('hwcheck.none')}</p>
		{/each}
	</SettingGroup>

	<SettingGroup title={t('hwcheck.disks')} id="hw-disks">
		{#each report.disks as disk (disk.name)}
			{@const usb = disk.usb ?? ''}
			{@const quirkHint =
				quirks?.writable && usb
					? disk.hints.find(
							(hint) =>
								hint.code === 'uas_quirk' ||
								(hint.code === 'uas_quirk_active' && quirks?.devices.includes(usb)),
						)
					: undefined}
			<SettingRow icon="disk" title={disk.model || disk.name}>
				{#snippet description()}
					<p class="mono">
						{disk.name} · {formatDiskBytes(disk.sizeBytes)} · {disk.transport.toUpperCase()} · {disk.rotational
							? t('hwcheck.hdd')
							: t('hwcheck.ssd')}{disk.usb
							? ` · ${t('hwcheck.usbEnclosure', { id: disk.usb, driver: disk.usbDriver || '–' })}`
							: ''}
					</p>
					{#each disk.hints as hint (hint.code)}
						<p class={hint.code === 'smr' || hint.code === 'uas_quirk' ? 'notice-warn' : 'note'}>
							{hintText(hint, disk)}
						</p>
					{/each}
					{#if quirkHint && quirks?.pending.includes(usb)}
						<p>
							{t(quirks.devices.includes(usb) ? 'hwcheck.uas.pendingOn' : 'hwcheck.uas.pendingOff')}
						</p>
					{/if}
				{/snippet}
				{#snippet control()}
					{#if quirkHint && quirks}
						{@const set = quirks.devices.includes(usb)}
						<button
							type="button"
							class="btn"
							disabled={quirkBusy !== ''}
							onclick={() => setQuirk(usb, !set)}
							>{t(set ? 'hwcheck.uas.undo' : 'hwcheck.uas.apply')}</button
						>
					{/if}
				{/snippet}
			</SettingRow>
		{:else}
			<p class="muted">{t('hwcheck.none')}</p>
		{/each}
		{#if quirkError}<p class="error" role="alert">{quirkError}</p>{/if}
	</SettingGroup>

	<SettingGroup title={t('hwcheck.network')} id="hw-network">
		{#each report.network as nic (nic.name)}
			<SettingRow icon="globe" title={nic.name}>
				{#snippet description()}
					<p class="mono">{nic.address} · {driver(nic.driver)}</p>
				{/snippet}
				{#snippet control()}
					{nic.link
						? nic.speedMbs > 0
							? t('hwcheck.link', { speed: `${nic.speedMbs} Mbit/s` })
							: t('hwcheck.linkUp')
						: t('hwcheck.noLink')}
				{/snippet}
			</SettingRow>
		{:else}
			<p class="muted">{t('hwcheck.none')}</p>
		{/each}
	</SettingGroup>

	<SettingGroup title={t('hwcheck.graphics')} id="hw-graphics">
		{#each report.graphics as device (device.address)}
			<SettingRow icon="gpu" title={device.name}>
				{#snippet description()}
					<p class="mono">{device.address} · {device.id} · {driver(device.driver)}</p>
				{/snippet}
			</SettingRow>
		{:else}
			<p class="muted">{t('hwcheck.none')}</p>
		{/each}
	</SettingGroup>

	<!-- Like Einstellungen › Hardware: two rows; the list is their content. -->
	<SettingGroup title={t('hwcheck.drivers')} id="hw-drivers">
		<SettingRow
			id="hw-unbound"
			icon="tool"
			title={t('hwcheck.unbound')}
			desc={shown.length
				? t('hwcheck.unboundCount', { count: shown.length })
				: t('hwcheck.unboundNone')}
			open
		>
			{#snippet control()}<span class="chip {shown.length ? 'warn' : 'ok'}"
					>{t(shown.length ? 'hwcheck.look' : 'hwcheck.fine')}</span
				>{/snippet}
			<div class="srow-block">
				<UnboundDevices unbound={report.unbound} firmware={[]} part="unbound" />
			</div>
		</SettingRow>
		<SettingRow
			id="hw-firmware"
			icon="chip"
			title={t('hwcheck.firmware')}
			desc={report.firmwareMissing.length
				? t('hwcheck.firmwareCount', { count: report.firmwareMissing.length })
				: t('hwcheck.firmwareNone')}
			open
		>
			{#snippet control()}<span class="chip {report?.firmwareMissing.length ? 'warn' : 'ok'}"
					>{t(report?.firmwareMissing.length ? 'hwcheck.look' : 'hwcheck.fine')}</span
				>{/snippet}
			{#if report.firmwareMissing.length}
				<div class="srow-block">
					<UnboundDevices unbound={[]} firmware={report.firmwareMissing} part="firmware" />
				</div>
			{/if}
		</SettingRow>
	</SettingGroup>
{/if}

<style>
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2-5);
		margin-bottom: var(--gap);
	}
	.small {
		font-size: var(--text-base);
		margin: 0 0 var(--gap);
	}
	p {
		margin: 0;
	}
	.mono {
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.note {
		font-size: var(--text-base);
	}
	.notice-warn {
		margin: var(--space-1-5) 0;
	}
</style>
