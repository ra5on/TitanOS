<script lang="ts">
	import { onDestroy } from 'svelte';
	import VoiceSpectrum from './VoiceSpectrum.svelte';
	import { api, ApiError, apiFetch, responseError } from '#lib/api.ts';
	import { apiErrorText, i18n } from '#lib/i18n/index.svelte.ts';
	import { toWav16k } from '#lib/speech-audio.ts';
	import { openInProgram } from '#lib/agent/open.ts';
	import { msg, reason } from './labels.ts';
	import { changeJob, type Job, type Capability, requestId } from './api.ts';
	import { modelStatus, type LocalModel } from './model.ts';
	import { VoiceAudio } from './voice-audio.ts';
	import Sheet from './Sheet.svelte';

	let {
		calm,
		caps,
		admin,
		onclose,
		ontext,
		onturn,
		onupdate,
		onsettings,
		model,
		onmodel,
	}: {
		calm: boolean;
		caps: Capability[];
		admin: boolean;
		onclose: () => void;
		ontext: (text: string) => void;
		onturn: (text: string, id: string) => Promise<Job>;
		onupdate: (j: Job) => void;
		onsettings: () => void;
		model: LocalModel;
		onmodel: () => void;
	} = $props();
	type Phase =
		| 'idle'
		| 'opening'
		| 'listening'
		| 'recorded'
		| 'transcribing'
		| 'waiting'
		| 'permission'
		| 'playConsent'
		| 'speaking'
		| 'next'
		| 'stopped';
	let phase = $state<Phase>('idle'),
		mode = $state<'dialog' | 'dictation'>('dialog'),
		error = $state(''),
		heard = $state(''),
		reply = $state(''),
		job = $state<Job>();
	const needed = $derived(
		mode === 'dialog' ? ['speech.transcribe', 'chat.ask', 'speech.speak'] : ['speech.transcribe'],
	);
	const blocked = $derived(
		needed.map((id) => caps.find((c) => c.id === id)).filter((c) => !c || c.effective === 'never'),
	);
	const modelBlocked = $derived(mode === 'dialog' && !blocked.length && model.state !== 'ready');
	const enabled = $derived(!blocked.length && !modelBlocked);
	const needsSetup = $derived(blocked.some((c) => !c || c.reason === 'app'));
	const inSession = $derived(!['idle', 'stopped'].includes(phase));
	const audio = new VoiceAudio();
	const abort = new AbortController();
	let clip: Blob | null = null,
		closed = false;
	const asks = (tool: string) => caps.find((c) => c.id === tool)?.effective === 'ask';
	function leave() {
		if (closed) return;
		closed = true;
		abort.abort();
		audio.close();
	}
	function stop() {
		leave();
		onclose();
	}
	onDestroy(leave);
	$effect(() => {
		if (blocked.length && inSession) stop();
	});

	function failure(e: unknown) {
		if (closed) return;
		const name = e instanceof Error || e instanceof DOMException ? e.name : '';
		if (name === 'NotAllowedError' || name === 'PermissionDeniedError')
			error = msg('voiceBrowserPermission');
		else if (e instanceof ApiError) error = apiErrorText(e);
		else if (phase === 'speaking') error = msg('voicePlayback');
		else error = msg('voiceAudioError') + (name ? ' (' + name + ')' : '');
		phase = 'stopped';
	}
	async function listen() {
		if (closed || !enabled || !['idle', 'next', 'stopped'].includes(phase)) return;
		error = '';
		clip = null;
		phase = 'opening';
		try {
			if (!navigator.mediaDevices?.getUserMedia || !window.AudioContext) {
				error = msg('voiceHTTPS');
				phase = 'stopped';
				return;
			}
			await audio.record(mode === 'dialog', (recording) => {
				if (closed) return;
				clip = recording;
				if (!clip) {
					error = msg('voiceNoSpeech');
					phase = 'stopped';
					return;
				}
				phase = 'recorded';
				if (mode === 'dialog' && !asks('speech.transcribe')) void useClip();
			});
			if (!closed) phase = 'listening';
		} catch (e) {
			audio.finish();
			audio.releaseInput();
			failure(e);
		}
	}
	async function useClip() {
		if (!clip || closed || phase !== 'recorded') return;
		phase = 'transcribing';
		try {
			const wav = await toWav16k(clip);
			clip = null;
			if (closed) return;
			const res = await apiFetch('/personal-agent/voice?lang=' + i18n.locale, {
				method: 'POST',
				headers: { 'Content-Type': 'audio/wav', 'X-TitanOS-Use-Recording': 'yes' },
				body: wav,
				signal: abort.signal,
			});
			if (!res.ok) throw await responseError(res);
			const data = (await res.json()) as { text: string };
			if (closed) return;
			const text = data.text.trim();
			if (!text) {
				error = msg('voiceNoSpeech');
				phase = 'stopped';
				return;
			}
			if (mode === 'dictation') {
				ontext(text);
				stop();
				return;
			}
			heard = text;
			reply = '';
			phase = 'waiting';
			job = await onturn(text, requestId());
			if (!closed) await follow(job);
		} catch (e) {
			failure(e);
		}
	}
	async function follow(initial: Job) {
		let current = initial;
		while (!closed) {
			job = current;
			onupdate(current);
			if (current.request.tool !== 'chat.ask') {
				error = msg('voiceCheckJob');
				phase = 'stopped';
				return;
			}
			if (current.status === 'waiting' && current.phase === 'permission' && current.token) {
				phase = 'permission';
				return;
			}
			if (current.status === 'done' && current.result?.text) {
				reply = current.result.text;
				if (asks('speech.speak')) {
					phase = 'playConsent';
					return;
				}
				await play();
				return;
			}
			if (!['planned', 'running'].includes(current.status)) {
				error = msg('voiceCheckJob');
				phase = 'stopped';
				return;
			}
			await new Promise((resolve) => setTimeout(resolve, 750));
			if (!closed)
				current = await api.get<Job>(
					'/personal-agent/jobs/' + encodeURIComponent(current.id),
					abort.signal,
				);
		}
	}
	async function permit() {
		if (!job?.token || phase !== 'permission' || job.request.tool !== 'chat.ask') return;
		phase = 'waiting';
		try {
			const updated = await changeJob(job.id, 'approve', job.token);
			await follow(updated);
		} catch (e) {
			failure(e);
		}
	}
	async function play() {
		if (!job || closed) return;
		phase = 'waiting';
		try {
			const res = await apiFetch('/personal-agent/jobs/' + encodeURIComponent(job.id) + '/speech', {
				method: 'POST',
				headers: { 'X-TitanOS-Play-Reply': 'yes' },
				signal: abort.signal,
			});
			if (!res.ok) throw await responseError(res);
			const wav = await res.arrayBuffer();
			if (closed) return;
			phase = 'speaking';
			await audio.play(wav);
			if (closed) return;
			phase = 'next';
			if (!asks('speech.transcribe')) await listen();
		} catch (e) {
			failure(e);
		}
	}
</script>

<svelte:window onpagehide={stop} />
<svelte:document
	onvisibilitychange={() => {
		if (document.hidden && inSession) stop();
	}}
/>
<Sheet title={msg(inSession && mode === 'dialog' ? 'voiceDialog' : 'mic')} onclose={stop} {calm}>
	<div class="pa-voice" data-guide="orb">
		<VoiceSpectrum
			listen={phase === 'listening' || phase === 'speaking'}
			spectrum={() => audio.spectrum()}
			{calm}
		/>
	</div>
	{#if !inSession}<div class="voice-modes">
			<button
				aria-pressed={mode === 'dialog'}
				class="pa-button"
				onclick={() => {
					mode = 'dialog';
					error = '';
				}}>{msg('voiceDialog')}</button
			>
			<button
				aria-pressed={mode === 'dictation'}
				class="pa-button"
				onclick={() => {
					mode = 'dictation';
					error = '';
				}}>{msg('voiceDictation')}</button
			>
		</div>{/if}
	<p class="pa-muted" role="status" data-voice-phase={phase}>
		{msg(
			phase === 'idle' || phase === 'stopped'
				? mode === 'dialog'
					? 'voiceExplain'
					: 'micExplain'
				: 'voicePhase.' + phase,
		)}
	</p>
	{#if heard && mode === 'dialog'}<p class="voice-turn"><b>{msg('you')}</b> {heard}</p>{/if}
	{#if reply && mode === 'dialog'}<p class="voice-turn"><b>Agent</b> {reply}</p>{/if}
	{#if error}<p role="alert">{error}</p>{/if}
	{#if modelBlocked}<div class="pa-warning">
			<p>{modelStatus(model)}</p>
			{#if model.state !== 'checking'}<button
					class="pa-button"
					onclick={() => {
						stop();
						onmodel();
					}}>{msg('model')}</button
				>{/if}
		</div>{:else if !enabled}<div class="pa-warning">
			<p>{needsSetup ? msg('voiceSetup') : msg('voiceDenied')}</p>
			{#each blocked as c}{#if c}<p>{c.title}{reason(c) ? ' · ' + reason(c) : ''}</p>{/if}{/each}
			{#if admin && needsSetup}<button
					class="pa-button"
					onclick={() => {
						stop();
						openInProgram('/settings/speech');
					}}>{msg('voiceServices')}</button
				>{/if}
			<button
				class="pa-button"
				onclick={() => {
					stop();
					onsettings();
				}}>{msg('permissions')}</button
			>
		</div>
	{:else if phase === 'idle' || phase === 'next' || phase === 'stopped'}<button
			class="pa-primary"
			onclick={listen}>{msg(mode === 'dialog' ? 'voiceStart' : 'micStart')}</button
		>
	{:else if phase === 'listening'}<button class="pa-primary" onclick={() => audio.finish()}
			>{msg('stopRecording')}</button
		>
	{:else if phase === 'recorded'}<button class="pa-primary" onclick={useClip}
			>{msg('micStop')}</button
		>
	{:else if phase === 'permission'}<button class="pa-primary" onclick={permit}
			>{msg('voiceAllowLocal')}</button
		>
	{:else if phase === 'playConsent'}<button class="pa-primary" onclick={play}
			>{msg('voicePlay')}</button
		>{/if}
	{#if inSession || job}<button class="pa-button" onclick={stop}>{msg('voiceEnd')}</button>{/if}
</Sheet>

<style>
	.pa-voice {
		margin: auto;
		width: min(156px, 58%);
		aspect-ratio: 1;
	}
	.voice-modes {
		display: flex;
		justify-content: center;
		gap: 0.4rem;
		flex-wrap: wrap;
	}
	.voice-modes [aria-pressed='true'] {
		outline: 2px solid var(--pa-accent);
	}
	.voice-turn {
		max-height: 6lh;
		overflow: auto;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	button {
		min-height: 44px;
	}
</style>
