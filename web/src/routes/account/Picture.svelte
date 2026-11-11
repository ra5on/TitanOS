<script lang="ts">
	// PLAN 74: My account › picture – what the login page shows in the circle
	// of this account: a photo of its own or one of the built-in pictures.
	// Without a choice the page shows a part of a built-in picture. A row of
	// "Mein Konto" (PLAN 78-5): the picture at the right, the choice inside.
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { pictureStyle, rememberPicture, type AccountPicture } from '#lib/login/accounts.ts';

	let { name }: { name: string } = $props();
	const BUILTIN = ['nordlicht', 'bergsee', 'kueste', 'nebelwald', 'wueste', 'weltraum'] as const;

	let picture = $state<AccountPicture>({});
	let busy = $state(false);
	let error = $state('');

	onMount(async () => {
		try {
			picture = await api.get<AccountPicture>('/account/picture');
			rememberPicture(name, picture);
		} catch (err) {
			error = apiErrorText(err);
		}
	});

	async function save(body: { builtin: string } | Blob | null) {
		busy = true;
		error = '';
		try {
			if (body === null) {
				await api.delete('/account/picture');
				picture = {};
			} else {
				picture = await api.put<AccountPicture>('/account/picture', body);
			}
			rememberPicture(name, picture);
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	function upload(event: Event & { currentTarget: HTMLInputElement }) {
		const file = event.currentTarget.files?.[0];
		event.currentTarget.value = '';
		if (file) void save(file);
	}
	const shown = $derived(pictureStyle(name, picture, true));
	// What the closed row says: the chosen built-in picture, an own photo or none.
	const summary = $derived(
		picture.photo
			? t('account.picture.own')
			: picture.builtin && (BUILTIN as readonly string[]).includes(picture.builtin)
				? t(`desktop.wallpaper.${picture.builtin as (typeof BUILTIN)[number]}`)
				: t('account.picture.none'),
	);
</script>

<SettingRow
	id="acc-picture"
	icon="image"
	title={t('account.picture.title')}
	desc={t('account.picture.hint')}
	{summary}
>
	{#snippet control()}
		<span
			class="avatar"
			aria-hidden="true"
			style:background-image={`url("${shown.url}")`}
			style:background-position={shown.position}
			><span class="letter">{[...name][0]?.toUpperCase()}</span></span
		>
	{/snippet}
	<div class="srow-block">
		<div class="builtin" role="radiogroup" aria-label={t('account.picture.builtin')}>
			{#each BUILTIN as id (id)}
				<button
					type="button"
					role="radio"
					aria-checked={picture.builtin === id}
					aria-label={t(`desktop.wallpaper.${id}`)}
					title={t(`desktop.wallpaper.${id}`)}
					disabled={busy}
					style:background-image={`url("/backgrounds/${id}.webp")`}
					onclick={() => save({ builtin: id })}
				></button>
			{/each}
		</div>
	</div>
	<div class="srow-block actions">
		<label class="btn" class:disabled={busy}
			>{t('account.picture.upload')}<input
				type="file"
				accept="image/jpeg,image/png,image/webp"
				hidden
				disabled={busy}
				onchange={upload}
			/></label
		>
		{#if picture.builtin || picture.photo}<button
				type="button"
				class="btn"
				disabled={busy}
				onclick={() => save(null)}>{t('account.picture.reset')}</button
			>{/if}
	</div>
	{#if error}<div class="srow-block"><p class="notice-error" role="alert">{error}</p></div>{/if}
</SettingRow>

<style>
	.avatar {
		position: relative;
		flex: none;
		width: 40px;
		height: 40px;
		border-radius: 50%;
		background: center / cover no-repeat;
	}
	.letter {
		position: absolute;
		inset: 0;
		display: grid;
		place-items: center;
		color: #fff;
		font-weight: 700;
		font-size: var(--text-lg);
		text-shadow: 0 1px 8px rgba(0, 0, 0, 0.6);
	}
	.builtin {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
	}
	.builtin button {
		width: 44px;
		height: 44px;
		border-radius: 50%;
		border: 2px solid transparent;
		background: center / cover no-repeat;
		cursor: pointer;
		padding: 0;
	}
	.builtin button[aria-checked='true'] {
		border-color: var(--accent);
		box-shadow: 0 0 0 2px var(--surface);
	}
	.builtin button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.disabled {
		opacity: 0.6;
		pointer-events: none;
	}
</style>
