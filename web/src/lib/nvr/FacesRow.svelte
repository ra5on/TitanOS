<script lang="ts">
	// PLAN 79-7: known faces – people with their pictures, and the faces the
	// cameras saw without knowing them, to assign or discard. Pictures of
	// faces are personal data: administrators only (docs/NVR.md).
	import { api, apiFetch, responseError } from '../api.ts';
	import { apiPath } from '../cluster-node.ts';
	import SettingRow from '../components/settings/SettingRow.svelte';
	import { apiErrorText, i18n, t } from '../i18n/index.svelte.ts';
	import Confirm from './Confirm.svelte';
	import { dayClock } from './texts.ts';
	import type { Faces } from './types.ts';

	const locale = $derived(i18n.locale === 'de' ? 'de-DE' : 'en-GB');
	let faces = $state<Faces | null>(null);
	let missing = $state(false);
	let error = $state('');
	let busy = $state(false);
	let open = $state(false);

	async function load() {
		try {
			faces = await api.get<Faces>('/nvr/faces');
			missing = false;
		} catch (err) {
			missing = true;
			faces = null;
			if ((err as { status?: number }).status !== 404) error = apiErrorText(err);
		}
	}
	$effect(() => {
		void load();
	});

	async function run(work: () => Promise<unknown>) {
		busy = true;
		error = '';
		try {
			await work();
			await load();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	const person = (name: string) => `/nvr/faces/${encodeURIComponent(name)}`;

	let newName = $state('');
	function add(event: SubmitEvent) {
		event.preventDefault();
		const name = newName.trim();
		if (!name) return;
		void run(async () => {
			await api.post('/nvr/faces', { name });
			newName = '';
		});
	}
	function upload(name: string, input: HTMLInputElement) {
		const file = input.files?.[0];
		input.value = '';
		if (!file) return;
		void run(async () => {
			const res = await apiFetch(`${person(name)}/images`, {
				method: 'POST',
				body: file,
				headers: { 'Content-Type': file.type || 'image/jpeg' },
			});
			if (!res.ok) throw await responseError(res);
		});
	}
	let renaming = $state('');
	let renameTo = $state('');
	function rename(event: SubmitEvent) {
		event.preventDefault();
		const from = renaming;
		const to = renameTo.trim();
		if (!to || to === from) {
			renaming = '';
			return;
		}
		void run(async () => {
			await api.put(person(from), { name: to });
			renaming = '';
		});
	}
	let removing = $state('');
	let assign = $state<Record<string, string>>({});
	const attempt = (file: string) => `/nvr/faces/attempts/${encodeURIComponent(file)}`;

	const summary = $derived(
		missing
			? t('nvr.faces.missingShort')
			: !faces
				? ''
				: faces.faces.length
					? t('nvr.faces.count', { count: faces.faces.length }) +
						(faces.attempts.length
							? ` · ${t('nvr.faces.unknownCount', { count: faces.attempts.length })}`
							: '')
					: t('nvr.faces.none'),
	);
</script>

<SettingRow
	icon="users"
	title={t('nvr.faces.title')}
	desc={faces && !faces.enabled ? t('nvr.faces.unused') : t('nvr.faces.hint')}
	{summary}
	id="nvr-faces"
	bind:open
>
	<div class="srow-block">
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if missing}
			<p class="muted">{t('nvr.faces.missing')}</p>
		{:else if faces}
			<ul class="people">
				{#each faces.faces as face (face.name)}
					<li data-face={face.name}>
						<div class="who">
							{#if renaming === face.name}
								<form class="inline" onsubmit={rename}>
									<!-- svelte-ignore a11y_autofocus -->
									<input
										class="field"
										type="text"
										maxlength="40"
										bind:value={renameTo}
										aria-label={t('nvr.faces.name')}
										autofocus
									/>
									<button type="submit" class="btn" disabled={busy}>{t('common.save')}</button>
									<button type="button" class="btn" onclick={() => (renaming = '')}
										>{t('common.cancel')}</button
									>
								</form>
							{:else}
								<b>{face.name}</b>
								<small>{t('nvr.faces.pictures', { count: face.images.length })}</small>
							{/if}
						</div>
						<div class="pics">
							{#each face.images as file (file)}
								<span class="pic">
									<img
										src={apiPath(`${person(face.name)}/images/${encodeURIComponent(file)}`)}
										alt={t('nvr.faces.pictureOf', { name: face.name })}
										loading="lazy"
									/>
									<button
										type="button"
										class="x"
										disabled={busy}
										aria-label={t('nvr.faces.removePicture', { name: face.name })}
										title={t('nvr.faces.removePictureShort')}
										onclick={() =>
											run(() =>
												api.delete(`${person(face.name)}/images/${encodeURIComponent(file)}`),
											)}>×</button
									>
								</span>
							{/each}
						</div>
						<div class="acts">
							<label class="btn file"
								>{t('nvr.faces.addPicture')}
								<input
									type="file"
									accept="image/jpeg,image/png,image/webp"
									class="sr-only"
									disabled={busy}
									onchange={(e) => upload(face.name, e.currentTarget)}
								/>
							</label>
							<button
								type="button"
								class="btn"
								onclick={() => ((renaming = face.name), (renameTo = face.name))}
								>{t('nvr.faces.rename')}</button
							>
							<button
								type="button"
								class="btn btn-danger-outline"
								onclick={() => (removing = face.name)}>{t('common.delete')}</button
							>
						</div>
					</li>
				{/each}
			</ul>
			<form class="inline add" onsubmit={add}>
				<input
					class="field"
					type="text"
					maxlength="40"
					bind:value={newName}
					aria-label={t('nvr.faces.name')}
					placeholder={t('nvr.faces.name')}
				/>
				<button type="submit" class="btn" disabled={busy || !newName.trim()}
					>{t('nvr.faces.add')}</button
				>
			</form>
			<p class="muted">{t('nvr.faces.pictureHint')}</p>
			{#if faces.attempts.length}
				<h4>{t('nvr.faces.unknown')}</h4>
				<ul class="attempts">
					{#each faces.attempts as a (a.file)}
						<li>
							<img src={apiPath(attempt(a.file))} alt={t('nvr.faces.unknownOne')} loading="lazy" />
							<small
								>{dayClock(a.at, locale)}{a.guess
									? ` · ${t('nvr.faces.guess', { name: a.guess, percent: Math.round(a.score * 100) })}`
									: ''}</small
							>
							<select
								class="field"
								aria-label={t('nvr.faces.assignTo')}
								value={assign[a.file] ?? a.guess ?? ''}
								onchange={(e) => (assign[a.file] = e.currentTarget.value)}
							>
								<option value="">{t('nvr.faces.assignTo')}</option>
								{#each faces.faces as face (face.name)}<option value={face.name}>{face.name}</option
									>{/each}
							</select>
							<span class="acts">
								<button
									type="button"
									class="btn"
									disabled={busy || !(assign[a.file] ?? a.guess)}
									onclick={() =>
										run(() =>
											api.post(`${attempt(a.file)}/assign`, { name: assign[a.file] ?? a.guess }),
										)}>{t('nvr.faces.assign')}</button
								>
								<button
									type="button"
									class="btn"
									disabled={busy}
									onclick={() => run(() => api.delete(attempt(a.file)))}
									>{t('nvr.faces.discard')}</button
								>
							</span>
						</li>
					{/each}
				</ul>
			{/if}
		{:else}
			<p class="muted" role="status">{t('common.loading')}</p>
		{/if}
	</div>
</SettingRow>

{#if removing}
	<Confirm
		title={t('nvr.faces.deleteTitle', { name: removing })}
		text={t('nvr.faces.deleteText')}
		confirm={t('common.delete')}
		danger
		{busy}
		onconfirm={() => {
			const name = removing;
			removing = '';
			void run(() => api.delete(person(name)));
		}}
		onclose={() => (removing = '')}
	/>
{/if}

<style>
	.people,
	.attempts {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.people li {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding-bottom: var(--space-3);
		border-bottom: 1px solid var(--border);
	}
	.who {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: var(--space-1) var(--space-3);
		overflow-wrap: anywhere;
	}
	.who small,
	.attempts small {
		color: var(--muted);
	}
	.pics {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.pic {
		position: relative;
		width: 64px;
		height: 64px;
	}
	.pic img,
	.attempts img {
		width: 64px;
		height: 64px;
		border-radius: 8px;
		object-fit: cover;
		background: #0b0f14;
		display: block;
	}
	.x {
		position: absolute;
		top: -6px;
		right: -6px;
		width: 22px;
		height: 22px;
		padding: 0;
		border: 1px solid var(--border);
		border-radius: 50%;
		background: var(--surface);
		color: var(--text);
		line-height: 1;
		cursor: pointer;
	}
	.acts,
	.inline {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.inline .field {
		flex: 1 1 10rem;
		min-width: 0;
		max-width: 18rem;
	}
	.add {
		margin: var(--space-3) 0 var(--space-2);
	}
	.file {
		position: relative;
		cursor: pointer;
	}
	.attempts li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
	}
	.attempts select {
		flex: 1 1 9rem;
		min-width: 0;
		max-width: 14rem;
	}
	h4 {
		margin: var(--space-3) 0 var(--space-2);
		font-size: var(--text-base);
		font-weight: 600;
	}
	p {
		margin: 0;
	}
</style>
