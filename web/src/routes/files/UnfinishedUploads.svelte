<script lang="ts">
	import { api, type FileRoot, type UploadStatus } from '#lib/api.ts';
	import { formatBytes } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { recover, uploads } from '#lib/uploads.svelte.ts';

	type Props = {
		/** Partial uploads the server knows in this folder. */
		pending: UploadStatus[];
		root: FileRoot | undefined;
		dir: string;
		/** A partial upload is being finished. */
		recovering: boolean;
		/** After a partial upload was finished or removed. */
		onchanged: () => Promise<void>;
		onerror: (message: string) => void;
	};
	let { pending, root, dir, recovering = $bindable(), onchanged, onerror }: Props = $props();

	let recoveryInput = $state<HTMLInputElement>();
	let recovery = $state<{ status: UploadStatus; root: FileRoot; dir: string } | null>(null);
	// Uploads still in the queue of this browser are not unfinished.
	const unfinished = $derived(
		pending.filter((part) => !uploads.items.some((item) => item.id === part.id)),
	);

	async function recoverPicked(input: HTMLInputElement) {
		const file = input.files?.[0],
			chosen = recovery;
		input.value = '';
		if (!file || !chosen) return;
		recovering = true;
		try {
			await recover(file, chosen.root, chosen.dir, chosen.status);
			await onchanged();
		} catch (err) {
			onerror(apiErrorText(err));
		} finally {
			recovering = false;
			recovery = null;
		}
	}
	async function removePending(part: UploadStatus) {
		try {
			await api.delete(`/files/uploads/${part.id}`);
			await onchanged();
		} catch (err) {
			onerror(apiErrorText(err));
		}
	}
</script>

{#if unfinished.length > 0}
	<section class="unfinished" aria-label={t('uploads.unfinished')}>
		<strong>{t('uploads.unfinished')}</strong>
		<p class="muted">{recovering ? t('uploads.verifying') : t('uploads.recoverHint')}</p>
		<input
			bind:this={recoveryInput}
			type="file"
			hidden
			onchange={(event) => recoverPicked(event.currentTarget)}
		/>
		{#each unfinished as part (part.id)}
			<div class="unfinished-row">
				<span
					>{part.name} ·
					<span class="mono">{formatBytes(part.offset)} / {formatBytes(part.size)}</span></span
				>
				<button
					class="btn"
					disabled={recovering || part.busy}
					onclick={() => {
						if (root) {
							recovery = { status: part, root, dir };
							recoveryInput?.click();
						}
					}}>{t('uploads.resume')}</button
				>
				<button class="btn" disabled={recovering || part.busy} onclick={() => removePending(part)}
					>{t('uploads.removePartial')}</button
				>
			</div>
		{/each}
	</section>
{/if}

<style>
	.unfinished {
		padding: 12px var(--tile-pad);
		border-bottom: 1px solid var(--border);
	}
	.unfinished-row {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 10px;
		margin-top: 10px;
	}
	.unfinished-row > span {
		flex: 1;
		overflow-wrap: anywhere;
	}
</style>
