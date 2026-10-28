<script lang="ts">
	// Settings › Appearance › App symbol (for the whole NAS): the symbol of
	// TitanOS on the home screen and, outside iOS, in notifications.
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Icon = { kind: 'default' | 'color' | 'image'; color?: string; version: number };
	let icon = $state<Icon | null>(null);
	let kind = $state<Icon['kind']>('default');
	let color = $state('#6cb6ff');
	let busy = $state(false);
	let error = $state('');
	let saved = $state(false);
	let file = $state<HTMLInputElement>();

	function take(next: Icon) {
		icon = next;
		kind = next.kind;
		if (next.color) color = next.color;
	}

	onMount(async () => {
		try {
			take(await api.get<Icon>('/app-icon'));
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	});

	async function send(body: BodyInit, type: string) {
		busy = true;
		error = '';
		saved = false;
		try {
			const res = await fetch('/api/v1/app-icon', { method: 'PUT', body, headers: { 'Content-Type': type }, credentials: 'same-origin' });
			const answer = await res.json().catch(() => ({}));
			if (!res.ok) throw new ApiError(res.status, answer.error?.code ?? 'internal', answer.error?.message ?? '');
			take(answer as Icon);
			saved = true;
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			busy = false;
		}
	}

	function save() {
		if (kind === 'image') {
			file?.click();
			return;
		}
		void send(JSON.stringify(kind === 'color' ? { kind, color } : { kind }), 'application/json');
	}

	function picked(event: Event) {
		const chosen = (event.currentTarget as HTMLInputElement).files?.[0];
		if (chosen) void send(chosen, chosen.type || 'image/png');
		(event.currentTarget as HTMLInputElement).value = '';
	}
</script>

<section class="tile panel" id="appearance-app-icon" aria-labelledby="app-icon-title">
	<h2 class="tile-title" id="app-icon-title">{t('appIcon.title')}</h2>
	<p class="muted">{t('appIcon.intro')}</p>
	<div class="layout">
		{#if icon}<img class="preview" src="/icons/app-192.png?v={icon.version}" alt={t('appIcon.preview')} width="96" height="96" />{/if}
		<div class="choices">
			<label class="check"><input type="radio" name="app-icon" value="default" bind:group={kind} disabled={busy} /><span>{t('appIcon.default')}</span></label>
			<label class="check"
				><input type="radio" name="app-icon" value="color" bind:group={kind} disabled={busy} /><span>{t('appIcon.color')}</span>{#if kind === 'color'}<input
						type="color"
						bind:value={color}
						disabled={busy}
						aria-label={t('appIcon.color')}
					/>{/if}</label
			>
			<label class="check"><input type="radio" name="app-icon" value="image" bind:group={kind} disabled={busy} /><span>{t('appIcon.image')}</span></label>
			{#if kind === 'image'}<p class="muted small">{t('appIcon.imageHint')}</p>{/if}
		</div>
	</div>
	<input bind:this={file} type="file" accept="image/png,image/jpeg,image/webp" hidden onchange={picked} />
	<p class="muted small">{t('appIcon.ios')}</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="row-actions">
		<button type="button" class="btn btn-primary" disabled={busy || !icon} onclick={save}>{t(kind === 'image' ? 'appIcon.pick' : 'common.save')}</button>
		{#if busy || saved}<span class="muted" role="status">{t(busy ? 'ui.saving' : 'ui.saved')}</span>{/if}
	</div>
</section>

<style>
	.layout {
		display: flex;
		gap: 20px;
		align-items: flex-start;
		flex-wrap: wrap;
	}
	.preview {
		border-radius: 22%;
		flex: none;
	}
	.choices {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	input[type='color'] {
		width: 2.5rem;
		height: 1.75rem;
		padding: 0;
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		background: none;
	}
</style>
