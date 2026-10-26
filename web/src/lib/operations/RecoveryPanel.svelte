<script lang="ts">
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
		error = $state(''),
		busy = $state(false),
		review = $state<{ kind: 'app' | 'clone'; id: string; volume: string; action: string } | null>(
			null,
		),
		restart = $state(false);
	async function load(details = false) {
		try {
			const recovery = await api.get<{
				operation: Application | null;
				active: boolean;
				error?: string;
			}>('/backups/application-recovery' + (details ? '?details=1' : ''));
			app = recovery.operation;
			active = recovery.active;
			const storage = await api.get<StorageStatus>('/storage');
			const responses = await Promise.allSettled(
				storage.volumes
					.filter((v) => v.mounted)
					.map((v) => api.get<Clone[]>(`/vm-clones/${encodeURIComponent(v.name)}`)),
			);
			clones = responses.flatMap((r) => (r.status === 'fulfilled' ? r.value : []));
			const failed = responses.find((r) => r.status === 'rejected');
			if (failed?.status === 'rejected') throw failed.reason;
			error = recovery.error ?? '';
		} catch (e) {
			error = operationError(e, t);
		}
	}
	onMount(() => {
		load();
		const timer = setInterval(() => {
			if (!document.hidden && !busy && !review) load();
		}, 15000);
		return () => clearInterval(timer);
	});
	async function openApplication() {
		await load(true);
		if (error || !app) return;
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
				await api.post('/backups/application-recovery', { confirm, restart });
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

{#if app || clones.length || error}<section class="tile recovery">
		<div class="head">
			<h2 class="tile-title">{t('ops.review')}</h2>
			<button class="btn" disabled={busy} onclick={() => load()}>{t('ops.refresh')}</button>
		</div>
		{#if error}<p class="notice-warn">{error}</p>{/if}
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
							review = { kind: 'clone', id: op.id, volume: op.vm.volume, action: 'finish' };
						}}>{t('ops.finishClone')}</button
					>{/if}{#if ['copying', 'verified', 'publishing', 'conflict'].includes(op.phase)}<button
						class="btn"
						disabled={busy}
						onclick={() => {
							review = { kind: 'clone', id: op.id, volume: op.vm.volume, action: 'discard' };
						}}>{t('ops.discardClone')}</button
					>{/if}
			</div>{/each}
	</section>{/if}
{#if review}<Dialog title={t('ops.review')} locked={busy} onclose={() => (review = null)}
		><p>{t('ops.reviewHint')}</p>
		{#if review.kind === 'app'}<ul>
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
			><button class="btn btn-warn" disabled={busy} onclick={act}
				>{t(
					review.kind === 'app' && !restart
						? 'ops.ackOnly'
						: review.action === 'discard'
							? 'ops.discardClone'
							: 'ops.review',
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
