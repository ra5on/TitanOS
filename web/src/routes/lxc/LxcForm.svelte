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
	<!-- PLAN 72: cards with rows like every settings card (label and hint left, control right). -->
	<form class="form" onsubmit={submit}>
		<section class="tile ui-card">
			<div class="set-row">
				<label class="set-label" for="lxc-name"
					>{t('lxc.form.name')}<small class="set-hint mono"
						>{t('lxc.form.id', { id: identifier })}</small
					></label
				>
				<span class="set-control"
					><input id="lxc-name" class="field" bind:value={label} maxlength="80" required /></span
				>
			</div>
			<div class="set-row">
				<label class="set-label" for="lxc-description">{t('lxc.form.description')}</label>
				<span class="set-control"
					><input
						id="lxc-description"
						class="field"
						bind:value={description}
						maxlength="2000"
					/></span
				>
			</div>
		</section>

		<section class="tile ui-card">
			<div class="card-head"><h3>{t('lxc.form.image')}</h3></div>
			{#if initial}
				<p class="set-row wide mono">
					{initial.distribution}
					{initial.release} · {initial.arch} · {initial.volume}
				</p>
			{:else}
				{#if images === null}<p class="set-row wide muted" role="status">
						{t('lxc.form.imagesLoading')}
					</p>{/if}
				{#if imagesError}<div class="set-row wide">
						<p class="notice-warn" role="alert">
							{t('lxc.form.imagesError')} <span class="mono">{imagesError}</span>
						</p>
					</div>{/if}
				<div class="set-row">
					<label class="set-label" for="lxc-distribution"
						>{t('lxc.form.distribution')}<span class="set-hint">{t('lxc.form.imageSource')}</span
						></label
					>
					<span class="set-control"
						><select
							id="lxc-distribution"
							class="field"
							bind:value={distribution}
							onchange={pickRelease}
							disabled={!images?.length}
						>
							{#each groups as group (group.distribution)}<option value={group.distribution}
									>{group.distribution}</option
								>{/each}
						</select></span
					>
				</div>
				<div class="set-row">
					<label class="set-label" for="lxc-release">{t('lxc.form.release')}</label>
					<span class="set-control"
						><select id="lxc-release" class="field" bind:value={release} disabled={!images?.length}>
							{#each releases as name (name)}<option value={name}>{name}</option>{/each}
						</select></span
					>
				</div>
				<div class="set-row">
					<label class="set-label" for="lxc-volume">{t('lxc.form.volume')}</label>
					<span class="set-control"
						><select
							id="lxc-volume"
							class="field"
							bind:value={volume}
							disabled={volumes.length === 0}
						>
							{#each volumes as v (v.name)}<option value={v.name}>{v.name}</option>{/each}
						</select></span
					>
				</div>
				{#if storage.status && volumes.length === 0}<div class="set-row wide">
						<p class="notice-warn" role="status">{t('lxc.form.noVolume')}</p>
					</div>{/if}
			{/if}
		</section>

		<section class="tile ui-card">
			<div class="card-head"><h3>{t('lxc.form.resources')}</h3></div>
			<div class="set-row">
				<label class="set-label" for="lxc-cpus">{t('lxc.form.cpus')}</label>
				<span class="set-control"
					><input
						id="lxc-cpus"
						class="field short"
						type="number"
						min="0"
						max={list.host.cpus}
						bind:value={cpus}
					/></span
				>
			</div>
			<div class="set-row">
				<label class="set-label" for="lxc-memory">{t('lxc.form.memory')}</label>
				<span class="set-control"
					><input
						id="lxc-memory"
						class="field short"
						type="number"
						min="0"
						step="1"
						bind:value={() => mibToMB(memoryMiB), (mb) => (memoryMiB = mbToMiB(mb))}
					/></span
				>
			</div>
		</section>

		<section class="tile ui-card">
			<div class="card-head"><h3>{t('lxc.form.network')}</h3></div>
			<div class="set-row">
				<label class="set-label" for="lxc-network">{t('lxc.form.network')}</label>
				<span class="set-control"
					><select id="lxc-network" class="field" bind:value={network}>
						<option value="none">{t('lxc.net.none')}</option>
						{#if list.bridge || network === 'bridge'}<option value="bridge"
								>{t('lxc.net.bridge')}</option
							>{/if}
						{#each bridges as n (n.id)}<option value={`network:${n.id}`}
								>{n.name}{n.vlan ? ` · VLAN ${n.vlan}` : ''}</option
							>{/each}
					</select></span
				>
			</div>
			<div class="set-row">
				<label class="set-label" for="lxc-mac">{t('lxc.form.mac')}</label>
				<span class="set-control"
					><input
						id="lxc-mac"
						class="field mono"
						bind:value={mac}
						placeholder="00:16:3e:…"
						aria-invalid={!validMAC(mac)}
					/></span
				>
			</div>
			{#if !validMAC(mac)}<div class="set-row wide">
					<p class="notice-error" role="alert">{t('lxc.form.badMac')}</p>
				</div>{/if}
		</section>

		<section class="tile ui-card">
			<div class="card-head">
				<h3>{t('lxc.form.mounts')}</h3>
				<span class="card-tools"
					><button type="button" class="btn" onclick={addMount} disabled={mounts.length >= 16}
						>{t('lxc.form.addMount')}</button
					></span
				>
			</div>
			{#each mounts as mount, index (index)}
				<div class="set-row wide mount">
					<label class="stack">
						<span>{t('lxc.form.mountPath')}</span>
						<input
							class="field mono"
							list="lxc-shares"
							bind:value={mount.path}
							placeholder="/mnt/…"
						/>
					</label>
					<label class="stack">
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
			{#if invalidMounts && mounts.length > 0}<div class="set-row wide">
					<p class="notice-error" role="alert">{t('lxc.form.badTarget')}</p>
				</div>{/if}
			<p class="set-row wide muted small">{t('lxc.form.mountHint')}</p>
		</section>

		<section class="tile ui-card">
			<div class="set-row">
				<label class="set-label" for="lxc-autostart">{t('lxc.form.autostart')}</label>
				<span class="set-control end"
					><input
						id="lxc-autostart"
						type="checkbox"
						class="switch"
						bind:checked={autostart}
					/></span
				>
			</div>
			{#if initial}
				<div class="set-row wide">
					{#if initial.privileged}<p class="notice-warn">{t('lxc.form.privilegedWarning')}</p>{/if}
					<p class="muted small">{t('lxc.form.restartHint')}</p>
				</div>
			{:else}
				<div class="set-row">
					<label class="set-label" for="lxc-privileged">{t('lxc.form.privileged')}</label>
					<span class="set-control end"
						><input
							id="lxc-privileged"
							type="checkbox"
							class="switch"
							bind:checked={privileged}
						/></span
					>
				</div>
				{#if privileged}<div class="set-row wide">
						<p class="notice-warn" role="alert">{t('lxc.form.privilegedWarning')}</p>
					</div>{/if}
			{/if}
		</section>

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
		gap: var(--gap, 16px);
	}
	.tile.ui-card {
		flex-shrink: 0;
	}
	.set-row.wide {
		grid-template-columns: minmax(0, 1fr);
		margin: 0;
	}
	.set-control .field {
		width: 18rem;
		max-width: 100%;
	}
	.set-control .field.short {
		width: 8rem;
	}
	label.stack {
		display: grid;
		gap: var(--space-1);
		font-size: var(--text-base);
	}
	label.check {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--text-md);
	}
	.set-row.mount {
		grid-template-columns: 2fr 1.5fr auto auto;
		align-items: end;
	}
	@media (max-width: 760px) {
		.set-row.mount {
			grid-template-columns: 1fr;
		}
	}
	.small {
		font-size: var(--text-base);
	}
	p {
		margin: 0;
	}
</style>
