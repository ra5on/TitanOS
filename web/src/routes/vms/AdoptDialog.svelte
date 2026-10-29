<script lang="ts">
	// "VM übernehmen": lists the copies of VM settings (vm.json) on the mounted
	// volumes whose VM this NAS does not know, and registers one of them again.
	// The volumes are only read when this dialog opens or on "Search again".
	import Dialog from '#lib/components/Dialog.svelte';
	import { api, ApiError, type VMAdoptCandidate, type VMAdoptResult } from '#lib/api.ts';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { errorText, t, tOr } from '#lib/i18n/index.svelte.ts';
	import { onMount } from 'svelte';

	let { onclose, onadopted }: { onclose: () => void; onadopted: (name: string) => void } = $props();

	let candidates = $state<VMAdoptCandidate[] | null>(null);
	let loading = $state(false);
	let loadError = $state('');
	let chosen = $state('');
	let newName = $state('');
	let busy = $state(false);
	let error = $state('');
	let done = $state<VMAdoptResult | null>(null);

	const key = (c: VMAdoptCandidate) => `${c.volume}/${c.directory}`;
	const selected = $derived(candidates?.find((c) => key(c) === chosen));
	const label = (c: VMAdoptCandidate) => c.displayName || c.name;
	const changeText = (code: string) => tOr(`vms.adopt.change.${code}`, code);
	const nameValid = $derived(/^[a-z0-9][a-z0-9-]{0,30}$/.test(newName));
	const needsName = $derived(!!selected?.nameTaken);

	async function load() {
		loading = true;
		loadError = '';
		try {
			const result = await api.get<{ candidates: VMAdoptCandidate[] }>('/vms/adopt');
			candidates = result.candidates;
			const first = candidates.find((c) => c.missing.length === 0);
			if (!selected) choose(first);
		} catch (err) {
			loadError = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			loading = false;
		}
	}

	function choose(c: VMAdoptCandidate | undefined) {
		chosen = c ? key(c) : '';
		error = '';
		// A free name is suggested when the old one is taken.
		newName = c?.nameTaken ? `${c.name.slice(0, 29)}-2` : '';
	}

	async function adopt() {
		if (!selected || (needsName && !nameValid)) return;
		busy = true;
		error = '';
		try {
			done = await api.post<VMAdoptResult>('/vms/adopt', {
				volume: selected.volume,
				directory: selected.directory,
				name: needsName ? newName : '',
			});
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			error =
				code === 'vm_exists'
					? t('vms.adopt.nameTaken', { name: needsName ? newName : selected.name })
					: errorText(code);
			if (code === 'vm_exists' && !needsName && selected) selected.nameTaken = true;
			if (code === 'vm_exists' && selected?.nameTaken && !newName)
				newName = `${selected.name.slice(0, 29)}-2`;
			if (code === 'vm_failed' && err instanceof ApiError) error += ` ${err.message}`;
		} finally {
			busy = false;
		}
	}

	onMount(load);
</script>

<Dialog
	title={t('vms.adopt.title')}
	wide
	locked={busy}
	onclose={() => (done ? onadopted(done.name) : onclose())}
>
	{#if done}
		<p class="text">{t('vms.adopt.done', { name: done.name })}</p>
		{#if done.changes.length}
			<p class="text">{t('vms.adopt.changes')}</p>
			<ul class="changes">
				{#each done.changes as code (code)}<li>{changeText(code)}</li>{/each}
			</ul>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn btn-primary" onclick={() => onadopted(done!.name)}
				>{t('common.done')}</button
			>
		</div>
	{:else}
		<p class="text muted">{t('vms.adopt.hint')}</p>
		{#if loadError}
			<div class="notice-error" role="alert">
				{loadError}
				<button type="button" class="btn small" onclick={load}>{t('common.retry')}</button>
			</div>
		{:else if candidates === null}
			<p class="loading">{t('vms.adopt.loading')}</p>
		{:else if candidates.length === 0}
			<p class="text">{t('vms.adopt.none')}</p>
		{:else}
			<div class="list" role="radiogroup" aria-label={t('vms.adopt.title')}>
				{#each candidates as c (key(c))}
					<label class="row" class:chosen={key(c) === chosen} class:off={c.missing.length > 0}>
						<input
							type="radio"
							name="adopt"
							checked={key(c) === chosen}
							disabled={busy || c.missing.length > 0}
							onchange={() => choose(c)}
						/>
						<span class="body">
							<span class="title"
								><strong>{label(c)}</strong>
								{#if c.displayName}<span class="mono muted">{c.name}</span>{/if}
								{#if c.removed}<span class="chip"
										>{c.removedAt
											? t('vms.adopt.removedAt', { date: formatDateTime(new Date(c.removedAt)) })
											: t('vms.adopt.removed')}</span
									>{/if}
								{#if c.nameTaken}<span class="chip warn">{t('vms.adopt.nameTakenChip')}</span>{/if}
							</span>
							<span class="mono muted">{c.volume}/vms/{c.directory}</span>
							<span class="muted"
								>{t('vms.adopt.specs', {
									vcpus: c.vcpus,
									memory: formatBytes(c.memoryMiB * 1024 * 1024),
									disks: c.disks.length,
								})}</span
							>
							{#if c.missing.length}<span class="notice-warn"
									>{t('vms.adopt.missing', { volumes: c.missing.join(', ') })}</span
								>{/if}
						</span>
					</label>
				{/each}
			</div>
			{#if selected}
				<form
					id="adopt-vm"
					onsubmit={(event) => {
						event.preventDefault();
						adopt();
					}}
				>
					{#if needsName}
						<p class="notice-warn">{t('vms.adopt.nameTaken', { name: selected.name })}</p>
						<label class="stack"
							>{t('vms.adopt.newName')}<input
								class="field mono"
								bind:value={newName}
								maxlength="31"
								disabled={busy}
								required
							/></label
						>
						{#if newName && !nameValid}<p class="notice-error">{t('vms.new.nameInvalid')}</p>{/if}
					{/if}
					<p class="text">
						{t('vms.adopt.confirm', {
							name: needsName ? newName || '…' : selected.name,
							path: `${selected.volume}/vms/${selected.directory}`,
						})}
					</p>
					{#if selected.disks.length}
						<ul class="mono disks">
							{#each selected.disks as disk (disk)}<li>{disk}</li>{/each}
						</ul>
					{/if}
					{#if selected.dropped.length}
						<p class="text">{t('vms.adopt.dropped')}</p>
						<ul class="changes">
							{#each selected.dropped as code (code)}<li>{changeText(code)}</li>{/each}
						</ul>
					{/if}
				</form>
			{/if}
		{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={onclose}
				>{t('common.cancel')}</button
			>
			{#if candidates !== null}
				<button type="button" class="btn" disabled={busy || loading} onclick={load}
					>{t('vms.adopt.search')}</button
				>
			{/if}
			<button
				type="submit"
				form="adopt-vm"
				class="btn btn-primary"
				disabled={busy || !selected || (needsName && !nameValid)}>{t('vms.adopt.submit')}</button
			>
		</div>
	{/if}
</Dialog>

<style>
	.list {
		display: flex;
		flex-direction: column;
		gap: 8px;
		max-height: 40vh;
		overflow: auto;
	}
	.row {
		display: flex;
		gap: 10px;
		align-items: flex-start;
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		cursor: pointer;
	}
	.row.chosen {
		border-color: var(--accent);
	}
	.row.off {
		cursor: default;
		opacity: 0.7;
	}
	.body {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
	}
	.title {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		align-items: center;
	}
	.mono {
		overflow-wrap: anywhere;
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 10px;
		margin-top: 14px;
	}
	.changes,
	.disks {
		margin: 0;
		padding-left: 1.2rem;
		font-size: 0.9rem;
	}
</style>
