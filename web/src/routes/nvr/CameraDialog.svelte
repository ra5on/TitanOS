<script lang="ts">
	// Add or change a camera: address, user and password (or a stream link),
	// then "Search" finds the streams. TitanOS tries at most three logins and
	// stops when the camera reports a locked account.
	import { untrack } from 'svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import {
		discoveryProblem,
		resolution,
		modeOf,
		SENSITIVITY,
		sensitivityChoice,
		type Camera,
		type Discovery,
	} from './nvr.ts';
	import type { MessageKey } from '#lib/i18n/index.svelte.ts';

	type Props = { camera?: Camera; vendors: string[]; onclose: () => void; onsaved: () => void };
	let { camera, vendors, onclose, onsaved }: Props = $props();

	// The dialog is mounted anew for each camera; later prop changes must not overwrite the form.
	const initial = untrack(() => camera);
	let name = $state(initial?.name ?? '');
	let host = $state(initial?.host ?? '');
	let user = $state(initial?.user ?? '');
	let password = $state('');
	let link = $state('');
	let vendor = $state(initial?.vendor ?? '');
	// New cameras: "only around events" is the suggestion, not a rule.
	let mode = $state<'off' | 'continuous' | 'events'>(initial ? modeOf(initial) : 'events');
	let eventKeepDays = $state(initial?.eventKeepDays ? String(initial.eventKeepDays) : '');
	let detect = $state(initial?.detect ?? false);
	let sensitivity = $state(sensitivityChoice(initial?.sensitivity));
	let audio = $state(initial?.audio ?? false);
	let keepDays = $state(initial?.keepDays ? String(initial.keepDays) : '');
	let viewers = $state<string[]>($state.snapshot(initial?.viewers ?? []));
	let accounts = $state<string[]>([]);
	let found = $state<Discovery | null>(null);
	let searching = $state(false);
	let saving = $state(false);
	let error = $state('');

	const editing = initial !== undefined;

	// Accounts without admin rights can be allowed to watch this camera.
	onMount(async () => {
		try {
			const list = await api.get<{ name: string; role: string }[]>('/users');
			accounts = list.filter((a) => a.role === 'user').map((a) => a.name);
		} catch {
			accounts = [];
		}
	});
	const problem = $derived(found ? discoveryProblem(found) : '');
	const canSearch = $derived(
		!searching && user.trim() !== '' && (host.trim() !== '' || link.trim() !== ''),
	);

	async function search() {
		error = '';
		found = null;
		searching = true;
		try {
			found = await api.post<Discovery>('/nvr/discover', {
				host: host.trim(),
				user: user.trim(),
				password,
				streamUrl: link.trim(),
				vendor,
				// Editing: an empty password field means the stored password.
				camera: initial?.id ?? '',
			});
			if (!name.trim()) name = host.trim() || t('nvr.defaultName');
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			searching = false;
		}
	}

	async function save() {
		error = '';
		// After a new search only what it found counts; old addresses of
		// another device must not stay behind.
		const main = found
			? (found.streams.find((s) => s.role === 'main')?.url ?? '')
			: (initial?.mainUrl ?? '');
		const sub = found
			? (found.streams.find((s) => s.role === 'sub')?.url ?? '')
			: (initial?.subUrl ?? '');
		const body = {
			name: name.trim(),
			host: host.trim(),
			user: user.trim(),
			vendor,
			mainUrl: main,
			subUrl: sub,
			record: mode !== 'off',
			recordMode: mode,
			eventKeepDays: eventKeepDays ? Number(eventKeepDays) : 0,
			audio,
			detect,
			sensitivity,
			viewers,
			// Areas are edited in their own dialog and kept here.
			zones: initial?.zones ?? [],
			masks: initial?.masks ?? [],
			onvifPort: found ? (found.onvifPort ?? 0) : (initial?.onvifPort ?? 0),
			keepDays: keepDays ? Number(keepDays) : 0,
			// An empty field keeps the stored password when editing.
			...(password !== '' || !editing ? { password } : {}),
		};
		saving = true;
		try {
			if (editing) await api.put(`/nvr/cameras/${initial?.id}`, body);
			else await api.post('/nvr/cameras', body);
			onsaved();
		} catch (e) {
			error = apiErrorText(e);
			saving = false;
		}
	}

	const ready = $derived(name.trim() !== '' && (found?.found || (editing && !found)));
</script>

<Dialog
	title={editing ? t('nvr.editTitle', { name: initial?.name ?? '' }) : t('nvr.addTitle')}
	locked={searching || saving}
	{onclose}
>
	<form
		onsubmit={(e) => {
			e.preventDefault();
			if (ready) void save();
			else if (canSearch) void search();
		}}
	>
		<label
			><span>{t('nvr.fieldName')}</span>
			<input class="field" bind:value={name} maxlength="60" /></label
		>
		<label
			><span>{t('nvr.fieldHost')}</span>
			<input
				class="field"
				bind:value={host}
				placeholder="192.168.1.20"
				autocomplete="off"
				spellcheck="false"
			/></label
		>
		<label
			><span>{t('nvr.fieldUser')}</span>
			<input class="field" bind:value={user} autocomplete="off" spellcheck="false" /></label
		>
		<label
			><span>{t('nvr.fieldPassword')}</span>
			<input class="field" type="password" bind:value={password} autocomplete="new-password" />
			{#if editing}<small class="muted">{t('nvr.passwordKeep')}</small>{/if}
		</label>
		<details>
			<summary>{t('nvr.more')}</summary>
			<label
				><span>{t('nvr.fieldLink')}</span>
				<input
					class="field"
					bind:value={link}
					placeholder="rtsp://192.168.1.20:554/stream1"
					spellcheck="false"
				/>
				<small class="muted">{t('nvr.linkHint')}</small>
			</label>
			<label
				><span>{t('nvr.fieldVendor')}</span>
				<select class="field" bind:value={vendor}>
					<option value="">{t('nvr.vendorAuto')}</option>
					{#each vendors.filter((v) => v !== 'generic') as v (v)}<option value={v}>{v}</option
						>{/each}
				</select>
			</label>
			<label class="check"
				><input type="checkbox" bind:checked={audio} /><span>{t('nvr.fieldAudio')}</span></label
			>
			<label
				><span>{t('nvr.fieldKeepDays')}</span>
				<input class="field" type="number" min="1" max="3650" bind:value={keepDays} />
				<small class="muted">{t('nvr.keepDaysHint')}</small>
			</label>
			<label
				><span>{t('nvr.fieldEventKeepDays')}</span>
				<input class="field" type="number" min="1" max="3650" bind:value={eventKeepDays} />
				<small class="muted">{t('nvr.keepDaysHint')}</small>
			</label>
		</details>

		<div class="row-actions">
			<button type="button" class="btn" onclick={search} disabled={!canSearch}
				>{searching ? t('nvr.searching') : t('nvr.search')}</button
			>
		</div>

		{#if found?.found}
			<div class="notice-ok" role="status">
				<strong>{t('nvr.foundTitle')}</strong>
				<ul>
					{#each found.streams as s (s.role)}
						<li>
							{t(s.role === 'main' ? 'nvr.streamMain' : 'nvr.streamSub')}
							{#if resolution(s)}· {resolution(s)}{/if}
							{#if s.codec}· {s.codec}{/if}
						</li>
					{/each}
				</ul>
				{#if found.clockOffsetSeconds !== undefined && Math.abs(found.clockOffsetSeconds) > 120}
					<p>{t('nvr.clockOff', { seconds: Math.round(Math.abs(found.clockOffsetSeconds)) })}</p>
				{/if}
			</div>
		{:else if problem}
			<div class="notice-warn" role="alert">
				{t(`nvr.problem.${problem}` as 'nvr.problem.none', { count: found?.rejected ?? 0 })}
			</div>
		{/if}
		{#if error}<div class="notice-error" role="alert">{error}</div>{/if}

		<label
			><span>{t('nvr.fieldMode')}</span>
			<select class="field" bind:value={mode}>
				<option value="events">{t('nvr.modeEvents')}</option>
				<option value="continuous">{t('nvr.modeContinuous')}</option>
				<option value="off">{t('nvr.modeOff')}</option>
			</select>
			{#if mode === 'events'}<small class="muted">{t('nvr.modeEventsHint')}</small>{/if}
		</label>
		<label class="check"
			><input type="checkbox" bind:checked={detect} /><span>{t('nvr.fieldDetect')}</span></label
		>
		{#if detect}
			<label
				><span>{t('nvr.fieldSensitivity')}</span>
				<select class="field" bind:value={sensitivity}>
					{#each SENSITIVITY as c (c.value)}<option value={c.value}>{t(c.key as MessageKey)}</option
						>{/each}
				</select>
			</label>
		{/if}

		{#if accounts.length > 0}
			<fieldset class="viewers">
				<legend>{t('nvr.viewers')}</legend>
				{#each accounts as a (a)}
					<label class="check"
						><input
							type="checkbox"
							checked={viewers.includes(a)}
							onchange={() =>
								(viewers = viewers.includes(a) ? viewers.filter((v) => v !== a) : [...viewers, a])}
						/><span>{a}</span></label
					>
				{/each}
				<small class="muted">{t('nvr.viewersHint')}</small>
			</fieldset>
		{/if}

		{#if !editing}<p class="muted small">
				{t('nvr.privacyHint')}
				<a
					href="https://github.com/ra5on/TitanOS/blob/main/docs/NVR.md#datenschutz"
					target="_blank"
					rel="noopener noreferrer">{t('nvr.privacyMore')}</a
				>
			</p>{/if}

		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose} disabled={saving}
				>{t('common.cancel')}</button
			>
			<button type="submit" class="btn btn-primary" disabled={!ready || saving}
				>{t('common.save')}</button
			>
		</div>
	</form>
</Dialog>

<style>
	form {
		display: grid;
		gap: 12px;
		min-width: min(420px, 100%);
	}
	label {
		display: grid;
		gap: 4px;
	}
	label.check {
		display: flex;
		gap: 8px;
		align-items: center;
	}
	.viewers {
		border: 0;
		padding: 0;
		margin: 0;
		display: grid;
		gap: 6px;
	}
	details {
		display: grid;
		gap: 12px;
	}
	summary {
		cursor: pointer;
	}
	ul {
		margin: 6px 0 0;
		padding-left: 18px;
	}
	.notice-ok p {
		margin: 6px 0 0;
	}
</style>
