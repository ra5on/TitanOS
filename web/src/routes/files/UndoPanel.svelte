<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { api, type FileRoot } from '#lib/api.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
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
	/** PLAN 67-1: Bearbeiten › Rückgängig in the menu bar takes back the newest change. */
	export function canUndo(): boolean {
		const last = records[0];
		return !!last && !busy && !last.busy && allowed(last);
	}
	export function undoLast() {
		if (records[0]) void undo(records[0]);
	}
	$effect(() => {
		refresh;
		return poll(load, { interval: 5000 });
	});
</script>

<!-- PLAN 78-5: a plain heading; every change that can be taken back is a
     flat row with its button. -->
{#if records.length || error}<div class="undo">
		<SettingGroup title={t('files.undoTitle')} desc={t('files.undoHint')} id="files-undo">
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			{#each records as record (record.id)}<SettingRow
					icon="undo"
					title={t(`files.undoKind.${record.kind}`, { count: record.count })}
				>
					{#snippet control()}<button
							class="btn"
							disabled={!!busy || record.busy || !allowed(record)}
							onclick={() => undo(record)}>{t('files.undo')}</button
						>{/snippet}
				</SettingRow>{/each}
		</SettingGroup>
	</div>{/if}

<style>
	.undo {
		margin-top: var(--gap);
	}
</style>
