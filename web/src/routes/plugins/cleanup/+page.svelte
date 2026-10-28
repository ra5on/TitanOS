<script lang="ts">
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { formatBytes } from '#lib/format.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	type Release = {
		version: string;
		bytes: number;
		protected: boolean;
		reason?: string;
		fingerprint: string;
	};
	let releases = $state<Release[]>([]),
		selected = $state<string[]>([]),
		loading = $state(true),
		busy = $state(false),
		error = $state(''),
		confirm = $state(false),
		checked = $state(false);
	const total = $derived(
		releases
			.filter((r) => selected.includes(r.version) && !r.protected)
			.reduce((sum, r) => sum + r.bytes, 0),
	);
	async function load() {
		try {
			const res = await api.get<{ releases: Release[] }>('/plugins/releases');
			releases = res.releases;
			selected = [];
		} catch (e) {
			error = errorText(e instanceof ApiError ? e.code : 'internal');
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		void load();
	});
	async function clean() {
		busy = true;
		error = '';
		try {
			await api.post('/plugins/cleanup', {
				selection: releases
					.filter((r) => selected.includes(r.version) && !r.protected)
					.map((r) => ({ version: r.version, fingerprint: r.fingerprint })),
			});
			confirm = false;
			await load();
		} catch (e) {
			error = errorText(e instanceof ApiError ? e.code : 'internal');
			await load();
		} finally {
			busy = false;
		}
	}
</script>

<PageHead title={t('cleanup.title')} parent={{ href: '/plugins', label: t('nav.plugins') }}
	><p class="muted">{t('cleanup.intro')}</p></PageHead
>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if loading}<p class="loading" role="status">{t('common.loading')}</p>{:else if !releases.length}<EmptyState
		title={t('cleanup.empty')}
	/>{:else}<section class="tile content">
		<h2 class="tile-title">{t('cleanup.versions')}</h2>
		{#each releases as release (release.version)}<div class="release">
				<div>
					<strong class="mono">{release.version}</strong>
					<p class="muted">
						{formatBytes(release.bytes)}{release.protected
							? ` · ${t('cleanup.protectedHint')}`
							: ''}
					</p>
				</div>
				{#if release.protected}<span class="chip">{t('cleanup.protected')}</span>{:else}<label
						class="check"
						><input type="checkbox" bind:group={selected} value={release.version} /><span
							>{t('cleanup.select')}</span
						></label
					>{/if}
			</div>{/each}
	</section>
	<p class="muted">{t('cleanup.estimate', { count: selected.length, size: formatBytes(total) })}</p>
	<button
		class="btn btn-primary"
		disabled={!selected.length || busy}
		onclick={() => {
			checked = false;
			confirm = true;
		}}>{t('cleanup.preview')}</button
	>{/if}
<p class="muted">{t('cleanup.safety')}</p>
{#if confirm}<Dialog
		title={t('cleanup.confirmTitle')}
		locked={busy}
		onclose={() => (confirm = false)}
		>{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<p>{t('cleanup.estimate', { count: selected.length, size: formatBytes(total) })}</p>
		<ul>
			{#each releases.filter((r) => selected.includes(r.version)) as r}<li class="mono">
					{r.version} · {formatBytes(r.bytes)}
				</li>{/each}
		</ul>
		<label class="check"
			><input type="checkbox" bind:checked /><span>{t('cleanup.checked')}</span></label
		>
		<div class="dialog-actions">
			<button class="btn" disabled={busy} onclick={() => (confirm = false)}
				>{t('common.cancel')}</button
			><button
				class="btn btn-danger"
				disabled={busy || !checked || !selected.length}
				onclick={clean}>{t('netx.remove')}</button
			>
		</div></Dialog
	>{/if}

<style>
	.content {
		padding: var(--tile-pad);
	}
	.release,
	.dialog-actions {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: var(--gap);
		padding: var(--row-pad) 0;
	}
	.release {
		border-bottom: 1px solid var(--border);
		min-height: 70px;
	}
	.release:last-child {
		border: 0;
	}
	.check {
		display: flex;
		gap: 8px;
		align-items: center;
		min-height: 44px;
	}
	.check input {
		width: 18px;
		height: 18px;
	}
	.dialog-actions {
		justify-content: flex-end;
		margin-top: var(--gap);
	}
</style>
