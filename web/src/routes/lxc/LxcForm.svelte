<script lang="ts">
	// Creating a system container, or changing one. What is fixed at
	// creation (image, volume, privileged) is only shown when editing.
	import Dialog from '#lib/components/Dialog.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
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
			error =
				code === 'lxc_failed' && err instanceof ApiError
					? `${errorText(code)} ${err.message}`
					: errorText(code);
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
	<!-- PLAN 78-5: plain headings above flat rows (name and hint left, control
	     right); every row stays open, so each field is part of the form. -->
	<form class="form" onsubmit={submit}>
		<SettingGroup>
			<SettingRow title={t('lxc.form.name')} for="lxc-name">
				{#snippet description()}<span class="mono">{t('lxc.form.id', { id: identifier })}</span
					>{/snippet}
				{#snippet control()}<input
						id="lxc-name"
						class="field"
						bind:value={label}
						maxlength="80"
						required
					/>{/snippet}
			</SettingRow>
			<SettingRow title={t('lxc.form.description')} for="lxc-description">
				{#snippet control()}<input
						id="lxc-description"
						class="field"
						bind:value={description}
						maxlength="2000"
					/>{/snippet}
			</SettingRow>
		</SettingGroup>

		<SettingGroup title={t('lxc.form.image')}>
			{#if initial}
				<SettingRow title={t('lxc.form.image')}>
					{#snippet control()}<span class="mono value"
							>{initial.distribution}
							{initial.release} · {initial.arch} · {initial.volume}</span
						>{/snippet}
				</SettingRow>
			{:else}
				{#if images === null}<p class="muted" role="status">
						{t('lxc.form.imagesLoading')}
					</p>{/if}
				{#if imagesError}
					<p class="notice-warn" role="alert">
						{t('lxc.form.imagesError')} <span class="mono">{imagesError}</span>
					</p>
				{/if}
				<SettingRow
					title={t('lxc.form.distribution')}
					desc={t('lxc.form.imageSource')}
					for="lxc-distribution"
				>
					{#snippet control()}<select
							id="lxc-distribution"
							class="field"
							bind:value={distribution}
							onchange={pickRelease}
							disabled={!images?.length}
						>
							{#each groups as group (group.distribution)}<option value={group.distribution}
									>{group.distribution}</option
								>{/each}
						</select>{/snippet}
				</SettingRow>
				<SettingRow title={t('lxc.form.release')} for="lxc-release">
					{#snippet control()}<select
							id="lxc-release"
							class="field"
							bind:value={release}
							disabled={!images?.length}
						>
							{#each releases as name (name)}<option value={name}>{name}</option>{/each}
						</select>{/snippet}
				</SettingRow>
				<SettingRow title={t('lxc.form.volume')} for="lxc-volume">
					{#snippet control()}<select
							id="lxc-volume"
							class="field"
							bind:value={volume}
							disabled={volumes.length === 0}
						>
							{#each volumes as v (v.name)}<option value={v.name}>{v.name}</option>{/each}
						</select>{/snippet}
				</SettingRow>
				{#if storage.status && volumes.length === 0}
					<p class="notice-warn" role="status">{t('lxc.form.noVolume')}</p>
				{/if}
			{/if}
		</SettingGroup>

		<SettingGroup title={t('lxc.form.resources')}>
			<SettingRow title={t('lxc.form.cpus')} for="lxc-cpus">
				{#snippet control()}<input
						id="lxc-cpus"
						class="field"
						type="number"
						min="0"
						max={list.host.cpus}
						bind:value={cpus}
					/>{/snippet}
			</SettingRow>
			<SettingRow title={t('lxc.form.memory')} for="lxc-memory">
				{#snippet control()}<input
						id="lxc-memory"
						class="field"
						type="number"
						min="0"
						step="1"
						bind:value={() => mibToMB(memoryMiB), (mb) => (memoryMiB = mbToMiB(mb))}
					/>{/snippet}
			</SettingRow>
		</SettingGroup>

		<SettingGroup title={t('lxc.form.network')}>
			<SettingRow title={t('lxc.form.network')} for="lxc-network">
				{#snippet control()}<select id="lxc-network" class="field" bind:value={network}>
						<option value="none">{t('lxc.net.none')}</option>
						{#if list.bridge || network === 'bridge'}<option value="bridge"
								>{t('lxc.net.bridge')}</option
							>{/if}
						{#each bridges as n (n.id)}<option value={`network:${n.id}`}
								>{n.name}{n.vlan ? ` · VLAN ${n.vlan}` : ''}</option
							>{/each}
					</select>{/snippet}
			</SettingRow>
			<SettingRow title={t('lxc.form.mac')} for="lxc-mac">
				{#snippet control()}<input
						id="lxc-mac"
						class="field mono"
						bind:value={mac}
						placeholder="00:16:3e:…"
						aria-invalid={!validMAC(mac)}
					/>{/snippet}
			</SettingRow>
			{#if !validMAC(mac)}
				<p class="notice-error" role="alert">{t('lxc.form.badMac')}</p>
			{/if}
		</SettingGroup>

		<SettingGroup title={t('lxc.form.mounts')}>
			{#snippet tools()}
				<button type="button" class="btn" onclick={addMount} disabled={mounts.length >= 16}
					>{t('lxc.form.addMount')}</button
				>
			{/snippet}
			{#each mounts as mount, index (index)}
				<div class="srow plain">
					<div class="srow-block wide mount">
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
							><input
								type="checkbox"
								class="switch"
								role="switch"
								bind:checked={mount.readOnly}
							/>{t('lxc.form.mountReadOnly')}</label
						>
						<button
							type="button"
							class="btn"
							aria-label={t('lxc.form.removeMount')}
							onclick={() => mounts.splice(index, 1)}>✕</button
						>
					</div>
				</div>
			{/each}
			<datalist id="lxc-shares">
				{#each shares as share (share.name)}<option value={share.path}>{share.name}</option>{/each}
			</datalist>
			{#if invalidMounts && mounts.length > 0}
				<p class="notice-error" role="alert">{t('lxc.form.badTarget')}</p>
			{/if}
			<p class="muted small">{t('lxc.form.mountHint')}</p>
		</SettingGroup>

		<SettingGroup>
			<SettingRow title={t('lxc.form.autostart')} for="lxc-autostart">
				{#snippet control()}<input
						id="lxc-autostart"
						type="checkbox"
						class="switch"
						bind:checked={autostart}
					/>{/snippet}
			</SettingRow>
			{#if initial}
				{#if initial.privileged}<p class="notice-warn">{t('lxc.form.privilegedWarning')}</p>{/if}
				<p class="muted small">{t('lxc.form.restartHint')}</p>
			{:else}
				<SettingRow title={t('lxc.form.privileged')} for="lxc-privileged">
					{#snippet control()}<input
							id="lxc-privileged"
							type="checkbox"
							class="switch"
							bind:checked={privileged}
						/>{/snippet}
				</SettingRow>
				{#if privileged}
					<p class="notice-warn" role="alert">{t('lxc.form.privilegedWarning')}</p>
				{/if}
			{/if}
		</SettingGroup>

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
		gap: var(--gap, var(--space-4));
	}
	.form :global(.sgroup) {
		flex-shrink: 0;
	}
	.value {
		color: var(--text);
		overflow-wrap: anywhere;
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
		min-height: var(--control-height, 2.25rem);
		font-size: var(--text-md);
	}
	.mount {
		display: grid;
		grid-template-columns: 2fr 1.5fr auto auto;
		align-items: end;
	}
	@media (max-width: 760px) {
		.mount {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	.small {
		font-size: var(--text-base);
	}
	p {
		margin: 0;
	}
</style>
