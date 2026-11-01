<script lang="ts">
	// Hardware prüfen: a read-only compatibility report that can be saved
	// as text or JSON (for a bug report or a forum question).
	import { api } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import UnboundDevices from '#lib/components/UnboundDevices.svelte';
	import VirtCheck from '#lib/components/VirtCheck.svelte';
	import { formatBytes, formatDateTime, formatDiskBytes } from '#lib/format.ts';
	import { needList, type HardwareReport, type HwDisk, type Hint } from '#lib/hwcheck.ts';
	import { apiErrorText, t, tOr, type MessageKey } from '#lib/i18n/index.svelte.ts';

	let report = $state<HardwareReport | null>(null);
	let running = $state(false);
	let error = $state('');
	let saveError = $state('');

	async function check() {
		running = true;
		error = '';
		try {
			report = await api.get<HardwareReport>('/hardware/report');
		} catch (err) {
			error = apiErrorText(err);
		}
		running = false;
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
</script>

<PageHead title={t('hwcheck.title')} parent={{ href: '/tools', label: t('nav.tools') }}>
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

	<section class="tile panel" aria-labelledby="hw-summary">
		<h2 class="tile-title" id="hw-summary">{t('hwcheck.summary')}</h2>
		{#if report.summary.length === 0}
			<p class="notice-ok">{t('hwcheck.allGood')}</p>
		{:else}
			<ul class="findings">
				{#each report.summary as finding, i (i)}
					<li>
						<span class="chip {finding.level}">{t(`hwcheck.level.${finding.level}`)}</span>
						{byCode(`hwcheck.finding.${finding.code}`, finding.code, {
							subject: finding.subject ?? '',
						})}
					</li>
				{/each}
			</ul>
		{/if}
	</section>

	<div class="grid">
		<section class="tile panel" aria-labelledby="hw-cpu">
			<h2 class="tile-title" id="hw-cpu">{t('hwcheck.cpu')}</h2>
			<p class="name">{report.cpu.model || '–'}</p>
			<p>
				{t('hwcheck.cpuLine', {
					cores: report.cpu.cores || '–',
					threads: report.cpu.threads,
					level: report.cpu.level,
				})}
			</p>
			<p class="muted">{t('hwcheck.features', { list: needList(report.cpu.features) || '–' })}</p>
			{#if report.board}<p class="muted">{t('hwcheck.board', { board: report.board })}</p>{/if}
			<p class="muted">
				{t('hwcheck.kernel', { kernel: report.kernel, boot: t(`hwcheck.boot.${report.boot}`) })}
			</p>
			<h3 class="sub">{t('hwcheck.virt.title')}</h3>
			<VirtCheck virt={report.virtualization} />
		</section>

		<section class="tile panel" aria-labelledby="hw-memory">
			<h2 class="tile-title" id="hw-memory">{t('hwcheck.memory')}</h2>
			<p class="name">{formatBytes(report.memory.totalBytes)}</p>
			<p class="muted">
				{report.memory.ecc
					? t('hwcheck.ecc', {
							corrected: report.memory.corrected,
							uncorrected: report.memory.uncorrected,
						})
					: t('hwcheck.noEcc')}
			</p>
			<h3 class="sub">{t('hwcheck.sensors')}</h3>
			{#each report.sensors as sensor, i (i)}
				<p>
					<span class="mono">{sensor.chip}</span> · {t('hwcheck.sensorLine', {
						temps: sensor.temps,
						fans: sensor.fans,
					})}
				</p>
			{:else}
				<p class="muted">{t('hwcheck.none')}</p>
			{/each}
		</section>
	</div>

	<section class="tile panel" aria-labelledby="hw-controllers">
		<h2 class="tile-title" id="hw-controllers">{t('hwcheck.controllers')}</h2>
		{#each report.controllers as controller (controller.address)}
			<div class="row">
				<div class="main">
					<span class="name">{controller.name}</span>
					<span class="muted mono"
						>{controller.address} · {controller.id} · {controller.kind.toUpperCase()} · {driver(
							controller.driver,
						)}</span
					>
				</div>
				{#each controller.hints as hint (hint.code)}
					<p class="notice-warn">{hintText(hint)}</p>
				{/each}
			</div>
		{:else}
			<p class="muted">{t('hwcheck.none')}</p>
		{/each}
	</section>

	<section class="tile panel" aria-labelledby="hw-disks">
		<h2 class="tile-title" id="hw-disks">{t('hwcheck.disks')}</h2>
		{#each report.disks as disk (disk.name)}
			<div class="row">
				<div class="main">
					<span class="name">{disk.model || disk.name}</span>
					<span class="muted mono"
						>{disk.name} · {formatDiskBytes(disk.sizeBytes)} · {disk.transport.toUpperCase()} · {disk.rotational
							? t('hwcheck.hdd')
							: t('hwcheck.ssd')}{disk.usb
							? ` · ${t('hwcheck.usbEnclosure', { id: disk.usb, driver: disk.usbDriver || '–' })}`
							: ''}</span
					>
				</div>
				{#each disk.hints as hint (hint.code)}
					<p
						class={hint.code === 'smr' || hint.code === 'uas_quirk' ? 'notice-warn' : 'muted note'}
					>
						{hintText(hint, disk)}
					</p>
				{/each}
			</div>
		{:else}
			<p class="muted">{t('hwcheck.none')}</p>
		{/each}
	</section>

	<div class="grid">
		<section class="tile panel" aria-labelledby="hw-network">
			<h2 class="tile-title" id="hw-network">{t('hwcheck.network')}</h2>
			{#each report.network as nic (nic.name)}
				<div class="row">
					<div class="main">
						<span class="name">{nic.name}</span>
						<span class="muted mono">{nic.address} · {driver(nic.driver)}</span>
						<span class="muted"
							>{nic.link
								? nic.speedMbs > 0
									? t('hwcheck.link', { speed: `${nic.speedMbs} Mbit/s` })
									: t('hwcheck.linkUp')
								: t('hwcheck.noLink')}</span
						>
					</div>
				</div>
			{:else}
				<p class="muted">{t('hwcheck.none')}</p>
			{/each}
		</section>

		<section class="tile panel" aria-labelledby="hw-graphics">
			<h2 class="tile-title" id="hw-graphics">{t('hwcheck.graphics')}</h2>
			{#each report.graphics as device (device.address)}
				<div class="row">
					<div class="main">
						<span class="name">{device.name}</span>
						<span class="muted mono">{device.address} · {device.id} · {driver(device.driver)}</span>
					</div>
				</div>
			{:else}
				<p class="muted">{t('hwcheck.none')}</p>
			{/each}
		</section>
	</div>

	<section class="tile panel" aria-labelledby="hw-drivers">
		<h2 class="tile-title" id="hw-drivers">{t('hwcheck.drivers')}</h2>
		<UnboundDevices unbound={report.unbound} firmware={report.firmwareMissing} />
	</section>
{/if}

<style>
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
		margin-bottom: var(--gap);
	}
	.small {
		font-size: var(--text-base);
		margin: 0 0 var(--gap);
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(min(100%, 380px), 1fr));
		gap: var(--gap);
	}
	section {
		margin-bottom: var(--gap);
		font-size: var(--text-md);
	}
	.grid section {
		margin-bottom: 0;
	}
	.grid {
		margin-bottom: var(--gap);
	}
	section p {
		margin: 0;
	}
	.findings {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.findings li {
		display: flex;
		align-items: baseline;
		gap: 10px;
		overflow-wrap: anywhere;
	}
	.row {
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding-top: 12px;
		border-top: 1px solid var(--panel-border);
	}
	.main {
		display: flex;
		flex-direction: column;
		gap: 3px;
		min-width: 0;
	}
	.name {
		font-weight: 600;
	}
	.mono {
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.note {
		font-size: var(--text-base);
	}
	.sub {
		margin: 6px 0 0;
		font-size: var(--text-md);
	}
</style>
