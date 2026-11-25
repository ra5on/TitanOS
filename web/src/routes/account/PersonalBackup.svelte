<script lang="ts">
	import { api, apiFetch } from '#lib/api.ts';
	import { apiPath } from '#lib/cluster-node.ts';
	import { formatBytes } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';

	type Kind = 'calendar' | 'contacts' | 'tasks';
	type Source = { kind: Kind; id: string };
	type Collection = Source & { name: string; count: number; bytes: number };
	type Status = {
		settings: { recipient: string; sources: Source[] | null };
		collections: Collection[] | null;
		unavailable: Kind[] | null;
	};
	type Report = {
		account: string;
		collections: (Collection & {
			target?: string;
			added: number;
			identical: number;
			conflicts: number;
		})[];
	};
	const endpoint = '/account/personal-backup';
	let open = $state(false),
		status = $state<Status>(),
		loading = $state(false),
		busy = $state(false);
	let error = $state(''),
		message = $state(''),
		identity = $state(''),
		recipient = $state(''),
		selected = $state<Source[]>([]);
	let keyDownloaded = $state(false);
	let restoreKey = $state(''),
		files = $state<FileList>(),
		report = $state<Report>(),
		applied = $state(false);
	let testedFile: File | undefined = $state(),
		testedKey = $state('');
	const same = (a: Source, b: Source) => a.kind === b.kind && a.id === b.id;
	async function load() {
		loading = true;
		error = '';
		try {
			status = await api.get<Status>(endpoint);
			recipient = status.settings.recipient;
			selected = status.settings.sources ?? [];
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			loading = false;
		}
	}
	let requested = false;
	$effect(() => {
		if (open && !requested) {
			requested = true;
			void load();
		}
	});
	async function generate() {
		busy = true;
		error = message = '';
		try {
			const key = await api.post<{ identity: string; recipient: string }>(endpoint + '/key', {});
			identity = key.identity;
			keyDownloaded = false;
			recipient = key.recipient;
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	async function save() {
		busy = true;
		error = message = '';
		try {
			await api.put(endpoint, { recipient, sources: selected });
			await load();
			identity = '';
			message = t(selected.length ? 'personal.saved' : 'personal.disabled');
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	async function restore(apply: boolean) {
		const file = files?.[0],
			key = restoreKey.trim();
		if (!file || !key || (apply && (testedFile !== file || testedKey !== key))) return;
		busy = true;
		error = message = '';
		report = undefined;
		testedFile = undefined;
		testedKey = '';
		try {
			const form = new FormData();
			form.append('identity', key);
			form.append('file', file);
			const res = await apiFetch(endpoint + '/restore' + (apply ? '?apply=1' : ''), {
				method: 'POST',
				body: form,
			});
			const result = await res.json();
			if (res.ok) {
				report = result;
				applied = apply;
				if (!apply) {
					testedFile = file;
					testedKey = key;
				} else {
					restoreKey = '';
				}
			} else if (result.partial) {
				report = result.report;
				applied = true;
				error = t('personal.partial');
			} else {
				error = t(
					result.error?.code === 'personal_backup_busy' ? 'personal.busy' : 'personal.invalid',
				);
			}
		} catch {
			error = t('personal.invalid');
		} finally {
			busy = false;
		}
	}
	function downloadKey() {
		const url = URL.createObjectURL(new Blob([identity + '\n'], { type: 'text/plain' }));
		const a = document.createElement('a');
		a.href = url;
		a.download = 'TitanOS-age-key.txt';
		a.click();
		keyDownloaded = true;
		setTimeout(() => URL.revokeObjectURL(url), 1000);
	}
</script>

<SettingRow
	id="personal-backup"
	icon="lock"
	title={t('personal.title')}
	desc={t('personal.hint')}
	bind:open
>
	<div class="srow-block">
		{#if loading}<p role="status">{t('common.loading')}</p>{/if}
		{#if status}
			<fieldset disabled={busy}>
				<legend>{t('personal.sources')}</legend>
				{#each status.collections ?? [] as c (c.kind + ':' + c.id)}
					<label class="check"
						><input
							type="checkbox"
							checked={selected.some((v) => same(v, c))}
							onchange={(e) =>
								(selected = e.currentTarget.checked
									? [...selected, { kind: c.kind, id: c.id }]
									: selected.filter((v) => !same(v, c)))}
						/>
						<span>{c.name} · {t(`personal.${c.kind}`)} · {c.count} · {formatBytes(c.bytes)}</span>
					</label>
				{/each}
				{#each status.unavailable ?? [] as kind (kind)}<p class="notice-warn">
						{t('personal.offline', { name: t(`personal.${kind}`) })}
					</p>{/each}
			</fieldset>
			{#if !recipient}<button type="button" class="btn" onclick={generate} disabled={busy}
					>{t('personal.newKey')}</button
				>{/if}
			{#if identity}<p>{t('personal.keepKey')}</p>
				<button type="button" class="btn" onclick={downloadKey}>{t('personal.downloadKey')}</button>
			{/if}
			<button
				type="button"
				class="btn"
				disabled={busy || (selected.length > 0 && (!recipient || (!!identity && !keyDownloaded)))}
				onclick={save}>{t('common.save')}</button
			>
			{#if status.settings.sources?.length}<a
					class="btn"
					href={apiPath(endpoint + '/archive')}
					download>{t('personal.download')}</a
				>{/if}
		{:else if !loading && error}<button class="btn" onclick={load}>{t('ops.refresh')}</button>{/if}
		<details>
			<summary>{t('personal.restore')}</summary>
			<p>{t('personal.restoreHint')}</p>
			<label
				>{t('personal.archive')}<input
					class="field"
					type="file"
					accept=".age"
					bind:files
					disabled={busy}
				/></label
			>
			<label
				>{t('personal.key')}<input
					class="field"
					type="password"
					bind:value={restoreKey}
					autocomplete="off"
					spellcheck="false"
					disabled={busy}
				/></label
			>
			<button
				class="btn"
				type="button"
				onclick={() => restore(false)}
				disabled={busy || !files?.length || !restoreKey.trim()}>{t('personal.probe')}</button
			>
			{#if report}<p role="status">
					{t(applied ? 'personal.restored' : 'personal.verified', { account: report.account })}
				</p>
				<ul>
					{#each report.collections as c (c.kind + ':' + c.id)}<li>
							{c.name} · {c.count} · {formatBytes(c.bytes)}
							{#if applied}
								· {t('personal.result', {
									added: c.added,
									same: c.identical,
									conflicts: c.conflicts,
								})}{/if}
						</li>{/each}
				</ul>
			{/if}
			{#if testedFile && testedFile === files?.[0] && testedKey === restoreKey.trim()}
				<button type="button" class="btn btn-primary" onclick={() => restore(true)} disabled={busy}
					>{t('personal.apply')}</button
				>
			{/if}
		</details>
		{#if message}<p role="status">{message}</p>{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	</div>
</SettingRow>

<style>
	fieldset {
		border: 0;
		padding: 0;
		margin: 0 0 var(--space-3);
	}
	.check,
	details label {
		display: flex;
		gap: var(--space-2);
		margin: var(--space-2) 0;
	}
	details label {
		flex-direction: column;
	}
	details {
		margin-top: var(--space-3);
	}
</style>
