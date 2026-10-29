<script lang="ts">
	// Storage for Home Assistant: shares (or folders in them) that Home
	// Assistant OS mounts as network storage for backups, media or add-ons,
	// all with one account of its own. TitanOS can enter them there itself
	// and watches the backups.
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Usage = 'backup' | 'media' | 'share';
	type Backup = {
		checked: string;
		latest?: string;
		name?: string;
		error?: string;
		overdue: boolean;
	};
	type Entry = {
		share: string;
		folder?: string;
		existing?: boolean;
		readOnly?: boolean;
		usage: Usage;
		mount: string;
		created: string;
		maxAgeDays: number;
		backup?: Backup;
	};
	type Status = {
		account: string;
		server: string;
		entries: Entry[];
		volumes: string[];
		shares: { name: string; volume: string }[];
		homeAssistant: boolean;
	};
	type HAResult = { mount: string; ok: boolean; error?: string };
	type Answer = {
		setup: { account: string; server: string; entries: Entry[] };
		entry?: Entry;
		password?: string;
		results?: HAResult[];
	};

	let status = $state<Status | null>(null);
	let loadError = $state('');
	let error = $state('');
	let busy = $state('');
	let adding = $state(false);
	let result = $state<Answer | null>(null);
	let confirm = $state<{ kind: 'register' } | { kind: 'remove'; entry: Entry } | null>(null);
	let copied = $state(false);
	let form = $state(blankForm());
	// The folder chooser for a share that exists: one level at a time.
	let picking = $state(false);
	let pickDir = $state('');
	let pickDirs = $state<string[] | null>(null);
	let pickError = $state('');

	const usages: Usage[] = ['backup', 'media', 'share'];
	const ages = [0, 1, 2, 3, 7, 14, 30];
	const hasBackups = $derived(!!status?.entries.some((e) => e.usage === 'backup'));

	function blankForm() {
		return {
			account: 'homeassistant',
			existing: false,
			volume: '',
			share: 'homeassistant',
			existingShare: '',
			folder: '',
			quotaGiB: '',
			usage: 'backup' as Usage,
			readOnly: false,
			register: true,
		};
	}
	function failure(err: unknown) {
		return errorText(err instanceof ApiError ? err.code : 'internal');
	}
	function take(next: Status) {
		// Older titand (and the development mode without volumes) may send null.
		next.volumes ??= [];
		next.shares ??= [];
		next.entries ??= [];
		status = next;
	}
	async function load() {
		loadError = '';
		try {
			take(await api.get<Status>('/homeassistant/storage'));
		} catch (err) {
			loadError = failure(err);
		}
	}
	$effect(() => {
		void load();
	});

	async function run<T>(name: string, call: () => Promise<T>): Promise<T | null> {
		if (busy) return null;
		busy = name;
		error = '';
		try {
			return await call();
		} catch (err) {
			error = failure(err);
			return null;
		} finally {
			busy = '';
		}
	}

	function startAdding() {
		if (!status) return;
		form = blankForm();
		form.volume = status.volumes[0] ?? '';
		form.existingShare = status.shares[0]?.name ?? '';
		// A second entry is most often media in a share that exists.
		if (status.entries.length) {
			form.existing = status.shares.length > 0;
			form.usage = 'media';
		}
		form.register = status.homeAssistant;
		error = '';
		adding = true;
	}
	async function add(event: SubmitEvent) {
		event.preventDefault();
		const register = form.register && !!status?.homeAssistant;
		const readOnly = form.readOnly && form.usage !== 'backup';
		const body = form.existing
			? {
					account: form.account,
					existing: true,
					share: form.existingShare,
					folder: form.folder,
					usage: form.usage,
					readOnly,
					register,
				}
			: {
					account: form.account,
					volume: form.volume,
					share: form.share,
					quotaGiB: Number(form.quotaGiB) || 0,
					usage: form.usage,
					readOnly,
					register,
				};
		const made = await run('add', () => api.post<Answer>('/homeassistant/storage', body));
		if (made) {
			adding = false;
			result = made;
			copied = false;
			await load();
		}
	}
	async function register() {
		confirm = null;
		const made = await run('register', () =>
			api.post<Answer>('/homeassistant/storage/register', {}),
		);
		if (made) {
			result = made;
			copied = false;
			await load();
		}
	}
	async function remove(entry: Entry) {
		confirm = null;
		const done = await run('remove', () =>
			api.delete<{ removedError: string }>(
				`/homeassistant/storage/${encodeURIComponent(entry.mount)}`,
			),
		);
		if (done?.removedError)
			error = t('hastore.removeHAFailed', { error: problem(done.removedError) });
		await load();
	}
	async function setAge(entry: Entry, days: number) {
		const next = await run('age', () =>
			api.put<Status>(`/homeassistant/storage/${encodeURIComponent(entry.mount)}`, {
				maxAgeDays: days,
			}),
		);
		if (next) take(next);
	}
	async function check() {
		const next = await run('check', () => api.post<Status>('/homeassistant/storage/check', {}));
		if (next) take(next);
	}
	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			copied = true;
		} catch {
			copied = false;
		}
	}
	function place(entry: Entry) {
		return entry.folder ? `${entry.share}/${entry.folder}` : entry.share;
	}
	function problem(text: string) {
		return text === 'no_supervisor' ? t('hastore.noSupervisor') : text;
	}

	async function browse(dir: string) {
		pickError = '';
		try {
			const list = await api.get<{ name: string; dir: boolean }[]>(
				`/files/list?${new URLSearchParams({ root: `share/${form.existingShare}`, path: dir })}`,
			);
			pickDir = dir;
			pickDirs = list
				.filter((entry) => entry.dir && !entry.name.startsWith('.'))
				.map((entry) => entry.name)
				.sort((a, b) => a.localeCompare(b));
		} catch (err) {
			pickError = failure(err);
		}
	}
	function openPicker() {
		picking = true;
		pickDirs = null;
		void browse(form.folder);
	}
	function up() {
		void browse(pickDir.includes('/') ? pickDir.slice(0, pickDir.lastIndexOf('/')) : '');
	}
</script>

<section class="tile panel" id="storage" aria-labelledby="hastore-title">
	<h2 class="tile-title" id="hastore-title">{t('hastore.title')}</h2>
	<p class="muted hint">{t('hastore.intro')}</p>

	{#if loadError}
		<div class="notice-error" role="alert">
			{loadError}
			<button class="btn small" type="button" onclick={load}>{t('common.retry')}</button>
		</div>
	{:else if !status}
		<p class="muted" role="status">{t('common.loading')}</p>
	{:else}
		{#if status.account}
			<p class="account">
				{t('hastore.accountLine', { account: status.account, server: status.server })}
			</p>
		{/if}
		{#if status.entries.length}
			<ul class="entries">
				{#each status.entries as entry (entry.mount)}
					<li>
						<div class="entry-head">
							<span class="mono name">{place(entry)}</span>
							<span class="chip">{t(`hastore.usage.${entry.usage}`)}</span>
							{#if entry.readOnly}<span class="chip">{t('hastore.readOnlyShort')}</span>{/if}
							<button
								class="btn small remove"
								type="button"
								disabled={!!busy}
								onclick={() => (confirm = { kind: 'remove', entry })}>{t('hastore.remove')}</button
							>
						</div>
						<span class="muted small">{t('hastore.mountLine', { mount: entry.mount })}</span>
						{#if entry.usage === 'backup'}
							<div class="backup">
								{#if entry.backup?.error}
									<span class="muted small">{t('hastore.backupUnknown')}</span>
								{:else if entry.backup?.latest}
									<span class="small" class:warn={entry.backup.overdue}
										>{t('hastore.backupLatest', {
											name: entry.backup.name ?? '',
											date: formatDateTime(new Date(entry.backup.latest)),
										})}</span
									>
								{:else if entry.backup}
									<span
										class="small"
										class:warn={entry.backup.overdue}
										class:muted={!entry.backup.overdue}>{t('hastore.backupNone')}</span
									>
								{/if}
								<label class="age small"
									><span>{t('hastore.maxAge')}</span>
									<select
										class="field"
										value={entry.maxAgeDays}
										disabled={!!busy}
										onchange={(event) => setAge(entry, Number(event.currentTarget.value))}
									>
										{#each ages as days (days)}<option value={days}
												>{days ? t('hastore.days', { count: days }) : t('hastore.off')}</option
											>{/each}
									</select></label
								>
							</div>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
		{#if !status.volumes.length && !status.shares.length}
			<p class="notice-warn">{t('hastore.noVolume')}</p>
		{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="row-actions">
			<button
				class="btn"
				class:btn-primary={!status.entries.length}
				type="button"
				disabled={!!busy || (!status.volumes.length && !status.shares.length)}
				onclick={startAdding}>{t(status.entries.length ? 'hastore.addMore' : 'hastore.add')}</button
			>
			{#if status.entries.length}
				<button
					class="btn"
					type="button"
					disabled={!!busy || !status.homeAssistant}
					onclick={() => (confirm = { kind: 'register' })}>{t('hastore.registerAgain')}</button
				>
			{/if}
			{#if hasBackups}
				<button class="btn" type="button" disabled={!!busy || !status.homeAssistant} onclick={check}
					>{t(busy === 'check' ? 'hastore.checking' : 'hastore.check')}</button
				>
			{/if}
		</div>
		{#if hasBackups}<p class="muted hint">{t('hastore.maxAgeHint')}</p>{/if}
	{/if}
</section>

{#if adding && status}
	<Dialog
		title={t(status.entries.length ? 'hastore.addMore' : 'hastore.add')}
		locked={!!busy}
		onclose={() => (adding = false)}
	>
		<form class="setup" onsubmit={add}>
			{#if status.shares.length}
				<fieldset class="mode" aria-label={t('hastore.where')}>
					<label class="check"
						><input
							type="radio"
							name="hastore-mode"
							value={false}
							bind:group={form.existing}
						/><span>{t('hastore.newShare')}</span></label
					>
					<label class="check"
						><input type="radio" name="hastore-mode" value={true} bind:group={form.existing} /><span
							>{t('hastore.existingShare')}</span
						></label
					>
				</fieldset>
			{/if}
			<div class="grid">
				{#if form.existing}
					<label class="stack"
						><span>{t('hastore.share')}</span>
						<select
							class="field"
							bind:value={form.existingShare}
							onchange={() => (form.folder = '')}
						>
							{#each status.shares as share (share.name)}<option value={share.name}
									>{share.name} ({share.volume})</option
								>{/each}
						</select></label
					>
					<div class="stack">
						<span id="hastore-folder">{t('hastore.folder')}</span>
						<div class="folder-row">
							<span class="field mono path" aria-labelledby="hastore-folder"
								>{form.folder
									? `${form.existingShare}/${form.folder}`
									: t('hastore.wholeShare')}</span
							>
							<button class="btn" type="button" disabled={!form.existingShare} onclick={openPicker}
								>{t('hastore.chooseFolder')}</button
							>
						</div>
					</div>
				{:else}
					<label class="stack"
						><span>{t('hastore.volume')}</span>
						<select class="field" bind:value={form.volume}>
							{#each status.volumes as volume (volume)}<option value={volume}>{volume}</option
								>{/each}
						</select></label
					>
					<label class="stack"
						><span>{t('hastore.share')}</span><input
							class="field mono"
							bind:value={form.share}
							spellcheck="false"
							autocapitalize="off"
						/></label
					>
					<label class="stack"
						><span>{t('hastore.quota')}</span><input
							class="field"
							type="number"
							min="0"
							step="1"
							inputmode="numeric"
							placeholder={t('hastore.quotaNone')}
							bind:value={form.quotaGiB}
						/></label
					>
				{/if}
				{#if !status.account}
					<label class="stack"
						><span>{t('hastore.account')}</span><input
							class="field mono"
							bind:value={form.account}
							spellcheck="false"
							autocapitalize="off"
						/></label
					>
				{/if}
			</div>
			<fieldset class="usage">
				<legend>{t('hastore.usage')}</legend>
				{#each usages as usage (usage)}
					<label class="check"
						><input type="radio" name="hastore-usage" value={usage} bind:group={form.usage} /><span
							><strong>{t(`hastore.usage.${usage}`)}</strong>
							<span class="muted">{t(`hastore.usageHint.${usage}`)}</span></span
						></label
					>
				{/each}
			</fieldset>
			{#if form.usage !== 'backup'}
				<label class="check"
					><input type="checkbox" bind:checked={form.readOnly} /><span>{t('hastore.readOnly')}</span
					></label
				>
			{/if}
			{#if form.existing}<p class="muted hint">{t('hastore.existingHint')}</p>{/if}
			<label class="check"
				><input
					type="checkbox"
					bind:checked={form.register}
					disabled={!status.homeAssistant}
				/><span>{t('hastore.register')}</span></label
			>
			<p class="muted hint">
				{t(
					!status.homeAssistant
						? 'hastore.registerOff'
						: status.entries.length
							? 'hastore.registerHintMore'
							: 'hastore.registerHint',
				)}
			</p>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={!!busy} onclick={() => (adding = false)}
					>{t('common.cancel')}</button
				>
				<button
					class="btn btn-primary"
					disabled={!!busy ||
						(!status.account && !form.account) ||
						(form.existing ? !form.existingShare : !form.volume || !form.share)}
					>{t(busy === 'add' ? 'hastore.settingUp' : 'hastore.setUp')}</button
				>
			</div>
		</form>
	</Dialog>
{/if}

{#if result}
	<Dialog title={t('hastore.resultTitle')} onclose={() => (result = null)}>
		{#if result.results}
			<ul class="results">
				{#each result.results as r (r.mount)}
					<li class:notice-ok={r.ok} class:notice-warn={!r.ok}>
						{r.ok
							? t('hastore.registered', { name: r.mount })
							: t('hastore.registerFailedFor', { name: r.mount, error: problem(r.error ?? '') })}
					</li>
				{/each}
			</ul>
		{/if}
		{#if result.entry}
			<p>{t(result.results?.every((r) => r.ok) ? 'hastore.valuesKept' : 'hastore.valuesHowTo')}</p>
			<dl class="values">
				<dt>{t('hastore.mount')}</dt>
				<dd class="mono">{result.entry.mount}</dd>
				<dt>{t('hastore.usage')}</dt>
				<dd>{t(`hastore.usage.${result.entry.usage}`)}</dd>
				<dt>{t('hastore.server')}</dt>
				<dd class="mono">{result.setup.server}</dd>
				<dt>{t('hastore.protocol')}</dt>
				<dd>Samba/Windows (CIFS)</dd>
				<dt>{t('hastore.share')}</dt>
				<dd class="mono">{place(result.entry)}</dd>
				<dt>{t('hastore.account')}</dt>
				<dd class="mono">{result.setup.account}</dd>
			</dl>
		{/if}
		{#if result.password}
			<dl class="values">
				<dt>{t('hastore.password')}</dt>
				<dd class="password">
					<span class="mono">{result.password}</span>
					<button class="btn small" type="button" onclick={() => copy(result?.password ?? '')}
						>{t(copied ? 'hastore.copied' : 'hastore.copy')}</button
					>
				</dd>
			</dl>
			<p class="notice-warn">{t('hastore.passwordOnce')}</p>
		{:else if result.entry}
			<p class="muted">{t('hastore.passwordSame')}</p>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn btn-primary" onclick={() => (result = null)}
				>{t('common.close')}</button
			>
		</div>
	</Dialog>
{/if}

{#if picking}
	<Dialog
		title={t('hastore.chooseFolderIn', { share: form.existingShare })}
		onclose={() => (picking = false)}
	>
		<p class="mono crumbs">{form.existingShare}{pickDir ? `/${pickDir}` : ''}</p>
		{#if pickError}<p class="notice-error" role="alert">{pickError}</p>{/if}
		{#if pickDirs === null}
			<p class="muted" role="status">{t('common.loading')}</p>
		{:else}
			<ul class="dirs">
				{#if pickDir}<li>
						<button type="button" class="dir" onclick={up}>‹ {t('hastore.up')}</button>
					</li>{/if}
				{#each pickDirs as name (name)}
					<li>
						<button
							type="button"
							class="dir mono"
							onclick={() => browse(pickDir ? `${pickDir}/${name}` : name)}>{name}</button
						>
					</li>
				{:else}
					<li class="muted">{t('hastore.noSubfolders')}</li>
				{/each}
			</ul>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (picking = false)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				onclick={() => {
					form.folder = pickDir;
					picking = false;
				}}>{t('hastore.useThisFolder')}</button
			>
		</div>
	</Dialog>
{/if}

{#if confirm}
	<Dialog
		title={t(confirm.kind === 'register' ? 'hastore.registerAgain' : 'hastore.removeTitle')}
		locked={!!busy}
		onclose={() => (confirm = null)}
	>
		{#if confirm.kind === 'register'}
			<p>{t('hastore.registerAgainText', { account: status?.account ?? '' })}</p>
		{:else}
			<p>
				{t('hastore.removeText', { place: place(confirm.entry), account: status?.account ?? '' })}
			</p>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={!!busy} onclick={() => (confirm = null)}
				>{t('common.cancel')}</button
			>
			{#if confirm.kind === 'register'}
				<button type="button" class="btn btn-warn" disabled={!!busy} onclick={register}
					>{t('hastore.registerAgain')}</button
				>
			{:else}
				{@const entry = confirm.entry}
				<button type="button" class="btn btn-danger" disabled={!!busy} onclick={() => remove(entry)}
					>{t('hastore.remove')}</button
				>
			{/if}
		</div>
	</Dialog>
{/if}

<style>
	.hint,
	.account {
		font-size: 0.875rem;
	}
	.small {
		font-size: 0.8125rem;
	}
	.warn {
		color: var(--warn-text);
		font-weight: 600;
	}
	.entries {
		display: flex;
		flex-direction: column;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.entries li {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 10px 0;
		border-top: 1px solid var(--panel-border);
	}
	.entries li:last-child {
		border-bottom: 1px solid var(--panel-border);
	}
	.entry-head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 8px;
	}
	.entry-head .name {
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.entry-head .remove {
		margin-left: auto;
	}
	.backup {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 16px;
	}
	.age {
		display: flex;
		align-items: center;
		gap: 8px;
		color: var(--muted);
	}
	.age select {
		width: auto;
	}
	.setup {
		display: flex;
		flex-direction: column;
		gap: 12px;
		min-width: min(36rem, 100%);
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr));
		gap: 12px;
	}
	.mode,
	.usage {
		display: flex;
		margin: 0;
		padding: 0;
		border: 0;
	}
	.mode {
		flex-wrap: wrap;
		gap: 8px 20px;
	}
	.usage {
		flex-direction: column;
		gap: 8px;
	}
	.usage legend {
		margin-bottom: 6px;
		font-size: 0.875rem;
	}
	.folder-row {
		display: flex;
		gap: 8px;
	}
	.folder-row .path {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.results {
		display: flex;
		flex-direction: column;
		gap: 6px;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.values {
		display: grid;
		grid-template-columns: max-content minmax(0, 1fr);
		gap: 6px 16px;
		margin: 0;
		font-size: 0.875rem;
	}
	.values dt {
		color: var(--muted);
	}
	.values dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.password {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
	}
	.crumbs {
		margin: 0;
		color: var(--muted);
		overflow-wrap: anywhere;
	}
	.dirs {
		display: flex;
		flex-direction: column;
		gap: 2px;
		max-height: 20rem;
		min-width: min(28rem, 100%);
		margin: 0;
		padding: 0;
		overflow-y: auto;
		list-style: none;
	}
	.dir {
		width: 100%;
		padding: 8px 10px;
		background: none;
		border: 0;
		border-radius: var(--radius-small);
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.dir:hover,
	.dir:focus-visible {
		background: var(--chip);
	}
</style>
