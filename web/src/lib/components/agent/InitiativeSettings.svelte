<script lang="ts">
	// Initiative, memory care and personality of the agent (PLAN 53), each
	// person for themselves: GET/PUT /agent/initiative, kept on the NAS in the
	// person's agent folder. Background work never wakes a sleeping disk; the
	// choice "Wecken / Erst fragen / Nie" is only for the person's own tasks.
	import { api, type FileRoot } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { defaultInitiative, sanitizeInitiative, type Initiative } from '#lib/agent.ts';

	let { part, onrestart }: { part: 'initiative' | 'mind'; onrestart?: () => void } = $props();

	let value = $state<Initiative>(defaultInitiative());
	let loaded = $state(false);
	let error = $state('');
	let roots = $state<FileRoot[]>([]);
	let timer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => {
		api
			.get<unknown>('/agent/initiative')
			// Saving starts only after the settings were read: an error never
			// overwrites them with defaults.
			.then((v) => {
				value = sanitizeInitiative(v);
				loaded = true;
			})
			.catch((e) => (error = apiErrorText(e)));
		api
			.get<FileRoot[]>('/files/roots')
			.then((r) => (roots = r.filter((x) => x.kind !== 'stick')))
			.catch(() => (roots = []));
	});
	$effect(() => {
		const body = $state.snapshot(value);
		if (!loaded) return;
		clearTimeout(timer);
		timer = setTimeout(() => {
			api
				.put('/agent/initiative', body)
				.then(() => (error = ''))
				.catch((e) => (error = apiErrorText(e)));
		}, 500);
	});
	const levels = ['quiet', 'hints', 'suggest', 'act'] as const;
	const searched = (id: string) => value.search.some((s) => s.root === id && !s.path);
	function toggleRoot(id: string, on: boolean) {
		value.search = on
			? [...value.search.filter((s) => s.root !== id), { root: id, path: '' }]
			: value.search.filter((s) => s.root !== id);
	}
	async function clearMemory() {
		try {
			await api.delete('/agent/memory');
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	let confirmClear = $state(false);
</script>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

{#if part === 'initiative'}
	<div class="grp level">
		<strong>{t('agent.init.how')}</strong>
		<div class="steps" role="radiogroup" aria-label={t('agent.init.how')}>
			{#each levels as l, i (l)}
				<label class:on={value.level === l} class:past={levels.indexOf(value.level) >= i}
					><input type="radio" bind:group={value.level} value={l} /><i aria-hidden="true"></i>{t(
						`agent.init.level.${l}`,
					)}</label
				>
			{/each}
		</div>
		<small>{t(`agent.init.levelHint.${value.level}`)}</small>
	</div>

	<p class="cap">{t('agent.init.night')}</p>
	<div class="grp">
		<label class="row">
			<span class="t"
				><strong>{t('agent.init.reflect')}</strong><small>{t('agent.init.reflectHint')}</small
				></span
			>
			<input type="checkbox" class="switch" bind:checked={value.reflect} />
		</label>
		<label class="row" class:off={!value.reflect}>
			<span class="t"><strong>{t('agent.init.at')}</strong></span>
			<input
				type="time"
				class="field time"
				bind:value={value.reflectAt}
				disabled={!value.reflect}
			/>
		</label>
		<div class="row col" class:off={!value.reflect}>
			<span class="t"
				><strong>{t('agent.init.take')}</strong><small>{t('agent.init.takeHint')}</small></span
			>
			<span class="seg" role="radiogroup" aria-label={t('agent.init.take')}>
				<label class:on={value.confirm}
					><input type="radio" bind:group={value.confirm} value={true} />{t(
						'agent.init.confirm',
					)}</label
				>
				<label class:on={!value.confirm}
					><input type="radio" bind:group={value.confirm} value={false} />{t(
						'agent.init.direct',
					)}</label
				>
			</span>
		</div>
		<label class="row" class:off={!value.reflect}>
			<span class="t"><strong>{t('agent.init.morningNote')}</strong></span>
			<input
				type="checkbox"
				class="switch"
				bind:checked={value.morningNote}
				disabled={!value.reflect}
			/>
		</label>
	</div>

	<p class="cap">{t('agent.init.watch')}</p>
	<div class="grp">
		<label class="row">
			<span class="t"
				><strong>{t('agent.init.watchLabel')}</strong><small>{t('agent.init.watchHint')}</small
				></span
			>
			<input type="checkbox" class="switch" bind:checked={value.watch} />
		</label>
		<label class="row">
			<span class="t"
				><strong>{t('agent.init.forecast')}</strong><small>{t('agent.init.forecastHint')}</small
				></span
			>
			<input type="checkbox" class="switch" bind:checked={value.forecast} />
		</label>
		{#each ['usb', 'update'] as const as ev (ev)}
			<label class="row">
				<span class="t"><strong>{t(`agent.init.event.${ev}`)}</strong></span>
				<input
					type="checkbox"
					class="switch"
					checked={value.events.includes(ev)}
					onchange={(e) =>
						(value.events = e.currentTarget.checked
							? [...value.events, ev]
							: value.events.filter((x) => x !== ev))}
				/>
			</label>
		{/each}
	</div>

	<p class="cap">{t('agent.init.disks')}</p>
	<div class="grp">
		<div class="row">
			<span class="t"
				><strong>{t('agent.init.background')}</strong><small>{t('agent.init.backgroundHint')}</small
				></span
			>
		</div>
		<div class="row col">
			<span class="t"
				><strong>{t('agent.init.wake')}</strong><small>{t('agent.init.wakeHint')}</small></span
			>
			<span class="seg" role="radiogroup" aria-label={t('agent.init.wake')}>
				{#each ['wake', 'ask', 'never'] as const as w (w)}
					<label class:on={value.wake === w}
						><input type="radio" bind:group={value.wake} value={w} />{t(
							`agent.init.wake.${w}`,
						)}</label
					>
				{/each}
			</span>
		</div>
	</div>

	<p class="cap">{t('agent.init.search')}</p>
	<div class="grp">
		<div class="row"><span class="t"><small>{t('agent.init.searchHint')}</small></span></div>
		{#each roots as r (r.id)}
			<label class="row">
				<span class="t"><strong>{r.name}</strong></span>
				<input
					type="checkbox"
					class="switch"
					checked={searched(r.id)}
					onchange={(e) => toggleRoot(r.id, e.currentTarget.checked)}
				/>
			</label>
		{/each}
	</div>

	<p class="cap">{t('agent.init.limits')}</p>
	<div class="grp">
		<div class="row">
			<span class="t"
				><strong>{t('agent.init.quiet')}</strong><small>{t('agent.init.quietHint')}</small></span
			>
			<input
				type="time"
				class="field time"
				bind:value={value.quietFrom}
				aria-label={t('agent.init.quietFrom')}
			/>
			<input
				type="time"
				class="field time"
				bind:value={value.quietTo}
				aria-label={t('agent.init.quietTo')}
			/>
		</div>
		<label class="row">
			<span class="t"
				><strong>{t('agent.init.gpu')}</strong><small
					>{t('agent.init.gpuHint', { min: value.gpuMinutes })}</small
				></span
			>
			<input type="range" min="5" max="120" step="5" bind:value={value.gpuMinutes} />
		</label>
		<label class="row">
			<span class="t"
				><strong>{t('agent.init.weekly')}</strong><small>{t('agent.init.weeklyHint')}</small></span
			>
			<input type="checkbox" class="switch" bind:checked={value.weekly} />
		</label>
	</div>
{:else}
	<p class="cap">{t('agent.memory')}</p>
	<div class="grp">
		<label class="row">
			<span class="t"
				><strong>{t('agent.init.memoryAuto')}</strong><small>{t('agent.init.memoryAutoHint')}</small
				></span
			>
			<input type="checkbox" class="switch" bind:checked={value.memoryAuto} />
		</label>
		<label class="row">
			<span class="t"
				><strong>{t('agent.init.conflicts')}</strong><small>{t('agent.init.conflictsHint')}</small
				></span
			>
			<input type="checkbox" class="switch" bind:checked={value.conflicts} />
		</label>
		<label class="row">
			<span class="t"
				><strong>{t('agent.init.fade')}</strong><small
					>{value.fadeMonths
						? t('agent.init.fadeHint', { months: value.fadeMonths })
						: t('agent.init.fadeOff')}</small
				></span
			>
			<input type="range" min="0" max="24" step="1" bind:value={value.fadeMonths} />
		</label>
		{#if confirmClear}
			<div class="row">
				<span class="t"><strong>{t('agent.init.clearSure')}</strong></span>
				<button
					type="button"
					class="btn btn-danger"
					onclick={() => {
						confirmClear = false;
						void clearMemory();
					}}>{t('agent.init.clear')}</button
				>
				<button type="button" class="btn" onclick={() => (confirmClear = false)}
					>{t('common.cancel')}</button
				>
			</div>
		{:else}
			<button type="button" class="row danger" onclick={() => (confirmClear = true)}
				><span class="t"><strong>{t('agent.init.clear')}</strong></span></button
			>
		{/if}
	</div>

	<p class="cap">{t('agent.init.skillsAndInbox')}</p>
	<div class="grp">
		<label class="row">
			<span class="t"
				><strong>{t('agent.init.learn')}</strong><small>{t('agent.init.learnHint')}</small></span
			>
			<input type="checkbox" class="switch" bind:checked={value.learnSkills} />
		</label>
		{#if onrestart}
			<div class="row">
				<span class="t"
					><strong>{t('agent.init.know')}</strong><small>{t('agent.init.knowHint')}</small></span
				>
				<button type="button" class="btn" onclick={onrestart}>{t('agent.init.knowStart')}</button>
			</div>
		{/if}
	</div>

	<p class="cap">{t('agent.init.personality')}</p>
	<div class="grp">
		<div class="row col">
			<span class="t"><strong>{t('agent.init.length')}</strong></span>
			<span class="seg" role="radiogroup" aria-label={t('agent.init.length')}>
				{#each ['short', 'normal', 'long'] as const as l (l)}
					<label class:on={value.length === l}
						><input type="radio" bind:group={value.length} value={l} />{t(
							`agent.init.length.${l}`,
						)}</label
					>
				{/each}
			</span>
		</div>
		<div class="row col">
			<span class="t"><strong>{t('agent.init.tone')}</strong></span>
			<span class="seg" role="radiogroup" aria-label={t('agent.init.tone')}>
				{#each ['casual', 'plain'] as const as tone (tone)}
					<label class:on={value.tone === tone}
						><input type="radio" bind:group={value.tone} value={tone} />{t(
							`agent.init.tone.${tone}`,
						)}</label
					>
				{/each}
			</span>
		</div>
		<label class="row">
			<span class="t"
				><strong>{t('agent.init.name')}</strong><small>{t('agent.init.nameHint')}</small></span
			>
			<input class="field short" maxlength="24" bind:value={value.name} placeholder="Titan" />
		</label>
		<label class="row">
			<span class="t"><strong>{t('agent.init.icon')}</strong></span>
			<input class="field emoji" maxlength="4" bind:value={value.icon} placeholder="✨" />
		</label>
	</div>
{/if}

<style>
	.cap {
		margin: 12px 6px 4px;
		font-size: var(--text-xs);
		font-weight: 600;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.grp {
		flex-shrink: 0;
		border-radius: 20px;
		overflow: hidden;
		background: color-mix(in srgb, var(--text) 4%, transparent);
		border: 1px solid color-mix(in srgb, var(--text) 9%, transparent);
	}
	.row {
		display: flex;
		align-items: center;
		gap: 12px;
		width: 100%;
		min-height: 52px;
		padding: 9px 12px;
		border: 0;
		border-top: 1px solid color-mix(in srgb, var(--text) 8%, transparent);
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.row:first-child {
		border-top: 0;
	}
	.row.off {
		opacity: 0.55;
	}
	.row.col {
		flex-wrap: wrap;
	}
	.row.col > .seg {
		flex-basis: 100%;
	}
	.row.danger strong {
		color: var(--danger, #fb7185);
	}
	.t {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.t strong {
		font-weight: 500;
	}
	.t small,
	.level small {
		color: var(--muted);
		font-size: var(--text-xs);
	}
	.seg {
		display: grid;
		grid-auto-flow: column;
		grid-auto-columns: 1fr;
		padding: 3px;
		border-radius: 12px;
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.seg label {
		text-align: center;
		white-space: nowrap;
		padding: 7px 8px;
		border-radius: 9px;
		font-size: var(--text-sm);
		font-weight: 600;
		color: var(--muted);
		cursor: pointer;
	}
	.seg label.on {
		color: var(--on-accent, #fff);
		background: var(--ag-gradient, var(--accent));
	}
	.seg input,
	.steps input {
		position: absolute;
		opacity: 0;
		pointer-events: none;
	}
	.seg label:has(input:focus-visible),
	.steps label:has(input:focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.level {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 14px;
	}
	.steps {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		position: relative;
	}
	.steps label {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 6px;
		font-size: var(--text-xs);
		color: var(--muted);
		cursor: pointer;
		text-align: center;
	}
	.steps i {
		width: 20px;
		height: 20px;
		border-radius: 50%;
		border: 2px solid color-mix(in srgb, var(--text) 25%, transparent);
		background: var(--surface);
	}
	.steps label.past i {
		background: var(--accent);
		border-color: var(--accent);
	}
	.steps label.on {
		color: var(--text);
		font-weight: 700;
	}
	.steps label.on i {
		box-shadow: 0 0 0 5px color-mix(in srgb, var(--accent) 30%, transparent);
	}
	.switch {
		width: 22px;
		height: 22px;
		accent-color: var(--accent);
		flex: none;
	}
	.time {
		width: 6.5rem;
	}
	.short {
		width: 8rem;
	}
	.emoji {
		width: 4rem;
		text-align: center;
	}
	input[type='range'] {
		accent-color: var(--accent);
		max-width: 40%;
	}
</style>
