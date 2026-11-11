<script lang="ts">
	// PLAN 79-9/79-13: what the last run has to tell, as grouped entries in
	// the language of the page (codes from state.problems and state.notices)
	// instead of the NAS's English sentence. A NAS before 0.77 sends only the
	// sentence: it is shown as it is.
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		NOTE_PREVIEW,
		legacyWarning,
		noteGroups,
		type NoteGroup,
		type NotesState,
	} from '#lib/backup-notes.ts';

	let {
		state: run,
		id,
		onchoice,
	}: {
		state: NotesState;
		/** Names the row (remembered open or closed per job). */
		id: string;
		/** Opens the job's choice for disks of running VMs; without it no button. */
		onchoice?: () => void;
	} = $props();

	const groups = $derived(noteGroups(run));
	const legacy = $derived(legacyWarning(run));
	const problems = $derived(groups.some((group) => group.kind === 'problem'));
	let all = $state<Record<string, boolean>>({});

	const title = (group: NoteGroup) =>
		t(`backup.note.${group.code}.${group.count === 1 ? 'one' : 'many'}` as MessageKey, {
			count: group.count,
		});
	const key = (group: NoteGroup) => `${group.kind}:${group.code}`;
	const expandable = (group: NoteGroup) => group.paths.length > NOTE_PREVIEW || group.more > 0;
</script>

{#if groups.length}
	<div class="notes">
		<SettingRow
			id="notes-{id}"
			icon={problems ? 'info' : 'check'}
			title={t('backup.notes.done')}
			open={true}
		>
			{#snippet control()}<span class="chip" class:warn={problems} class:info={!problems}
					>{groups.length === 1
						? t('backup.notes.count.one')
						: t('backup.notes.count.many', { count: groups.length })}</span
				>{/snippet}
			{#each groups as group (key(group))}
				<SettingSub title={title(group)}>
					{#snippet description()}
						<p>{t(`backup.note.${group.code}.text` as MessageKey)}</p>
						{#if group.paths.length && !all[key(group)]}
							<p class="mono paths">{group.paths.slice(0, NOTE_PREVIEW).join(', ')}</p>
						{/if}
					{/snippet}
					{#snippet control()}
						{#if group.choice && onchoice}<button type="button" class="btn" onclick={onchoice}
								>{t('backup.notes.choice')}</button
							>{/if}
						{#if expandable(group)}<button
								type="button"
								class="btn"
								aria-expanded={!!all[key(group)]}
								onclick={() => (all[key(group)] = !all[key(group)])}
								>{t(all[key(group)] ? 'backup.notes.less' : 'backup.notes.all')}</button
							>{/if}
					{/snippet}
				</SettingSub>
				{#if all[key(group)]}
					<div class="srow-block">
						<ul class="mono paths">
							{#each group.paths as path (path)}<li>{path}</li>{/each}
						</ul>
						{#if group.more > 0}<p class="muted">
								{t('backup.notes.more', { count: group.more })}
							</p>{/if}
					</div>
				{/if}
			{/each}
		</SettingRow>
	</div>
{:else if legacy}
	<div class="notice-warn">
		{t('backup.notes.legacy')}
		<pre>{legacy}</pre>
	</div>
{/if}

<style>
	.notes {
		min-width: 0;
		font-size: 0.875rem;
	}
	.paths {
		margin: var(--space-1) 0 0;
		font-size: 0.75rem;
		overflow-wrap: anywhere;
	}
	ul.paths {
		padding-left: var(--space-4);
		max-height: 16rem;
		overflow-y: auto;
	}
	p {
		margin: 0;
	}
	pre {
		margin: var(--space-1-5) 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
