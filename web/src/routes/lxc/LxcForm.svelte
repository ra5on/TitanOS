<script lang="ts">
	// Creating a system container, or changing one. What is fixed at
	// creation (image, volume, privileged) is only shown when editing.
	import Dialog from '#lib/components/Dialog.svelte';
	import { api, ApiError, type Share } from '#lib/api.ts';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
	import { mbToMiB, mibToMB } from '#lib/format.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import {
		containerLabel,
		defaultImage,
		groupImages,
		newContainerIdentifier,
		validMAC,
		validMountTarget,
		type LXCImage,
		type LXCList,
		type LXCMount,
		type LXCStatus,
	} from '#lib/lxc.ts';
	import { validVMDisplayName } from '#lib/vm-name.ts';

	type Props = {
		list: LXCList;
		existing?: LXCStatus;
		onclose: () => void;
		ondone: (name: string) => void;
	};
	let { list, existing, onclose, ondone }: Props = $props();

	// The form starts from what is stored; it is not meant to follow later changes.
	// Props are read once on purpose: the draft starts from them and later
	// changes of the props must not overwrite what is being edited; the page mounts this form anew for each opening ({#key form}).
	// svelte-ignore state_referenced_locally
	const initial = existing;
	let label = $state(initial ? containerLabel(initial) : '');
	let description = $state(initial?.description ?? '');
	let distribution = $state(initial?.distribution ?? '');
	let release = $state(initial?.release ?? '');
	let volume = $state(initial?.volume ?? '');
	let cpus = $state(initial?.cpus ?? 0);
	let memoryMiB = $state(initial?.memoryMiB ?? 0);
	let network = $state(initial?.network ?? 'none');
	let mac = $state(initial?.mac ?? '');
	let mounts = $state<LXCMount[]>((initial?.mounts ?? []).map((mount) => ({ ...mount })));
	let autostart = $state(initial?.autostart ?? false);
	let privileged = $state(initial?.privileged ?? false);
	let images = $state<LXCImage[] | null>(null);
	let imagesError = $state('');
	let shares = $state<Share[]>([]);
	let busy = $state(false);
	let error = $state('');

	const volumes = $derived((storage.status?.volumes ?? []).filter((v) => v.mounted));
	const groups = $derived(groupImages(images ?? []));
	const releases = $derived(
		groups.find((group) => group.distribution === distribution)?.releases ?? [],
	);
	const identifier = $derived(
		initial?.name ??
			newContainerIdentifier(
				label,
				list.containers.map((c) => c.name),
			),
	);
	const bridges = $derived((list.networks ?? []).filter((n) => n.bridge));
	const invalidMounts = $derived(
		mounts.some((mount) => !mount.path || !validMountTarget(mount.target)) ||
			new Set(mounts.map((m) => m.target)).size !== mounts.length,
	);
	const valid = $derived(
		validVMDisplayName(label) &&
			validMAC(mac) &&
			!invalidMounts &&
			cpus >= 0 &&
			cpus <= list.host.cpus &&
			(memoryMiB === 0 || memoryMiB >= 64) &&
			(!!initial || (!!distribution && !!release && !!volume)),
	);

	$effect(() => {
		if (!initial) {
			api.get<{ images: LXCImage[]; error: string }>('/lxc/images').then(
				(res) => {
					images = res.images;
					imagesError = res.error;
					if (!distribution) ({ distribution, release } = defaultImage(res.images));
				},
				(err) => {
					images = [];
					imagesError = apiErrorText(err);
				},
			);
		}
		api.get<Share[]>('/shares').then(
			(res) => (shares = res.filter((share) => share.available)),
			() => (shares = []),
		);
	});
	$effect(() => {
		if (!volume && volumes[0]) volume = volumes[0].name;
	});

	function pickRelease() {
		if (!releases.includes(release)) release = releases[0] ?? '';
	}

	function addMount() {
		const share = shares[0];
		mounts.push({
			path: share ? share.path : '',
			target: share ? `/mnt/${share.name.toLowerCase().replace(/[^a-z0-9._-]/g, '-')}` : '/mnt/',
			readOnly: false,
		});
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (!valid || busy) return;
		busy = true;
		error = '';
		const name = identifier;
		const body = {
			displayName: label.trim() === name ? '' : label.trim(),
			description,
			cpus,
			memoryMiB,
			network,
			mac: mac.trim(),
			mounts,
			autostart,
		};
		try {
			if (initial) await api.put(`/lxc/${encodeURIComponent(name)}`, body);
			else await api.post('/lxc', { ...body, name, volume, distribution, release, privileged });
			ondone(name);
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			error = code === 'lxc_failed' && err instanceof ApiError ? err.message : errorText(code);
		} finally {
			busy = false;
		}
	}
</script>

<Dialog
	title={t(initial ? 'lxc.form.editTitle' : 'lxc.form.createTitle')}
	wide
	locked={busy}
	{onclose}
>
	<form class="form" onsubmit={submit}>
		<label>
			<span>{t('lxc.form.name')}</span>
			<input class="field" bind:value={label} maxlength="80" required />
			<small class="muted mono">{t('lxc.form.id', { id: identifier })}</small>
		</label>
		<label>
			<span>{t('lxc.form.description')}</span>
			<input class="field" bind:value={description} maxlength="2000" />
		</label>

		<fieldset>
			<legend>{t('lxc.form.image')}</legend>
			{#if initial}
				<p class="mono">
					{initial.distribution}
					{initial.release} · {initial.arch} · {initial.volume}
				</p>
			{:else}
				{#if images === null}<p class="muted" role="status">{t('lxc.form.imagesLoading')}</p>{/if}
				{#if imagesError}<p class="notice-warn" role="alert">
						{t('lxc.form.imagesError')} <span class="mono">{imagesError}</span>
					</p>{/if}
				<div class="row">
					<label>
						<span>{t('lxc.form.distribution')}</span>
						<select
							class="field"
							bind:value={distribution}
							onchange={pickRelease}
							disabled={!images?.length}
						>
							{#each groups as group (group.distribution)}<option value={group.distribution}
									>{group.distribution}</option
								>{/each}
						</select>
					</label>
					<label>
						<span>{t('lxc.form.release')}</span>
						<select class="field" bind:value={release} disabled={!images?.length}>
							{#each releases as name (name)}<option value={name}>{name}</option>{/each}
						</select>
					</label>
					<label>
						<span>{t('lxc.form.volume')}</span>
						<select class="field" bind:value={volume} disabled={volumes.length === 0}>
							{#each volumes as v (v.name)}<option value={v.name}>{v.name}</option>{/each}
						</select>
					</label>
				</div>
				{#if storage.status && volumes.length === 0}<p class="notice-warn" role="status">
						{t('lxc.form.noVolume')}
					</p>{/if}
				<p class="muted small">{t('lxc.form.imageSource')}</p>
			{/if}
		</fieldset>

		<fieldset>
			<legend>{t('lxc.form.resources')}</legend>
			<div class="row">
				<label>
					<span>{t('lxc.form.cpus')}</span>
					<input class="field" type="number" min="0" max={list.host.cpus} bind:value={cpus} />
				</label>
				<label>
					<span>{t('lxc.form.memory')}</span>
					<input
						class="field"
						type="number"
						min="0"
						step="1"
						bind:value={() => mibToMB(memoryMiB), (mb) => (memoryMiB = mbToMiB(mb))}
					/>
				</label>
			</div>
		</fieldset>

		<fieldset>
			<legend>{t('lxc.form.network')}</legend>
			<div class="row">
				<label>
					<span>{t('lxc.form.network')}</span>
					<select class="field" bind:value={network}>
						<option value="none">{t('lxc.net.none')}</option>
						{#if list.bridge || network === 'bridge'}<option value="bridge"
								>{t('lxc.net.bridge')}</option
							>{/if}
						{#each bridges as n (n.id)}<option value={`network:${n.id}`}
								>{n.name}{n.vlan ? ` · VLAN ${n.vlan}` : ''}</option
							>{/each}
					</select>
				</label>
				<label>
					<span>{t('lxc.form.mac')}</span>
					<input
						class="field mono"
						bind:value={mac}
						placeholder="00:16:3e:…"
						aria-invalid={!validMAC(mac)}
					/>
				</label>
			</div>
			{#if !validMAC(mac)}<p class="notice-error" role="alert">{t('lxc.form.badMac')}</p>{/if}
		</fieldset>

		<fieldset>
			<legend>{t('lxc.form.mounts')}</legend>
			{#each mounts as mount, index (index)}
				<div class="mount">
					<label>
						<span>{t('lxc.form.mountPath')}</span>
						<input
							class="field mono"
							list="lxc-shares"
							bind:value={mount.path}
							placeholder="/mnt/…"
						/>
					</label>
					<label>
						<span>{t('lxc.form.mountTarget')}</span>
						<input
							class="field mono"
							bind:value={mount.target}
							aria-invalid={!validMountTarget(mount.target)}
						/>
					</label>
					<label class="check"
						><input type="checkbox" bind:checked={mount.readOnly} />{t(
							'lxc.form.mountReadOnly',
						)}</label
					>
					<button
						type="button"
						class="btn"
						aria-label={t('lxc.form.removeMount')}
						onclick={() => mounts.splice(index, 1)}>✕</button
					>
				</div>
			{/each}
			<datalist id="lxc-shares">
				{#each shares as share (share.name)}<option value={share.path}>{share.name}</option>{/each}
			</datalist>
			{#if invalidMounts && mounts.length > 0}<p class="notice-error" role="alert">
					{t('lxc.form.badTarget')}
				</p>{/if}
			<div>
				<button type="button" class="btn" onclick={addMount} disabled={mounts.length >= 16}
					>{t('lxc.form.addMount')}</button
				>
			</div>
			<p class="muted small">{t('lxc.form.mountHint')}</p>
		</fieldset>

		<label class="check"
			><input type="checkbox" bind:checked={autostart} />{t('lxc.form.autostart')}</label
		>
		{#if initial}
			{#if initial.privileged}<p class="notice-warn">{t('lxc.form.privilegedWarning')}</p>{/if}
			<p class="muted small">{t('lxc.form.restartHint')}</p>
		{:else}
			<label class="check"
				><input type="checkbox" bind:checked={privileged} />{t('lxc.form.privileged')}</label
			>
			{#if privileged}<p class="notice-warn" role="alert">{t('lxc.form.privilegedWarning')}</p>{/if}
		{/if}

		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={onclose}
				>{t('common.cancel')}</button
			>
			<button class="btn btn-primary" disabled={!valid || busy}
				>{t(initial ? 'lxc.form.save' : 'lxc.form.createButton')}</button
			>
		</div>
	</form>
</Dialog>

<style>
	.form {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	label {
		display: grid;
		gap: 4px;
		font-size: 0.8125rem;
	}
	label.check {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 0.875rem;
	}
	fieldset {
		display: flex;
		flex-direction: column;
		gap: 10px;
		margin: 0;
		padding: 12px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	legend {
		padding: 0 6px;
		font-size: 0.8125rem;
		font-weight: 600;
	}
	.row {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
		gap: 10px;
	}
	.mount {
		display: grid;
		grid-template-columns: 2fr 1.5fr auto auto;
		align-items: end;
		gap: 8px;
	}
	@media (max-width: 760px) {
		.mount {
			grid-template-columns: 1fr;
		}
	}
	.small {
		margin: 0;
		font-size: 0.8125rem;
	}
	p {
		margin: 0;
	}
</style>
