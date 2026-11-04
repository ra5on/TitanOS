<script lang="ts">
	// Settings › Appearance › App symbol (for the whole NAS): the symbol of
	// TitanOS on the home screen and, outside iOS, in notifications. Saved by
	// the page's one "Apply" bar: it reads `dirty` and calls save()/discard().
	import { onDestroy, onMount } from 'svelte';
	import { api, apiFetch, ApiError } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	let { dirty = $bindable(false) }: { dirty?: boolean } = $props();

	type Icon = { kind: 'default' | 'color' | 'image'; color?: string; version: number };
	let icon = $state<Icon | null>(null);
	let kind = $state<Icon['kind']>('default');
	let color = $state('#6cb6ff');
	let pending = $state<{ file: File; url: string } | null>(null);
	let error = $state('');
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
			error = apiErrorText(err);
		}
	});
	onDestroy(() => {
		if (pending) URL.revokeObjectURL(pending.url);
	});

	// A picked picture counts only once one is chosen.
	$effect(() => {
		dirty =
			icon !== null &&
			(kind === 'image'
				? pending !== null
				: kind !== icon.kind ||
					(kind === 'color' && color.toLowerCase() !== (icon.color ?? '').toLowerCase()));
	});

	async function send(body: BodyInit, type: string) {
		const res = await apiFetch('/app-icon', {
			method: 'PUT',
			body,
			headers: { 'Content-Type': type },
		});
		const answer = await res.json().catch(() => ({}));
		if (!res.ok)
			throw new ApiError(res.status, answer.error?.code ?? 'internal', answer.error?.message ?? '');
		take(answer as Icon);
	}

	/** Saves the choice; throws so that the page can name this area in its message. */
	export async function save() {
		if (!dirty) return;
		error = '';
		try {
			if (kind === 'image' && pending) await send(pending.file, pending.file.type || 'image/png');
			else
				await send(
					JSON.stringify(kind === 'color' ? { kind, color } : { kind }),
					'application/json',
				);
			discardPicture();
		} catch (err) {
			error = apiErrorText(err);
			throw err;
		}
	}

	/** Back to the saved choice. */
	export function discard() {
		discardPicture();
		error = '';
		if (icon) take(icon);
	}

	function discardPicture() {
		if (pending) URL.revokeObjectURL(pending.url);
		pending = null;
	}

	function picked(event: Event) {
		const chosen = (event.currentTarget as HTMLInputElement).files?.[0];
		(event.currentTarget as HTMLInputElement).value = '';
		if (!chosen) return;
		discardPicture();
		pending = { file: chosen, url: URL.createObjectURL(chosen) };
	}
</script>

<section class="tile panel" id="appearance-app-icon" aria-labelledby="app-icon-title">
	<h2 class="tile-title" id="app-icon-title">{t('appIcon.title')}</h2>
	<p class="muted">{t('appIcon.intro')}</p>
	<div class="layout">
		{#if icon}<img
				class="preview"
				src={kind === 'image' && pending ? pending.url : `/icons/app-192.png?v=${icon.version}`}
				alt={t('appIcon.preview')}
				width="96"
				height="96"
			/>{/if}
		<div class="choices">
			<label class="check"
				><input type="radio" name="app-icon" value="default" bind:group={kind} /><span
					>{t('appIcon.default')}</span
				></label
			>
			<label class="check"
				><input type="radio" name="app-icon" value="color" bind:group={kind} /><span
					>{t('appIcon.color')}</span
				>{#if kind === 'color'}<input
						type="color"
						bind:value={color}
						aria-label={t('appIcon.color')}
					/>{/if}</label
			>
			<label class="check"
				><input type="radio" name="app-icon" value="image" bind:group={kind} /><span
					>{t('appIcon.image')}</span
				></label
			>
			{#if kind === 'image'}<p class="muted small">{t('appIcon.imageHint')}</p>
				<div class="row-actions">
					<button type="button" class="btn small" onclick={() => file?.click()}
						>{t('appIcon.pick')}</button
					>{#if pending}<span class="muted small">{pending.file.name}</span>{/if}
				</div>{/if}
		</div>
	</div>
	<input
		bind:this={file}
		type="file"
		accept="image/png,image/jpeg,image/webp"
		hidden
		onchange={picked}
	/>
	<p class="muted small">{t('appIcon.ios')}</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
</section>

<style>
	.layout {
		display: flex;
		gap: 20px;
		align-items: flex-start;
		flex-wrap: wrap;
	}
	.preview {
		object-fit: cover;
		border-radius: 22%;
		flex: none;
	}
	.choices {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
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
