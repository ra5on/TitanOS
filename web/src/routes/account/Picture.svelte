<script lang="ts">
	// PLAN 74: My account › picture – what the login page shows in the circle
	// of this account: a photo of its own or one of the built-in pictures.
	// Without a choice the page shows a part of a built-in picture.
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
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
</script>

<div class="picture">
	<span
		class="avatar"
		aria-hidden="true"
		style:background-image={`url("${shown.url}")`}
		style:background-position={shown.position}
		><span class="letter">{[...name][0]?.toUpperCase()}</span></span
	>
	<div class="choose">
		<p class="muted">{t('account.picture.hint')}</p>
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
		<div class="actions">
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
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	</div>
</div>

<style>
	.picture {
		display: flex;
		gap: 20px;
		align-items: flex-start;
		flex-wrap: wrap;
	}
	.avatar {
		position: relative;
		flex: none;
		width: 88px;
		height: 88px;
		border-radius: 50%;
		background: center / cover no-repeat;
		box-shadow: 0 10px 28px -12px rgba(0, 0, 0, 0.6);
	}
	.letter {
		position: absolute;
		inset: 0;
		display: grid;
		place-items: center;
		color: #fff;
		font-weight: 700;
		font-size: 34px;
		text-shadow: 0 1px 8px rgba(0, 0, 0, 0.6);
	}
	.choose {
		flex: 1;
		min-width: 220px;
		display: flex;
		flex-direction: column;
		gap: var(--space-2-5);
	}
	.choose p {
		margin: 0;
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
	.actions {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
	}
	.disabled {
		opacity: 0.6;
		pointer-events: none;
	}
</style>
