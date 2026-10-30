<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { onMount } from 'svelte';
	import { api, type StorageStatus } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { operationError } from '#lib/operations.ts';
	type Clone = {
		id: string;
		source: string;
		vm: { name: string; volume: string };
		phase: string;
		error: string;
	};
	type Application = {
		id: string;
		volume: string;
		job: string;
		members?: { name: string; kind: string; running: boolean; phase: string; error: string }[];
	};
	let clones = $state<Clone[]>([]),
		app = $state<Application | null>(null),
		active = $state(false),
		// The application backup and the VM copies are looked at apart: one
		// volume whose copies cannot be read does not hide the other review.
		appError = $state(''),
		cloneError = $state(''),
		error = $state(''),
		journalMissing = $state(false),
		busy = $state(false),
		review = $state<{
			kind: 'app' | 'clone';
			id: string;
			volume: string;
			action: string;
			vm?: string;
		} | null>(null),
		restart = $state(false);
	async function load(details = false) {
		try {
			const recovery = await api.get<{
				operation: Application | null;
				active: boolean;
				error?: string;
				journalMissing?: boolean;
			}>('/backups/application-recovery' + (details ? '?details=1' : ''));
			app = recovery.operation;
			active = recovery.active;
			journalMissing = !!recovery.journalMissing;
			appError = recovery.error ?? '';
		} catch (e) {
			appError = operationError(e, t);
		}
		try {
			const storage = await api.get<StorageStatus>('/storage');
			const responses = await Promise.allSettled(
				storage.volumes
					.filter((v) => v.mounted)
					.map((v) => api.get<Clone[]>(`/vm-clones/${encodeURIComponent(v.name)}`)),
			);
			clones = responses.flatMap((r) => (r.status === 'fulfilled' ? r.value : []));
			const failed = responses.find((r) => r.status === 'rejected');
			cloneError = failed?.status === 'rejected' ? operationError(failed.reason, t) : '';
		} catch (e) {
			cloneError = operationError(e, t);
		}
	}
	onMount(() => {
		return poll(() => load(), { interval: 15000, skip: () => busy || !!review });
	});
	async function openApplication() {
		await load(true);
		if (appError || !app) return;
		error = '';
		review = { kind: 'app', id: app.id, volume: app.volume, action: 'review' };
		restart = false;
	}
	async function act() {
		if (!review || busy) return;
		// The API still wants the operation ID as confirmation; it is sent once
		// the button is clicked.
		const confirm = review.id;
		busy = true;
		error = '';
		try {
			if (review.kind === 'app')
				await api.post('/backups/application-recovery', {
					confirm,
					restart: restart && !journalMissing,
				});
			else
				await api.post(`/vm-clones/${encodeURIComponent(review.volume)}/${review.id}`, {
					confirm,
					action: review.action,
				});
			review = null;
			await load();
		} catch (e) {
			error = operationError(e, t);
		} finally {
			busy = false;
		}
	}
</script>

{#if app || clones.length || appError || cloneError}<section class="tile recovery">
		<div class="head">
			<h2 class="tile-title">{t('ops.review')}</h2>
			<button class="btn" disabled={busy} onclick={() => load()}>{t('ops.refresh')}</button>
		</div>
		{#if appError}<p class="notice-warn">{appError}</p>{/if}
		{#if cloneError}<p class="notice-warn">{cloneError}</p>{/if}
		{#if app}<div class="row">
				<strong>{t('ops.appRecover')} · {app.job}</strong><span class="mono">{app.id}</span><button
					class="btn"
					disabled={busy || active}
					onclick={openApplication}>{t('ops.review')}</button
				>
			</div>{/if}
		{#each clones as op (op.id)}<div class="row">
				<strong>{t('ops.cloneRecovery')} · {op.vm.name}</strong><span>{op.phase}</span><span
					class="mono">{op.id}</span
				>{#if op.error}<p class="notice-warn">
						{op.error}
					</p>{/if}{#if ['verified', 'publishing', 'conflict'].includes(op.phase)}<button
						class="btn"
						disabled={busy}
						onclick={() => {
							error = '';
							review = {
								kind: 'clone',
								id: op.id,
								volume: op.vm.volume,
								action: 'finish',
								vm: op.vm.name,
							};
						}}>{t('ops.finishClone')}</button
					>{/if}{#if ['copying', 'verified', 'publishing', 'conflict'].includes(op.phase)}<button
						class="btn"
						disabled={busy}
						onclick={() => {
							error = '';
							review = {
								kind: 'clone',
								id: op.id,
								volume: op.vm.volume,
								action: 'discard',
								vm: op.vm.name,
							};
						}}>{t('ops.discardClone')}</button
					>{/if}
			</div>{/each}
	</section>{/if}
{#if review}<Dialog
		title={t(
			review.kind === 'app'
				? 'ops.appRecover'
				: review.action === 'discard'
					? 'ops.discardClone'
					: 'ops.finishClone',
		)}
		locked={busy}
		onclose={() => (review = null)}
		>{#if review.kind === 'clone'}<p>
				{t(review.action === 'discard' ? 'ops.discardCloneHint' : 'ops.finishCloneHint', {
					vm: review.vm ?? '',
				})}
			</p>{:else if journalMissing}<p class="notice-warn">{t('ops.appJournalMissing')}</p>{:else}<p>
				{t('ops.reviewHint')}
			</p>{/if}
		{#if review.kind === 'app' && !journalMissing}<ul>
				{#each app?.members ?? [] as member (`${member.kind}:${member.name}`)}<li>
						<strong>{member.name}</strong> · {member.kind} · {t(
							member.running ? 'tasks.wasRunning' : 'tasks.wasStopped',
						)}
						<div class="mono">{member.phase}</div>
						{#if member.error}<p class="notice-warn">{member.error}</p>{/if}
					</li>{/each}
			</ul>
			<label class="check"
				><input type="checkbox" bind:checked={restart} />{t('ops.restartPrevious')}</label
			>{/if}
		<p class="mono">{review.id}</p>
		{#if error}<p class="notice-error">{error}</p>{/if}
		<div class="dialog-actions">
			<button class="btn" disabled={busy} onclick={() => (review = null)}
				>{t('common.cancel')}</button
			><button
				class="btn {review.kind === 'clone'
					? review.action === 'discard'
						? 'btn-danger'
						: 'btn-primary'
					: 'btn-warn'}"
				disabled={busy}
				onclick={act}
				>{t(
					review.kind === 'app'
						? journalMissing
							? 'ops.forgetPointer'
							: restart
								? 'ops.review'
								: 'ops.ackOnly'
						: review.action === 'discard'
							? 'ops.discardClone'
							: 'ops.finishClone',
				)}</button
			>
		</div></Dialog
	>{/if}

<style>
	.recovery {
		padding: var(--tile-pad);
		margin: var(--gap) 0;
	}
	.row,
	.head {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
		align-items: center;
	}
	.head {
		justify-content: space-between;
	}
	.row {
		padding: 14px 0;
		border-bottom: 1px solid var(--border);
		font-size: 0.85rem;
	}
	.row p {
		flex-basis: 100%;
	}
	.mono {
		overflow-wrap: anywhere;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.85rem;
		margin: 14px 0;
	}
	.check {
		flex-direction: row;
		align-items: center;
	}
</style>
