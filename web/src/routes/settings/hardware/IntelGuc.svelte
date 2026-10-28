<script lang="ts">
	// GuC/HuC firmware loading of Intel graphics (Gen9 to Gen11 on i915) and
	// what the kernel loaded. Asked for once when the page opens: the status
	// may read the stick and the kernel log.
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PowerDialog from '#lib/components/PowerDialog.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { activeMode, applicableGPUs, gucModes, hucState, type GucMode, type IntelGPU, type IntelGucStatus } from '#lib/intel-gpu.ts';

	let status = $state<IntelGucStatus | null>(null);
	let error = $state('');
	let mode = $state<GucMode>('auto');
	let busy = $state(false);
	let saved = $state(false);
	let confirming = $state(false);
	let restarting = $state(false);

	function load() {
		api.get<IntelGucStatus>('/hardware/intel-gpu').then(
			(res) => {
				status = res;
				mode = res.mode;
			},
			(err) => (error = errorText(err instanceof ApiError ? err.code : 'internal'))
		);
	}
	$effect(load);

	const applicable = $derived(applicableGPUs(status));
	const changed = $derived(!!status && mode !== status.mode);
	const running = $derived(status ? activeMode(status.active) : 'auto');

	async function save() {
		confirming = false;
		busy = true;
		error = '';
		saved = false;
		try {
			status = await api.put<IntelGucStatus>('/hardware/intel-gpu', { mode });
			mode = status.mode;
			saved = true;
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			busy = false;
		}
	}

	function submit(event: Event) {
		event.preventDefault();
		// GuC submission is the one mode with a risk: ask first, plainly.
		if (mode === 'guc') confirming = true;
		else save();
	}

	// Firmware file names (i915/kbl_guc_70.1.1.bin) may only wrap after a
	// separator, never in the middle of a word or version.
	const fileParts = (file: string) => file.split(/(?<=[/_])/);
	const firmwareText = (gpu: IntelGPU, kind: 'guc' | 'huc') => {
		const fw = gpu[kind];
		if (fw.loaded && fw.version) return t('intelGuc.fwLoaded', { file: fw.loaded, version: fw.version });
		if (fw.state === 'disabled') return t('intelGuc.fwOff');
		if (fw.state) return t('intelGuc.fwState', { state: fw.state });
		return t('intelGuc.fwUnknown');
	};
</script>

{#if status && status.gpus.length > 0}
	<section class="tile panel" aria-labelledby="hw-intel">
		<h2 class="tile-title" id="hw-intel">{t('intelGuc.title')}</h2>
		{#each status.gpus as gpu (gpu.address)}
			<div class="gpu divided">
				<span class="name">{gpu.name ? t('intelGuc.platform', { name: gpu.name, gen: gpu.generation }) : t('intelGuc.otherIntel')}</span>
				<span class="muted mono">{gpu.address} · 8086:{gpu.device} · {gpu.driver ? t('hardware.driver', { driver: gpu.driver }) : t('hardware.noDriver')} · {gpu.renderNode ? `/dev/dri/${gpu.renderNode}` : t('intelGuc.noRender')}</span>
				{#if gpu.driver === 'xe'}
					<span class="muted">{t('intelGuc.xe')}</span>
				{:else if gpu.driver === 'i915' && !gpu.platform}
					<span class="muted">{t('intelGuc.newer')}</span>
				{:else if gpu.platform && gpu.driver !== 'i915'}
					<span class="muted">{t('intelGuc.notI915')}</span>
				{/if}
				{#if gpu.driver === 'i915'}
					<dl>
						<dt>HuC</dt>
						<dd>
							<span class="chip" class:ok={hucState(gpu) === 'loaded'} class:error={hucState(gpu) === 'failed'}>{t(`intelGuc.huc.${hucState(gpu)}`)}</span>
							{#if hucState(gpu) !== 'off'}<span class="muted mono">{firmwareText(gpu, 'huc')}</span>{/if}
						</dd>
						<dt>GuC</dt>
						<dd><span class="muted mono">{firmwareText(gpu, 'guc')}</span></dd>
						{#if gpu.submissionUsed !== null}
							<dt>{t('intelGuc.submission')}</dt>
							<dd>{gpu.submissionUsed ? t('intelGuc.on') : t('intelGuc.off')}</dd>
						{/if}
						{#if gpu.platform}
							<dt>{t('intelGuc.files')}</dt>
							<dd class="files">
								{#each [gpu.huc, gpu.guc] as fw, n (n)}
									<span
										><span class="mono file" title={fw.file}
											>{#each fileParts(fw.file) as part, i (i)}{#if i}<wbr />{/if}{part}{/each}</span
										>
										<span class="muted">{fw.present ? t('intelGuc.present') : t('intelGuc.absent')}</span></span
									>
								{/each}
							</dd>
						{/if}
						<dt>{t('intelGuc.source')}</dt>
						<dd class="muted">{t(`intelGuc.source.${gpu.source || 'none'}`)}</dd>
					</dl>
					{#each gpu.messages as message, i (i)}<p class="muted mono small">{message}</p>{/each}
				{/if}
			</div>
		{/each}

		{#if applicable.length > 0}
			<form class="divided" onsubmit={submit}>
				<fieldset disabled={!status.writable || busy}>
					<legend>{t('intelGuc.setting')}</legend>
					<p class="muted">{t('intelGuc.intro')}</p>
					{#each gucModes as value (value)}
						<label class="check">
							<input type="radio" name="intel-guc" {value} bind:group={mode} onchange={() => (saved = false)} />
							<span><strong>{t(`intelGuc.mode.${value}`)}</strong><br /><span class="muted">{t(`intelGuc.mode.${value}.hint`)}</span></span>
						</label>
					{/each}
					{#if mode === 'guc'}
						<p class="notice-warn" role="status">{applicable.some((gpu) => gpu.submission) ? t('intelGuc.gucWarn') : t('intelGuc.gucWarnGen9')}</p>
					{/if}
					{#if applicable.some((gpu) => !gpu.huc.present) && mode !== 'auto'}
						<p class="notice-warn" role="status">{t('intelGuc.missingFirmware')}</p>
					{/if}
				</fieldset>
				{#if !status.writable}<p class="muted">{t('intelGuc.notWritable')}</p>{/if}
				<p class="muted small">
					{t('intelGuc.running', { mode: running === 'other' ? status.active : t(`intelGuc.mode.${running}`) })}
					{#if status.param}· {t('intelGuc.param', { value: status.param })}{/if}
				</p>
				{#if !status.managed && status.active}<p class="muted small">{t('intelGuc.manual', { value: status.active })}</p>{/if}
				<div class="row-actions">
					<button class="btn" disabled={!status.writable || busy || !changed}>{t('common.save')}</button>
					{#if saved}<span role="status">{t('intelGuc.saved')}</span>{/if}
				</div>
				{#if status.restartNeeded}
					<div class="notice-warn row" role="status">
						<span>{status.deferred ? t('intelGuc.deferred') : t('intelGuc.afterRestart')}</span>
						<button type="button" class="btn" onclick={() => (restarting = true)}>{t('update.restartNow')}</button>
					</div>
				{/if}
				{#if status.problem}<p class="notice-error" role="alert">{status.problem}</p>{/if}
			</form>
		{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	</section>
{:else if error}
	<p class="notice-error" role="alert">{error}</p>
{/if}

{#if confirming}
	<Dialog title={t('intelGuc.confirmTitle')} onclose={() => (confirming = false)}>
		<p>{t('intelGuc.confirmText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (confirming = false)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" onclick={save}>{t('intelGuc.confirmAction')}</button>
		</div>
	</Dialog>
{/if}
{#if restarting}
	<PowerDialog action="restart" onclose={() => (restarting = false)} />
{/if}

<style>
	.gpu {
		display: flex;
		flex-direction: column;
		gap: 4px;
		font-size: 0.875rem;
	}
	.name {
		font-weight: 600;
	}
	.mono {
		overflow-wrap: anywhere;
	}
	dd.files {
		flex-direction: column;
		align-items: flex-start;
		gap: 2px;
	}
	.mono.file {
		overflow-wrap: normal;
		word-break: keep-all;
	}
	.small {
		font-size: 0.75rem;
		margin: 0;
	}
	dl {
		display: grid;
		grid-template-columns: max-content 1fr;
		gap: 4px 14px;
		margin: 6px 0 0;
		font-size: 0.8125rem;
	}
	dt {
		color: var(--muted);
	}
	dd {
		margin: 0;
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		align-items: center;
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	fieldset {
		border: 0;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	legend {
		font-weight: 600;
		padding: 0;
		margin-bottom: 4px;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
		justify-content: space-between;
	}
</style>
