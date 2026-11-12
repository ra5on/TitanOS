<script lang="ts">
	// PLAN 79-7 (drawing 79-nvr/entwurf, parts 2 and 3): one camera as rows –
	// how it is reached, what it looks for, what it records, its sound and
	// whom it tells. The same rows add a camera and change one. Nobody writes
	// a configuration file: the NAS makes it from these choices.
	import { untrack } from 'svelte';
	import { api, ApiError } from '../api.ts';
	import Icon from '../components/Icon.svelte';
	import Segments from '../components/Segments.svelte';
	import SettingGroup from '../components/settings/SettingGroup.svelte';
	import SettingRow from '../components/settings/SettingRow.svelte';
	import SettingSub from '../components/settings/SettingSub.svelte';
	import { apiErrorText, t, type MessageKey } from '../i18n/index.svelte.ts';
	import Pills from './Pills.svelte';
	import {
		groupsOf,
		labelName,
		OBJECT_GROUP_IDS,
		OBJECT_GROUPS,
		toggleGroup,
		type ObjectGroup,
	} from './texts.ts';
	import type {
		Camera,
		CameraNotify,
		Discovered,
		NvrHardware,
		ProbeResult,
		RecordMode,
	} from './types.ts';

	let {
		camera,
		hardware,
		objects,
		taken = [],
		oncancel,
		onsaved,
	}: {
		/** The camera to change; without one a new camera is added. */
		camera?: Camera;
		hardware?: NvrHardware;
		/** The objects the NAS allows. */
		objects: string[];
		/** Names of the other cameras: a name is unique, upper and lower case aside. */
		taken?: string[];
		oncancel: () => void;
		onsaved: (camera: Camera) => void;
	} = $props();

	// The form starts from the camera as it is now; a later change of the
	// list must not overwrite what is being typed.
	const initial = untrack(() => camera);
	const adding = !initial;
	const uid = $props.id();

	// ---- What is edited ----
	let kind = $state<'onvif' | 'rtsp'>(initial?.source?.kind ?? 'onvif');
	let name = $state(initial?.name ?? '');
	let host = $state(initial?.source?.host ?? '');
	let onvifPort = $state(initial?.source?.onvifPort || 80);
	let mainURL = $state(initial?.source?.mainURL ?? '');
	let subURL = $state(initial?.source?.subURL ?? '');
	let user = $state(initial?.source?.user ?? '');
	let password = $state('');
	let allowPublic = $state(initial?.source?.allowPublic ?? false);
	let askPublic = $state(false);
	let detectObjects = $state<string[]>(initial?.detect?.objects ?? ['person']);
	let detectOn = $state(initial?.detect?.enabled ?? true);
	let plates = $state(initial?.detect?.plates ?? false);
	let faces = $state(initial?.detect?.faces ?? false);
	let mode = $state<RecordMode>(initial?.record?.mode ?? 'events');
	let retainDays = $state(initial?.record?.retainDays ?? 14);
	let preSeconds = $state(initial?.record?.preSeconds ?? 5);
	let postSeconds = $state(initial?.record?.postSeconds ?? 5);
	let audio = $state(initial?.audio.enabled ?? false);
	let twoWay = $state(initial?.audio.twoWay ?? false);
	let ptz = $state(initial?.ptz ?? false);
	let notify = $state<CameraNotify>(
		initial?.notify
			? { ...initial.notify, kinds: [...initial.notify.kinds] }
			: {
					titan: true,
					push: false,
					email: false,
					homeAssistant: false,
					agent: false,
					kinds: ['person', 'offline'],
				},
	);

	// ---- Search in the network ----
	let found = $state<Discovered[] | null>(null);
	let searching = $state(false);
	let searchError = $state('');
	async function search() {
		searching = true;
		searchError = '';
		try {
			found = (await api.post<{ cameras: Discovered[] }>('/nvr/cameras/discover', {})).cameras;
		} catch (err) {
			searchError = apiErrorText(err);
		} finally {
			searching = false;
		}
	}
	function take(hit: Discovered) {
		kind = 'onvif';
		host = hit.address;
		onvifPort = hit.onvifPort || 80;
		if (!name) name = hit.name || hit.hardware;
		probe = null;
		mainURL = '';
		subURL = '';
	}

	// ---- Check ----
	let probe = $state<ProbeResult | null>(null);
	let probing = $state(false);
	let probeError = $state('');
	const canProbe = $derived(kind === 'onvif' ? host.trim() !== '' : mainURL.trim() !== '');
	async function check() {
		probing = true;
		probeError = '';
		probe = null;
		try {
			const res = await api.post<ProbeResult>('/nvr/cameras/probe', {
				kind,
				host: host.trim(),
				onvifPort: kind === 'onvif' ? Number(onvifPort) || 80 : 0,
				mainURL: kind === 'rtsp' ? mainURL.trim() : '',
				subURL: kind === 'rtsp' ? subURL.trim() : '',
				user: user.trim(),
				password,
				// Changing a camera: the NAS uses the stored password when none is typed.
				camera: camera?.id ?? '',
				allowPublic,
			});
			probe = res;
			if (res.outcome === 'ok') {
				if (kind === 'onvif') {
					mainURL = res.mainURL;
					subURL = res.subURL;
					if (res.onvifPort) onvifPort = res.onvifPort;
					if (adding) ptz = res.ptz;
				}
				if (!res.ptz) ptz = false;
			}
		} catch (err) {
			if (err instanceof ApiError && err.code === 'nvr_target_public') askPublic = true;
			probeError = apiErrorText(err);
		} finally {
			probing = false;
		}
	}
	const probeText = $derived(
		probe ? t(`nvr.probe.${knownOutcome(probe.outcome)}` as MessageKey) : '',
	);
	function knownOutcome(outcome: string) {
		return ['ok', 'unauthorized', 'locked', 'unreachable', 'not_found', 'no_stream'].includes(
			outcome,
		)
			? outcome
			: 'failed';
	}
	const streamText = (s: ProbeResult['streams'][number]) =>
		[
			t(s.role === 'main' ? 'nvr.probe.main' : 'nvr.probe.sub'),
			s.outcome === 'ok'
				? [
						s.width && s.height ? `${s.width} × ${s.height}` : '',
						s.codec,
						s.audio ? t('nvr.probe.withSound') : t('nvr.probe.noSound'),
					]
						.filter(Boolean)
						.join(' · ')
				: t(`nvr.probe.${knownOutcome(s.outcome)}` as MessageKey),
		].join(': ');
	// The sound a check found: a stream with an audio track.
	const probedSound = $derived(probe?.outcome === 'ok' ? probe.streams.some((s) => s.audio) : null);

	// ---- Detection ----
	const enrich = $derived(hardware?.enrichmentsPossible !== false);
	const enrichWhy = $derived(
		enrich
			? undefined
			: t(
					hardware?.enrichmentsReason === 'memory'
						? 'nvr.camera.enrichMemory'
						: hardware?.enrichmentsReason === 'avx2'
							? 'nvr.camera.enrichAvx'
							: 'nvr.camera.enrichNo',
				),
	);
	type DetectPill = ObjectGroup | 'plates' | 'faces';
	const detectChosen = $derived<DetectPill[]>(
		detectOn
			? [
					...groupsOf(detectObjects),
					...(plates ? (['plates'] as const) : []),
					...(faces ? (['faces'] as const) : []),
				]
			: [],
	);
	function toggleDetect(value: DetectPill, on: boolean) {
		if (value === 'plates') plates = on;
		else if (value === 'faces') faces = on;
		else detectObjects = toggleGroup(detectOn ? detectObjects : [], value, on, objects);
		// Plates need a vehicle, faces a person to look at (docs/NVR.md).
		if (on && value === 'plates' && !groupsOf(detectObjects).includes('vehicles'))
			detectObjects = toggleGroup(detectObjects, 'vehicles', true, objects);
		if (on && value === 'faces' && !detectObjects.includes('person'))
			detectObjects = toggleGroup(detectObjects, 'persons', true, objects);
		detectOn = detectObjects.length > 0;
		if (!detectOn) {
			plates = false;
			faces = false;
			if (mode === 'events') mode = 'motion';
		}
	}
	function toggleObject(object: string, on: boolean) {
		const next = on ? [...detectObjects, object] : detectObjects.filter((o) => o !== object);
		detectObjects = objects.length ? objects.filter((o) => next.includes(o)) : next;
		detectOn = detectObjects.length > 0;
		if (!detectOn) {
			plates = false;
			faces = false;
			if (mode === 'events') mode = 'motion';
		}
	}
	// ---- Notifications ----
	const WAYS = ['titan', 'push', 'email', 'homeAssistant', 'agent'] as const;
	type Way = (typeof WAYS)[number];
	const waysChosen = $derived(WAYS.filter((way) => notify[way]));
	type KindPill = ObjectGroup | 'plate' | 'face' | 'offline';
	const KIND_PILLS: KindPill[] = [...OBJECT_GROUP_IDS, 'plate', 'face', 'offline'];
	const kindMembers = (pill: KindPill): readonly string[] =>
		pill === 'plate' || pill === 'face' || pill === 'offline' ? [pill] : OBJECT_GROUPS[pill];
	const kindsChosen = $derived(
		KIND_PILLS.filter((pill) => kindMembers(pill).some((k) => notify.kinds.includes(k))),
	);
	function toggleKind(pill: KindPill, on: boolean) {
		const members = kindMembers(pill);
		const rest = notify.kinds.filter((k) => !members.includes(k));
		const add = members.filter(
			(k) => ['plate', 'face', 'offline'].includes(k) || !objects.length || objects.includes(k),
		);
		notify.kinds = on ? [...rest, ...add] : rest;
	}

	// ---- Save ----
	let saving = $state(false);
	let error = $state('');
	/** The NAS said the name is taken (also by a camera this page does not list). */
	let nameError = $state(false);
	const nameTaken = $derived(
		name.trim() !== '' &&
			taken.some((other) => other.trim().toLocaleLowerCase() === name.trim().toLocaleLowerCase()),
	);
	const ready = $derived(name.trim() !== '' && mainURL.trim() !== '' && !nameTaken);
	async function save(event: SubmitEvent) {
		event.preventDefault();
		saving = true;
		error = '';
		const body: Record<string, unknown> = {
			name: name.trim(),
			source: {
				kind,
				host: kind === 'onvif' ? host.trim() : '',
				port: kind === 'onvif' ? (camera?.source?.port ?? 0) : 0,
				onvifPort: kind === 'onvif' ? Number(onvifPort) || 80 : 0,
				user: user.trim(),
				password,
				mainURL: mainURL.trim(),
				subURL: subURL.trim(),
				allowPublic,
			},
			detect: { enabled: detectOn, objects: detectOn ? detectObjects : [], plates, faces },
			record: {
				mode,
				retainDays: Number(retainDays) || 14,
				preSeconds: Number(preSeconds) || 0,
				postSeconds: Number(postSeconds) || 0,
			},
			audio: { enabled: audio, twoWay: audio && twoWay },
			ptz: kind === 'onvif' && ptz,
			notify: $state.snapshot(notify),
		};
		if (adding) body.enabled = true;
		try {
			const saved = adding
				? await api.post<Camera>('/nvr/cameras', body)
				: await api.put<Camera>(`/nvr/cameras/${encodeURIComponent(camera!.id)}`, body);
			onsaved(saved);
		} catch (err) {
			if (err instanceof ApiError && err.code === 'nvr_target_public') askPublic = true;
			if (err instanceof ApiError && err.code === 'nvr_camera_name_exists') nameError = true;
			error = apiErrorText(err);
		} finally {
			saving = false;
		}
	}
</script>

<form onsubmit={save} class="camera-form">
	{#if adding}
		<SettingGroup title={t('nvr.add.find')} id="{uid}-find">
			<SettingRow
				icon="search"
				title={t('nvr.add.search')}
				desc={t('nvr.add.searchHint')}
				id="nvr-add-search"
			>
				{#snippet control()}
					<button type="button" class="btn" disabled={searching} onclick={search}
						>{searching ? t('nvr.add.searching') : t('nvr.add.searchNow')}</button
					>
				{/snippet}
			</SettingRow>
			{#if searchError}<p class="notice-error" role="alert">{searchError}</p>{/if}
			{#if found}
				{#if found.length === 0}
					<p class="muted note" role="status">{t('nvr.add.none')}</p>
				{:else}
					<ul class="found" aria-label={t('nvr.add.results')}>
						{#each found as hit (hit.id || hit.address)}
							<li>
								<button
									type="button"
									class="hit"
									class:picked={kind === 'onvif' && host === hit.address}
									aria-pressed={kind === 'onvif' && host === hit.address}
									onclick={() => take(hit)}
								>
									<b>{hit.name || hit.hardware || hit.address}</b>
									<small class="mono"
										>{hit.address}{hit.hardware && hit.name ? ` · ${hit.hardware}` : ''}</small
									>
									{#if hit.known}<small class="muted">{t('nvr.add.known')}</small>{/if}
								</button>
							</li>
						{/each}
					</ul>
				{/if}
			{/if}
		</SettingGroup>
	{/if}

	<SettingGroup title={t('nvr.camera.connection')} id="{uid}-conn">
		<SettingRow icon="globe" title={t('nvr.camera.kind')} desc={t('nvr.camera.kindHint')}>
			{#snippet control()}
				<Segments
					options={[
						{ value: 'onvif', label: t('nvr.camera.kindOnvif') },
						{ value: 'rtsp', label: t('nvr.camera.kindRtsp') },
					]}
					bind:value={kind}
					label={t('nvr.camera.kind')}
					onchange={() => {
						probe = null;
						if (kind === 'rtsp') ptz = false;
					}}
				/>
			{/snippet}
		</SettingRow>
		{#if kind === 'onvif'}
			<SettingRow
				icon="camera"
				title={t('nvr.camera.host')}
				desc={t('nvr.camera.hostHint')}
				for="{uid}-host"
			>
				{#snippet control()}
					<input
						id="{uid}-host"
						class="field mono host"
						type="text"
						bind:value={host}
						placeholder="192.168.1.60"
						autocomplete="off"
						spellcheck="false"
						oninput={() => (probe = null)}
					/>
					<label class="port"
						><span>{t('nvr.camera.port')}</span>
						<input class="field mono" type="number" min="1" max="65535" bind:value={onvifPort} />
					</label>
				{/snippet}
			</SettingRow>
		{:else}
			<SettingRow
				icon="video"
				title={t('nvr.camera.mainUrl')}
				desc={t('nvr.camera.mainUrlHint')}
				for="{uid}-main"
			>
				{#snippet control()}
					<input
						id="{uid}-main"
						class="field mono url"
						type="text"
						bind:value={mainURL}
						placeholder="rtsp://192.168.1.60:554/stream1"
						autocomplete="off"
						spellcheck="false"
						oninput={() => (probe = null)}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="video"
				title={t('nvr.camera.subUrl')}
				desc={t('nvr.camera.subUrlHint')}
				for="{uid}-sub"
			>
				{#snippet control()}
					<input
						id="{uid}-sub"
						class="field mono url"
						type="text"
						bind:value={subURL}
						placeholder="rtsp://192.168.1.60:554/stream2"
						autocomplete="off"
						spellcheck="false"
					/>
				{/snippet}
			</SettingRow>
		{/if}
		<SettingRow icon="key" title={t('nvr.camera.login')} desc={t('nvr.camera.loginHint')}>
			{#snippet control()}
				<input
					class="field cred"
					type="text"
					bind:value={user}
					aria-label={t('nvr.camera.user')}
					placeholder={t('nvr.camera.user')}
					autocomplete="off"
					spellcheck="false"
				/>
				<input
					class="field cred"
					type="password"
					bind:value={password}
					aria-label={t('nvr.camera.password')}
					placeholder={camera?.source?.hasPassword
						? t('nvr.camera.passwordKeep')
						: t('nvr.camera.password')}
					autocomplete="new-password"
				/>
			{/snippet}
		</SettingRow>
		{#if askPublic}
			<SettingRow icon="shield" title={t('nvr.camera.public')} desc={t('nvr.camera.publicHint')}>
				{#snippet control()}
					<input
						type="checkbox"
						role="switch"
						class="switch"
						aria-label={t('nvr.camera.public')}
						bind:checked={allowPublic}
					/>
				{/snippet}
			</SettingRow>
		{/if}
		<SettingRow icon="image" title={t('nvr.probe.title')} id="nvr-probe">
			{#snippet description()}
				{#if probing}<span role="status">{t('nvr.probe.running')}</span>
				{:else if probeError}<span class="bad" role="alert">{probeError}</span>
				{:else if probe}
					<span role="status" class:bad={probe.outcome !== 'ok'}>{probeText}</span>
					{#if probe.outcome === 'ok'}
						<ul class="streams">
							{#if probe.device}<li>
									{[probe.device.manufacturer, probe.device.model].filter(Boolean).join(' ')}
								</li>{/if}
							{#each probe.streams as s (s.role)}<li>{streamText(s)}</li>{/each}
							{#if kind === 'onvif'}<li>
									{probe.ptz ? t('nvr.probe.ptzYes') : t('nvr.probe.ptzNo')}
								</li>{/if}
							{#if probe.streams.some((s) => s.role === 'sub' && s.outcome === 'ok')}<li>
									{t('nvr.probe.subUsed')}
								</li>{/if}
						</ul>
					{/if}
				{:else}{t('nvr.probe.hint')}{/if}
			{/snippet}
			{#snippet control()}
				{#if probe?.outcome === 'ok'}<span class="chip ok">{t('nvr.probe.connected')}</span>{/if}
				<button type="button" class="btn" disabled={probing || !canProbe} onclick={check}
					>{t('nvr.probe.check')}</button
				>
			{/snippet}
		</SettingRow>
		<SettingRow icon="edit" title={t('nvr.camera.name')} for="{uid}-name">
			{#snippet description()}
				{#if nameTaken || nameError}<span class="bad" id="{uid}-name-taken" role="alert"
						>{t('nvr.camera.nameExists')}</span
					>{/if}
			{/snippet}
			{#snippet control()}
				<input
					id="{uid}-name"
					class="field name"
					type="text"
					bind:value={name}
					maxlength="60"
					required
					autocomplete="off"
					aria-invalid={nameTaken || nameError ? 'true' : undefined}
					aria-describedby={nameTaken || nameError ? `${uid}-name-taken` : undefined}
					oninput={() => (nameError = false)}
				/>
			{/snippet}
		</SettingRow>
	</SettingGroup>

	<SettingGroup title={t('nvr.camera.detection')} id="{uid}-detect">
		<SettingRow
			icon="eye"
			title={t('nvr.detect.title')}
			desc={enrich ? t('nvr.detect.hint') : `${t('nvr.detect.hint')} ${enrichWhy}`}
			id="nvr-detect"
		>
			{#snippet control()}
				<Pills
					label={t('nvr.detect.title')}
					options={[
						...OBJECT_GROUP_IDS.map((group) => ({
							value: group as DetectPill,
							label: t(`nvr.detect.${group}` as MessageKey),
						})),
						{
							value: 'plates' as DetectPill,
							label: t('nvr.detect.plates'),
							disabled: !enrich && !plates,
							why: enrichWhy,
						},
						{
							value: 'faces' as DetectPill,
							label: t('nvr.detect.faces'),
							disabled: !enrich && !faces,
							why: enrichWhy,
						},
					]}
					chosen={detectChosen}
					ontoggle={toggleDetect}
				/>
			{/snippet}
			<SettingSub title={t('nvr.detect.single')} desc={t('nvr.detect.singleHint')}>
				<div class="objects">
					{#each objects as object (object)}
						<label
							><input
								type="checkbox"
								checked={detectOn && detectObjects.includes(object)}
								onchange={(e) => toggleObject(object, e.currentTarget.checked)}
							/>
							{labelName(object)}</label
						>
					{/each}
				</div>
			</SettingSub>
		</SettingRow>
	</SettingGroup>

	<SettingGroup title={t('nvr.camera.recording')} id="{uid}-record">
		<SettingRow
			icon="video"
			title={t('nvr.record.title')}
			desc={t(`nvr.record.hint.${mode}` as MessageKey)}
			for="{uid}-mode"
		>
			{#snippet control()}
				<select id="{uid}-mode" class="field" bind:value={mode}>
					<option value="events" disabled={!detectOn}>{t('nvr.record.events')}</option>
					<option value="motion">{t('nvr.record.motion')}</option>
					<option value="continuous">{t('nvr.record.continuous')}</option>
					<option value="off">{t('nvr.record.off')}</option>
				</select>
			{/snippet}
		</SettingRow>
		{#if mode !== 'off'}
			<SettingRow
				icon="clock"
				title={t('nvr.record.retain')}
				desc={t('nvr.record.retainHint')}
				for="{uid}-days"
			>
				{#snippet control()}
					<label class="unit"
						><input
							id="{uid}-days"
							class="field num"
							type="number"
							min="1"
							max="365"
							bind:value={retainDays}
						/><span>{t('nvr.record.days')}</span></label
					>
				{/snippet}
			</SettingRow>
			{#if mode !== 'continuous'}
				<SettingRow
					icon="clock"
					title={t('nvr.record.around')}
					desc={mode === 'events'
						? `${t('nvr.record.aroundHint')} ${t('nvr.record.aroundEvents')}`
						: t('nvr.record.aroundHint')}
				>
					{#snippet control()}
						<label class="unit around"
							><span>{t('nvr.record.before')}</span><input
								class="field num"
								type="number"
								min="0"
								max="60"
								bind:value={preSeconds}
							/></label
						>
						<label class="unit around"
							><span>{t('nvr.record.after')}</span><input
								class="field num"
								type="number"
								min="0"
								max="60"
								bind:value={postSeconds}
							/></label
						>
					{/snippet}
				</SettingRow>
			{/if}
		{/if}
	</SettingGroup>

	<SettingGroup title={t('nvr.camera.soundMove')} id="{uid}-sound">
		<SettingRow
			icon="volume"
			title={t('nvr.audio.title')}
			desc={probedSound === false ? t('nvr.audio.none') : t('nvr.audio.hint')}
		>
			{#snippet control()}
				<input
					type="checkbox"
					role="switch"
					class="switch"
					aria-label={t('nvr.audio.title')}
					bind:checked={audio}
					onchange={() => {
						if (!audio) twoWay = false;
					}}
				/>
			{/snippet}
		</SettingRow>
		<SettingRow
			icon="mic"
			title={t('nvr.audio.twoWay')}
			desc={audio ? t('nvr.audio.twoWayHint') : t('nvr.audio.twoWayNeedsSound')}
		>
			{#snippet control()}
				<input
					type="checkbox"
					role="switch"
					class="switch"
					aria-label={t('nvr.audio.twoWay')}
					bind:checked={twoWay}
					disabled={!audio}
				/>
			{/snippet}
		</SettingRow>
		<SettingRow
			icon="move"
			title={t('nvr.ptz.title')}
			desc={kind !== 'onvif'
				? t('nvr.ptz.needsOnvif')
				: probe?.outcome === 'ok' && !probe.ptz
					? t('nvr.ptz.notFound')
					: t('nvr.ptz.hint')}
		>
			{#snippet control()}
				<input
					type="checkbox"
					role="switch"
					class="switch"
					aria-label={t('nvr.ptz.title')}
					bind:checked={ptz}
					disabled={kind !== 'onvif' || (probe?.outcome === 'ok' && !probe.ptz)}
				/>
			{/snippet}
		</SettingRow>
	</SettingGroup>

	<SettingGroup title={t('nvr.camera.notifications')} id="{uid}-notify">
		<SettingRow icon="bell" title={t('nvr.notify.ways')} desc={t('nvr.notify.waysHint')}>
			{#snippet control()}
				<Pills
					label={t('nvr.notify.ways')}
					options={WAYS.map((way) => ({
						value: way as Way,
						label: t(`nvr.notify.way.${way}` as MessageKey),
					}))}
					chosen={waysChosen}
					ontoggle={(way, on) => (notify[way] = on)}
				/>
			{/snippet}
		</SettingRow>
		<SettingRow icon="info" title={t('nvr.notify.kinds')} desc={t('nvr.notify.kindsHint')}>
			{#snippet control()}
				<Pills
					label={t('nvr.notify.kinds')}
					options={KIND_PILLS.map((pill) => ({
						value: pill,
						label: t(`nvr.notify.kind.${pill}` as MessageKey),
					}))}
					chosen={kindsChosen}
					disabled={waysChosen.length === 0}
					ontoggle={toggleKind}
				/>
			{/snippet}
		</SettingRow>
	</SettingGroup>

	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if !mainURL.trim() && kind === 'onvif'}<p class="muted note">
			{t('nvr.camera.checkFirst')}
		</p>{/if}
	<div class="form-actions">
		<button type="button" class="btn" disabled={saving} onclick={oncancel}
			>{t('common.cancel')}</button
		>
		<button type="submit" class="btn btn-primary" disabled={saving || !ready}
			>{#if adding}<Icon name="plus" size={15} />{/if}{adding
				? t('nvr.add.submit')
				: t('common.save')}</button
		>
	</div>
</form>

<style>
	.camera-form {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
		min-width: 0;
	}
	.found {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 15rem), 1fr));
		gap: var(--space-2);
	}
	.hit {
		width: 100%;
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: var(--space-2-5) var(--space-3);
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface);
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
		overflow-wrap: anywhere;
	}
	.hit.picked {
		border-color: var(--accent);
		box-shadow: inset 0 0 0 1px var(--accent);
	}
	.hit small {
		color: var(--muted);
	}
	.note {
		margin: 0;
	}
	.host {
		width: 13rem;
	}
	.url {
		width: min(26rem, 100%);
	}
	.cred {
		width: 9.5rem;
	}
	.name {
		width: 16rem;
	}
	.num {
		width: 5.5rem;
	}
	.port,
	.unit {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.port .field {
		width: 5.5rem;
	}
	/* "davor" and "danach" line their fields up when they stand below each other. */
	.around span {
		min-width: 3.6em;
	}
	.bad {
		color: var(--error-text);
	}
	.streams {
		margin: var(--space-1) 0 0;
		padding-left: 1.1rem;
		overflow-wrap: anywhere;
	}
	.objects {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2) var(--space-4);
	}
	.objects label {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	.form-actions {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: var(--space-2);
	}
	.form-actions .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	@media (max-width: 640px) {
		.host,
		.url,
		.cred,
		.name {
			width: 100%;
		}
	}
</style>
