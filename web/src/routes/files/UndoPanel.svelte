<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { api, type FileRoot } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	let { refresh, roots, onchanged }: { refresh: number; roots: FileRoot[]; onchanged: () => void } =
		$props();
	type Record = {
		id: string;
		kind: 'rename' | 'trash' | 'move';
		count: number;
		roots: string[];
		busy: boolean;
	};
	let records = $state<Record[]>([]),
		busy = $state(''),
		error = $state('');
	async function load() {
		try {
			records = await api.get<Record[]>('/files/undo');
		} catch {
			/* Mutations still report their own failures. */
		}
	}
	const allowed = (record: Record) =>
		record.roots.every((id) => roots.find((r) => r.id === id)?.writable);
	async function undo(record: Record) {
		if (!allowed(record)) return;
		busy = record.id;
		error = '';
		try {
			await api.post(`/files/undo/${record.id}`);
			onchanged();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			await load();
			busy = '';
		}
	}
	$effect(() => {
		refresh;
		return poll(load, { interval: 5000 });
	});
</script>

{#if records.length || error}<section class="tile undo" aria-label={t('files.undoTitle')}>
		<h2 class="tile-title">{t('files.undoTitle')}</h2>
		<p class="muted">{t('files.undoHint')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#each records as record (record.id)}<div class="record">
				<span>{t(`files.undoKind.${record.kind}`, { count: record.count })}</span><button
					class="btn"
					disabled={!!busy || record.busy || !allowed(record)}
					onclick={() => undo(record)}>{t('files.undo')}</button
				>
			</div>{/each}
	</section>{/if}

<style>
	.undo {
		margin-top: var(--gap);
		padding: var(--tile-pad);
	}
	.record {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 12px;
		padding: 8px 0;
		border-top: 1px solid var(--border);
	}
	p {
		font-size: var(--text-md);
	}
</style>
