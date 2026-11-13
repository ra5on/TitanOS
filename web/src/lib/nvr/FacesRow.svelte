<script lang="ts">
	// PLAN 79-7: known faces – people with their pictures, and the faces the
	// cameras saw without knowing them, to assign or discard. Pictures of
	// faces are personal data: administrators only (docs/NVR.md).
	import { api, apiFetch, responseError } from '../api.ts';
	import { apiPath } from '../cluster-node.ts';
	import SettingRow from '../components/settings/SettingRow.svelte';
	import { apiErrorText, i18n, t, type MessageKey } from '../i18n/index.svelte.ts';
	import Confirm from './Confirm.svelte';
	import { dayClock, faceNameProblem } from './texts.ts';
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

	// A change answers with the library as it is now (docs/NVR.md); a 204
	// (deleting) has no body, then the library is read again.
	async function run(work: () => Promise<unknown>) {
		busy = true;
		error = '';
		try {
			const now = await work();
			if (now && typeof now === 'object' && Array.isArray((now as Faces).faces)) {
				faces = now as Faces;
				missing = false;
			} else await load();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	const person = (name: string) => `/nvr/faces/${encodeURIComponent(name)}`;

	// Names follow the NAS's rule; what is wrong is said under the field.
	const problemText = (name: string) => {
		const problem = faceNameProblem(name);
		return problem ? t(`nvr.faces.nameProblem.${problem}` as MessageKey) : '';
	};
	let newName = $state('');
	let newTried = $state(false);
	const newProblem = $derived(newName.trim() || newTried ? problemText(newName) : '');
	function add(event: SubmitEvent) {
		event.preventDefault();
		newTried = true;
		const name = newName.trim();
		if (faceNameProblem(name)) return;
		void run(async () => {
			const now = await api.post<Faces>('/nvr/faces', { name });
			newName = '';
			newTried = false;
			return now;
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
			return res.json().catch(() => null);
		});
	}
	let renaming = $state('');
	let renameTo = $state('');
	const renameProblem = $derived(renaming ? problemText(renameTo) : '');
	function rename(event: SubmitEvent) {
		event.preventDefault();
		const from = renaming;
		const to = renameTo.trim();
		if (to === from) {
			renaming = '';
			return;
		}
		if (faceNameProblem(to)) return;
		void run(async () => {
			const now = await api.put<Faces>(person(from), { name: to });
			renaming = '';
			return now;
		});
	}
	let removing = $state('');
	/** A training picture to delete after the question (one tap on × must not lose it). */
	let removingPicture = $state<{ name: string; file: string } | null>(null);
	let assign = $state<Record<string, string>>({});
	const attempt = (file: string) => `/nvr/face-attempts/${encodeURIComponent(file)}`;

	const summary = $derived(
		missing
			? t('nvr.faces.missingShort')
			: !faces
				? ''
				: faces.faces.length
					? (faces.faces.length === 1
							? t('nvr.faces.countOne')
							: t('nvr.faces.count', { count: faces.faces.length })) +
						(faces.attempts.length
							? ` · ${faces.attempts.length === 1 ? t('nvr.faces.unknownCountOne') : t('nvr.faces.unknownCount', { count: faces.attempts.length })}`
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
										aria-invalid={renameProblem ? 'true' : undefined}
										aria-describedby="nvr-face-rename-rule"
										autofocus
									/>
									<button type="submit" class="btn" disabled={busy || !!renameProblem}
										>{t('common.save')}</button
									>
									<button type="button" class="btn" onclick={() => (renaming = '')}
										>{t('common.cancel')}</button
									>
									<small
										id="nvr-face-rename-rule"
										class="rule"
										class:bad={!!renameProblem}
										role={renameProblem ? 'alert' : undefined}
										>{renameProblem || t('nvr.faces.nameRule')}</small
									>
								</form>
							{:else}
								<b>{face.name}</b>
								<small
									>{face.images.length === 1
										? t('nvr.faces.picturesOne')
										: t('nvr.faces.pictures', { count: face.images.length })}</small
								>
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
										onclick={() => (removingPicture = { name: face.name, file })}>×</button
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
			<form class="inline add" onsubmit={add} novalidate>
				<input
					class="field"
					type="text"
					maxlength="40"
					bind:value={newName}
					aria-label={t('nvr.faces.name')}
					aria-invalid={newProblem ? 'true' : undefined}
					aria-describedby="nvr-face-add-rule"
					placeholder={t('nvr.faces.name')}
				/>
				<button type="submit" class="btn" disabled={busy || !newName.trim() || !!newProblem}
					>{t('nvr.faces.add')}</button
				>
				<small
					id="nvr-face-add-rule"
					class="rule"
					class:bad={!!newProblem}
					role={newProblem ? 'alert' : undefined}>{newProblem || t('nvr.faces.nameRule')}</small
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
											api.post<Faces>(`${attempt(a.file)}/assign`, {
												name: assign[a.file] ?? a.guess,
											}),
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
{#if removingPicture}
	<Confirm
		title={t('nvr.faces.deletePictureTitle')}
		text={t('nvr.faces.deletePictureText', { name: removingPicture.name })}
		confirm={t('common.delete')}
		danger
		{busy}
		onconfirm={() => {
			if (!removingPicture) return;
			const { name, file } = removingPicture;
			removingPicture = null;
			void run(() => api.delete(`${person(name)}/images/${encodeURIComponent(file)}`));
		}}
		onclose={() => (removingPicture = null)}
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
	.rule {
		flex: 1 1 100%;
		color: var(--muted);
		font-size: var(--text-sm);
		overflow-wrap: break-word;
	}
	.rule.bad {
		color: var(--error-text);
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
