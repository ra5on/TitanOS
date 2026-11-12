<script lang="ts">
	// PLAN 79-7: the cameras – one row each with its state; switch on and off,
	// order, change (the same rows as "Kamera hinzufügen") and remove after a
	// plain question that says what happens to the recordings.
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api } from '#lib/api.ts';
	import { apiPath } from '#lib/cluster-node.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import CameraForm from '#lib/nvr/CameraForm.svelte';
	import Confirm from '#lib/nvr/Confirm.svelte';
	import { nvr, refreshNvr, snapshotPath, unixOf } from '#lib/nvr/nvr.svelte.ts';
	import { cameraCondition, groupsOf, sinceText } from '#lib/nvr/texts.ts';
	import type { Camera } from '#lib/nvr/types.ts';

	const locale = $derived(i18n.locale === 'de' ? 'de-DE' : 'en-GB');
	const running = $derived(nvr.status?.state === 'running');
	const adding = $derived(page.url.searchParams.get('new') === '1');
	const editId = $derived(page.url.searchParams.get('edit') ?? '');
	const editing = $derived(nvr.cameras.find((c) => c.id === editId));
	const many = $derived(nvr.cameras.filter((c) => c.enabled).length > 4);

	let busy = $state('');
	let error = $state('');
	let removing = $state<Camera | null>(null);
	// One still picture per visit: the list is no live view.
	const tick = Date.now();

	async function change(camera: Camera, body: Record<string, unknown>) {
		busy = camera.id;
		error = '';
		try {
			await api.put(`/nvr/cameras/${encodeURIComponent(camera.id)}`, body);
			await refreshNvr();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = '';
		}
	}
	async function move(index: number, by: number) {
		const list = [...nvr.cameras];
		const to = index + by;
		if (to < 0 || to >= list.length) return;
		[list[index], list[to]] = [list[to], list[index]];
		busy = list[to].id;
		error = '';
		try {
			// Every camera gets its place: equal or missing orders sort themselves out.
			for (const [i, camera] of list.entries())
				if (camera.order !== i + 1)
					await api.put(`/nvr/cameras/${encodeURIComponent(camera.id)}`, { order: i + 1 });
			await refreshNvr();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = '';
		}
	}
	async function remove() {
		if (!removing) return;
		busy = removing.id;
		error = '';
		try {
			await api.delete(`/nvr/cameras/${encodeURIComponent(removing.id)}`);
			removing = null;
			await refreshNvr();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = '';
		}
	}
	async function saved() {
		await refreshNvr();
		await goto('/nvr/cameras');
	}

	function summary(camera: Camera): string {
		const parts: string[] = [];
		if (camera.source) parts.push(camera.source.host);
		if (camera.detect)
			parts.push(
				camera.detect.enabled
					? [
							...groupsOf(camera.detect.objects).map((g) => t(`nvr.detect.${g}` as MessageKey)),
							...(camera.detect.plates ? [t('nvr.detect.plates')] : []),
							...(camera.detect.faces ? [t('nvr.detect.faces')] : []),
						].join(', ')
					: t('nvr.detect.nothing'),
			);
		if (camera.record) parts.push(t(`nvr.record.short.${camera.record.mode}` as MessageKey));
		return parts.join(' · ');
	}
	/** A day as the page's language writes it (12.11.2026). */
	function dateText(seconds: number): string {
		return new Date(seconds * 1000).toLocaleDateString(locale, {
			day: '2-digit',
			month: '2-digit',
			year: 'numeric',
		});
	}
	function stateText(camera: Camera): string {
		const condition = cameraCondition(camera, running);
		if (condition === 'offline' && camera.state.offlineSince)
			return t('nvr.camera.offlineSince', { time: sinceText(camera.state.offlineSince, locale) });
		return t(`nvr.camera.state.${condition}` as MessageKey);
	}
</script>

{#if adding}
	<PageHead
		title={t('nvr.add.title')}
		path={[
			{ label: t('desktop.app.nvr'), href: '/nvr' },
			{ label: t('nvr.menu.cameras'), href: '/nvr/cameras' },
		]}
	>
		<p class="muted">{t('nvr.add.intro')}</p>
	</PageHead>
	<CameraForm
		hardware={nvr.status?.hardware}
		objects={nvr.objects}
		taken={nvr.cameras.map((c) => c.name)}
		oncancel={() => goto('/nvr/cameras')}
		onsaved={saved}
	/>
{:else if editId}
	{#if editing}
		<PageHead
			title={editing.name}
			path={[
				{ label: t('desktop.app.nvr'), href: '/nvr' },
				{ label: t('nvr.menu.cameras'), href: '/nvr/cameras' },
			]}
		/>
		{#key editing.id}
			<CameraForm
				camera={editing}
				hardware={nvr.status?.hardware}
				objects={nvr.objects}
				taken={nvr.cameras.filter((c) => c.id !== editing.id).map((c) => c.name)}
				oncancel={() => goto('/nvr/cameras')}
				onsaved={saved}
			/>
		{/key}
	{:else}
		<p class="notice-error" role="alert">{t('error.nvr_camera_not_found')}</p>
		<a class="btn" href="/nvr/cameras">{t('nvr.menu.cameras')}</a>
	{/if}
{:else}
	<PageHead title={t('nvr.menu.cameras')}>
		{#snippet actions()}
			<a class="btn btn-primary" href="/nvr/cameras?new=1"
				><Icon name="plus" size={15} />{t('nvr.add.title')}</a
			>
		{/snippet}
	</PageHead>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if nvr.cameras.length === 0}
		<EmptyState title={t('nvr.live.noneTitle')} text={t('nvr.live.noneAdmin')}>
			<a class="btn btn-primary" href="/nvr/cameras?new=1">{t('nvr.add.title')}</a>
		</EmptyState>
	{:else}
		<SettingGroup
			title={nvr.cameras.length === 1
				? t('nvr.cameras.countOne')
				: t('nvr.cameras.count', { count: nvr.cameras.length })}
			desc={many ? t('nvr.cameras.many') : undefined}
			id="nvr-cameras"
		>
			{#each nvr.cameras as camera, i (camera.id)}
				{@const condition = cameraCondition(camera, running)}
				<div class="srow cam" data-camera={camera.id}>
					<div class="srow-head">
						<span class="thumb" aria-hidden="true"
							>{#if camera.enabled && running}<img
									src={apiPath(snapshotPath(camera.id, 120, tick))}
									alt=""
									loading="lazy"
									onerror={(e) =>
										((e.currentTarget as HTMLImageElement).style.visibility = 'hidden')}
								/>{/if}</span
						>
						<div class="srow-text">
							<span class="srow-title"
								>{camera.name}<span
									class="chip {condition === 'online'
										? 'ok'
										: condition === 'offline'
											? 'error'
											: 'info'}">{stateText(camera)}</span
								></span
							>
							<p class="srow-desc">{summary(camera)}</p>
						</div>
						<div class="srow-ctl">
							<input
								type="checkbox"
								role="switch"
								class="switch"
								aria-label={t('nvr.cameras.enable', { name: camera.name })}
								checked={camera.enabled}
								disabled={busy === camera.id}
								onchange={(e) => change(camera, { enabled: e.currentTarget.checked })}
							/>
							<button
								type="button"
								class="btn icon"
								disabled={i === 0 || busy !== ''}
								aria-label={t('nvr.arrange.up', { name: camera.name })}
								title={t('nvr.cameras.up')}
								onclick={() => move(i, -1)}><Icon name="arrowUp" size={15} /></button
							>
							<button
								type="button"
								class="btn icon"
								disabled={i === nvr.cameras.length - 1 || busy !== ''}
								aria-label={t('nvr.arrange.down', { name: camera.name })}
								title={t('nvr.cameras.down')}
								onclick={() => move(i, 1)}><Icon name="arrowDown" size={15} /></button
							>
							<a class="btn" href="/nvr/cameras?edit={encodeURIComponent(camera.id)}"
								>{t('nvr.cameras.edit')}</a
							>
							<button
								type="button"
								class="btn btn-danger-outline"
								disabled={busy === camera.id}
								onclick={() => (removing = camera)}>{t('nvr.cameras.remove')}</button
							>
						</div>
					</div>
				</div>
			{/each}
		</SettingGroup>
	{/if}
	{#if nvr.removed.length}
		<!-- Removed cameras keep their recordings until these expire; administrators only. -->
		<SettingGroup title={t('nvr.removed.group')} desc={t('nvr.removed.groupHint')} id="nvr-removed">
			{#each nvr.removed as camera (camera.id)}
				{@const until = unixOf(camera.expiresAt)}
				{@const since = unixOf(camera.deletedAt)}
				<div class="srow cam removed" data-camera={camera.id}>
					<div class="srow-head">
						<span class="thumb gone" aria-hidden="true"><Icon name="camera" size={20} /></span>
						<div class="srow-text">
							<span class="srow-title"
								>{camera.name}<span class="chip info"
									>{until
										? t('nvr.removed.until', { date: dateText(until) })
										: t('nvr.removed.untilUnknown')}</span
								></span
							>
							{#if since}<p class="srow-desc">
									{t('nvr.removed.since', { date: dateText(since) })}
								</p>{/if}
						</div>
						<div class="srow-ctl">
							<a class="btn" href="/nvr/recordings?camera={encodeURIComponent(camera.id)}"
								>{t('nvr.removed.show')}</a
							>
						</div>
					</div>
				</div>
			{/each}
		</SettingGroup>
	{/if}
{/if}

{#if removing}
	<Confirm
		title={t('nvr.cameras.removeTitle', { name: removing.name })}
		text={[t('nvr.cameras.removeText'), t('nvr.cameras.removeRights')]}
		confirm={t('nvr.cameras.remove')}
		danger
		busy={busy === removing.id}
		{error}
		onconfirm={remove}
		onclose={() => (removing = null)}
	/>
{/if}

<style>
	.thumb {
		flex: none;
		width: 64px;
		aspect-ratio: 16 / 10;
		border-radius: 7px;
		overflow: hidden;
		background: #0b0f14;
	}
	.thumb.gone {
		display: grid;
		place-items: center;
		background: var(--srow-bg, var(--surface));
		color: var(--muted);
	}
	.thumb img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}
	.cam .chip {
		white-space: normal;
		height: auto;
	}
	.btn.icon {
		display: grid;
		place-items: center;
		width: 32px;
		padding: 0;
	}
	.cam .srow-ctl .btn:not(.icon) {
		display: inline-flex;
		align-items: center;
	}
	:global(.win-toolbar) .btn-primary {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
</style>
