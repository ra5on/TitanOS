<script lang="ts">
	// Name of the NAS and its time zone. A new name cannot cut the
	// connection, so it applies at once and is not part of the draft.
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	let {
		hostname,
		timezone,
		disabled,
		onsaved,
	}: { hostname: string; timezone: string; disabled: boolean; onsaved: (name: string) => void } =
		$props();

	let value = $state(''),
		edited = $state(false),
		busy = $state(false),
		error = $state(''),
		saved = $state(false);
	$effect(() => {
		if (!edited) value = hostname;
	});
	const valid = $derived(/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(value));
	let timer: ReturnType<typeof setTimeout> | undefined;

	async function save(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			await api.put('/system/hostname', { hostname: value });
			edited = false;
			saved = true;
			clearTimeout(timer);
			timer = setTimeout(() => (saved = false), 5000);
			onsaved(value);
		} catch (e) {
			error = apiErrorText(e, { detail: true });
		} finally {
			busy = false;
		}
	}
</script>

<section class="tile panel" aria-labelledby="net-system">
	<h2 class="tile-title" id="net-system">{t('netx.system')}</h2>
	<form class="row" onsubmit={save}>
		<label class="stack"
			><span>{t('netcfg.hostname')}</span><input
				class="field mono"
				type="text"
				maxlength="63"
				autocapitalize="none"
				spellcheck="false"
				required
				bind:value
				oninput={() => {
					edited = true;
					saved = false;
				}}
				{disabled}
			/><small class="muted">{t('netcfg.hostnameHint')}</small></label
		>
		<div class="row-actions">
			<button
				class="btn btn-primary"
				disabled={disabled || busy || !edited || !valid || value === hostname}
				>{t('netx.saveHostname')}</button
			>
			{#if saved}<span class="chip ok" role="status">{t('netx.hostnameSaved')}</span>{/if}
		</div>
	</form>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="divided zone">
		<span class="muted">{t('netx.timezone')}</span>
		<span class="mono">{timezone || '–'}</span>
		<a href="/settings/timezone">{t('netx.timezoneChange')}</a>
	</div>
</section>

<style>
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: 10px 16px;
	}
	.row .stack {
		flex: 1 1 18rem;
	}
	.row .row-actions {
		padding-bottom: 22px;
	}
	small {
		font-size: 0.75rem;
	}
	.zone {
		display: flex;
		flex-wrap: wrap;
		gap: 6px 12px;
		align-items: baseline;
		font-size: 0.875rem;
	}
	@media (max-width: 760px) {
		.row .row-actions {
			padding-bottom: 0;
		}
	}
</style>
