<script lang="ts">
	// The connection to Home Assistant and what of it is released to the
	// assistant. The token never comes back from the NAS.
	import PageHead from '#lib/components/PageHead.svelte';
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Settings = { enabled: boolean; url: string; token?: string; fingerprint: string; domains: string[]; entities: string[] };
	type Answer = { settings: Settings; tokenSet: boolean; domains: string[] };
	type Entity = { id: string; name: string; area?: string; state: string; domain: string };

	// Domains whose calls need the ticked confirmation; the rest switch
	// after a normal confirmation, sensors are read only.
	const DANGER = ['cover', 'lock', 'alarm_control_panel', 'vacuum', 'button', 'valve', 'water_heater'];
	const READ_ONLY = ['sensor', 'binary_sensor', 'weather', 'person', 'device_tracker', 'sun', 'zone'];

	let draft = $state<Settings | null>(null);
	let tokenSet = $state(false);
	let clearToken = $state(false);
	let domains = $state<string[]>([]);
	let error = $state<{ text: string; detail: string } | null>(null);
	let saved = $state(false);
	let busy = $state<'save' | 'check' | 'entities' | null>(null);
	let status = $state<{ version: string; location: string } | null>(null);
	let checkError = $state('');
	let entities = $state<Entity[] | null>(null);
	let search = $state('');

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = { text: errorText(code), detail: err instanceof ApiError && code !== 'internal' ? err.message : '' };
	}

	function take(res: Answer) {
		draft = { ...res.settings, token: '', fingerprint: res.settings.fingerprint ?? '' };
		tokenSet = res.tokenSet;
		domains = res.domains;
		clearToken = false;
	}

	$effect(() => {
		api.get<Answer>('/homeassistant/settings').then(take, problem);
	});

	async function store(): Promise<boolean> {
		if (!draft) return false;
		error = null;
		saved = false;
		try {
			take(await api.put<Answer>('/homeassistant/settings', { ...draft, clearToken }));
			return true;
		} catch (err) {
			problem(err);
			return false;
		}
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		busy = 'save';
		saved = await store();
		busy = null;
	}

	async function check() {
		busy = 'check';
		status = null;
		checkError = '';
		if (await store()) {
			try {
				status = await api.post<{ version: string; location: string }>('/homeassistant/check', {});
			} catch (err) {
				checkError = err instanceof ApiError ? err.message : errorText('internal');
			}
		}
		busy = null;
	}

	async function loadEntities() {
		busy = 'entities';
		try {
			entities = (await api.get<{ entities: Entity[] }>('/homeassistant/entities')).entities;
		} catch (err) {
			problem(err);
		}
		busy = null;
	}

	function toggle(list: string[], value: string): string[] {
		return list.includes(value) ? list.filter((v) => v !== value) : [...list, value].sort();
	}

	const shown = $derived.by(() => {
		if (!entities || !draft) return [];
		const q = search.trim().toLowerCase();
		const known = new Set(domains);
		return entities.filter((e) => known.has(e.domain) && (!q || `${e.id} ${e.name} ${e.area ?? ''}`.toLowerCase().includes(q))).slice(0, 200);
	});
	const level = (domain: string) => (READ_ONLY.includes(domain) ? 'read' : DANGER.includes(domain) ? 'danger' : 'normal');
</script>

<PageHead title={t('ha.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('ha.intro')}</p>
</PageHead>

{#if error}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>{/if}

{#if draft}
	<form onsubmit={save}>
		<section class="tile" id="connection">
			<div class="head">
				<h2 class="tile-title">{t('ha.connection')}</h2>
				<label class="check"><input type="checkbox" bind:checked={draft.enabled} /><span>{t('notify.enabled')}</span></label>
			</div>
			<div class="row">
				<label class="wide"><span>{t('ha.url')}</span><input class="field mono" type="url" bind:value={draft.url} placeholder="http://homeassistant.local:8123" spellcheck="false" autocapitalize="none" required={draft.enabled} /></label>
			</div>
			<div class="row">
				<label class="wide"><span>{t('ha.token')}</span>
					<input class="field mono" type="password" bind:value={draft.token} autocomplete="new-password" placeholder={tokenSet && !clearToken ? t('notify.secretStored') : ''} required={draft.enabled && !tokenSet} />
					{#if tokenSet}<button type="button" class="link" onclick={() => (clearToken = !clearToken)}>{clearToken ? t('notify.secretRemoved') : t('notify.secretRemove')}</button>{/if}
				</label>
			</div>
			<p class="muted small">{t('ha.tokenHint')}</p>
			<details>
				<summary>{t('ha.fingerprint')}</summary>
				<label class="wide"><span>{t('ha.fingerprintLabel')}</span><input class="field mono" type="text" bind:value={draft.fingerprint} placeholder="AB:CD:…" spellcheck="false" /></label>
				<p class="muted small">{t('ha.fingerprintHint')}</p>
			</details>
			<div class="test">
				<button type="button" class="btn" disabled={busy !== null} onclick={check}>{t(busy === 'check' ? 'ha.checking' : 'ha.check')}</button>
				{#if status}<p class="notice-ok" role="status">{t('ha.checkOk', { version: status.version, location: status.location })}</p>{/if}
				{#if checkError}<div class="notice-error" role="alert">{t('ha.checkFailed')}<pre>{checkError}</pre></div>{/if}
			</div>
		</section>

		<section class="tile" id="release">
			<h2 class="tile-title">{t('ha.release')}</h2>
			<p class="muted">{t('ha.releaseIntro')} <a href="/settings/assistant">{t('ha.releaseArea')}</a></p>
			<fieldset>
				<legend>{t('ha.domains')}</legend>
				{#each domains as domain}
					<label class="check domain"><input type="checkbox" checked={draft.domains.includes(domain)} onchange={() => (draft!.domains = toggle(draft!.domains, domain))} /><span class="mono">{domain}</span><span class="chip {level(domain) === 'danger' ? 'warn' : 'info'}">{t(`ha.level.${level(domain)}`)}</span></label>
				{/each}
			</fieldset>
			<div class="entities">
				<div class="row end">
					<button type="button" class="btn" disabled={busy !== null || !tokenSet} onclick={loadEntities}>{t(entities ? 'ha.reloadEntities' : 'ha.loadEntities')}</button>
					{#if entities}<label class="narrow"><span>{t('ha.searchEntities')}</span><input class="field" type="search" bind:value={search} /></label>{/if}
				</div>
				{#if draft.entities.length}<p class="muted small">{t('ha.singleCount', { count: draft.entities.length })}</p>{/if}
				{#if entities}
					<ul>
						{#each shown as entity (entity.id)}
							{@const viaDomain = draft.domains.includes(entity.domain)}
							<li>
								<label class="check"><input type="checkbox" checked={viaDomain || draft.entities.includes(entity.id)} disabled={viaDomain} onchange={() => (draft!.entities = toggle(draft!.entities, entity.id))} />
									<span><span class="name">{entity.name}</span> <span class="mono muted">{entity.id}</span>{#if entity.area}<span class="muted">&nbsp;· {entity.area}</span>{/if}</span>
								</label>
								<span class="mono muted">{entity.state}</span>
							</li>
						{:else}
							<li class="muted">{t('ha.noEntities')}</li>
						{/each}
					</ul>
				{/if}
			</div>
		</section>

		<div class="actions">
			{#if saved}<p class="notice-ok" role="status">{t('notify.saved')}</p>{/if}
			<button type="submit" class="btn btn-primary" disabled={busy !== null}>{t(busy === 'save' ? 'notify.saving' : 'notify.save')}</button>
		</div>
	</form>
{/if}

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
	}
	section {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 18px;
		font-size: 0.875rem;
	}
	section > p {
		margin: 0;
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px 16px;
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
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: 16px 24px;
	}
	.end {
		align-items: flex-end;
	}
	.wide {
		flex: 1 1 360px;
		max-width: 664px;
	}
	.narrow {
		flex: 0 1 320px;
		max-width: 320px;
	}
	.small {
		margin: 0;
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
	fieldset {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 230px), 1fr));
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
	.domain .chip {
		font-size: 0.6875rem;
	}
	details {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	summary {
		cursor: pointer;
		color: var(--muted);
	}
	.entities {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	ul {
		margin: 0;
		padding: 0;
		list-style: none;
		max-height: 28rem;
		overflow: auto;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	li {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 8px 12px;
		border-bottom: 1px solid var(--border);
	}
	li:last-child {
		border-bottom: 0;
	}
	li .check {
		min-width: 0;
	}
	li .check > span {
		overflow-wrap: anywhere;
	}
	.name {
		font-weight: 600;
	}
	.link {
		align-self: flex-start;
		padding: 0;
		background: none;
		border: 0;
		color: var(--muted);
		font: inherit;
		font-size: 0.75rem;
		text-decoration: underline;
		cursor: pointer;
	}
	.test {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px 16px;
	}
	.test > :global(.notice-ok),
	.test > :global(.notice-error) {
		margin: 0;
		flex: 1 1 280px;
	}
	.actions {
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 16px;
	}
	.actions p {
		margin: 0;
	}
	pre {
		margin: 6px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
