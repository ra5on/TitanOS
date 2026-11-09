<script lang="ts">
	// "Update all": which apps to update; all with an update start ticked,
	// failed ones stay ticked for another try.
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	let {
		apps,
		selected,
		failed,
		busy,
		onselect,
		onclose,
		onsubmit,
	}: {
		apps: { app: string; names: string[] }[];
		selected: string[];
		failed: { app: string; text: string }[];
		busy: boolean;
		onselect: (selected: string[]) => void;
		onclose: () => void;
		onsubmit: () => void;
	} = $props();
</script>

<Dialog title={t('docker.updatesTitle')} locked={busy} {onclose}>
	<p class="text">{t('docker.updatesText')}</p>
	<div class="update-list">
		{#each apps as entry (entry.app)}
			<label class="check"
				><input
					type="checkbox"
					disabled={busy}
					checked={selected.includes(entry.app)}
					onchange={(event) =>
						onselect(
							event.currentTarget.checked
								? [...selected, entry.app]
								: selected.filter((app) => app !== entry.app),
						)}
				/>
				<span
					><span class="mono">{entry.app}</span
					>{#if entry.names.length > 1 || entry.names[0] !== entry.app}
						<span class="muted">· {entry.names.join(', ')}</span>{/if}</span
				></label
			>
		{:else}<p class="muted">{t('docker.updatesNone')}</p>{/each}
	</div>
	{#if failed.length}<div class="notice-error" role="alert">
			{t('docker.updatesFailed')}
			<ul>
				{#each failed as entry (entry.app)}<li>
						<span class="mono">{entry.app}</span>: {entry.text}
					</li>{/each}
			</ul>
		</div>{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" disabled={busy} onclick={onclose}>{t('common.cancel')}</button
		>
		<button
			type="button"
			class="btn btn-primary"
			disabled={busy || selected.length === 0}
			onclick={onsubmit}
			>{t(busy ? 'docker.updatesRunning' : 'docker.updatesSubmit', {
				count: selected.length,
			})}</button
		>
	</div>
</Dialog>

<style>
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		font-size: var(--text-md);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: var(--space-2-5);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.text {
		font-size: var(--text-md);
	}
	.update-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		max-height: 50vh;
		overflow: auto;
		margin: var(--space-3) 0;
	}
	.notice-error ul {
		margin: var(--space-1-5) 0 0;
		padding-left: 1.1rem;
	}
</style>
