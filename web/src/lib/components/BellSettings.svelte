<script lang="ts">
	// Which messages this account's bell shows. The NAS keeps all of them;
	// mail and push follow the shared settings.
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { bell } from '#lib/notifications.svelte.ts';
	import { bellGroups, sanitizeBell } from '#lib/notify-groups.ts';

	let draft = $state(sanitizeBell(bell.settings));
	let error = $state('');
	let saved = $state(false);
	let busy = $state(false);

	async function save() {
		busy = true;
		error = '';
		saved = false;
		try {
			const value = sanitizeBell(draft);
			await api.put('/preferences/bell', value);
			bell.settings = value;
			saved = true;
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			busy = false;
		}
	}
</script>

<section class="tile bell" id="bell" aria-labelledby="bell-title">
	<h2 class="tile-title" id="bell-title">{t('bell.title')}</h2>
	<p class="muted">{t('bell.intro')}</p>
	<label class="narrow"><span>{t('bell.minSeverity')}</span>
		<select class="field" bind:value={draft.minSeverity} onchange={() => (saved = false)}>
			<option value="info">{t('notify.min.info')}</option>
			<option value="warning">{t('notify.min.warning')}</option>
			<option value="error">{t('notify.min.error')}</option>
		</select>
	</label>
	<fieldset>
		<legend>{t('bell.groups')}</legend>
		{#each bellGroups as group}
			<label class="check"><input type="checkbox" bind:checked={draft.groups[group]} onchange={() => (saved = false)} /><span>{t(`bell.group.${group}`)}</span></label>
		{/each}
	</fieldset>
	<p class="muted small">{t('bell.storageErrors')}</p>
	<div class="actions">
		{#if saved}<span role="status">{t('notify.saved')}</span>{/if}
		<button class="btn btn-primary" type="button" onclick={save} disabled={busy}>{t('common.save')}</button>
	</div>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
</section>

<style>
	.bell {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: var(--tile-pad);
		margin-block: var(--gap);
		font-size: 0.875rem;
	}
	.bell > p {
		margin: 0;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	.narrow {
		max-width: 320px;
	}
	fieldset {
		display: flex;
		flex-wrap: wrap;
		gap: 10px 24px;
		margin: 0;
		padding: 0;
		border: 0;
	}
	legend {
		margin-bottom: 8px;
		color: var(--muted);
		font-size: 0.8125rem;
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: 0.875rem;
	}
	.check > span {
		color: var(--text);
	}
	input[type='checkbox'] {
		width: 18px;
		height: 18px;
		margin: 0;
		flex: none;
	}
	.small {
		font-size: 0.8125rem;
	}
	.actions {
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 16px;
	}
</style>
