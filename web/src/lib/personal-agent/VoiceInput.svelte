<script lang="ts">
	import { onDestroy } from 'svelte';
	import VoiceSpectrum from './VoiceSpectrum.svelte';
	import { apiFetch, responseError } from '#lib/api.ts';
	import { apiErrorText, i18n } from '#lib/i18n/index.svelte.ts';
	import { toWav16k } from '#lib/speech-audio.ts';
	import { msg } from './labels.ts';
	import Sheet from './Sheet.svelte';
	let {
		calm,
		onclose,
		ontext,
	}: { calm: boolean; onclose: () => void; ontext: (text: string) => void } = $props();
	let phase = $state<'rest' | 'listen' | 'ready' | 'think'>('rest'),
		error = $state('');
	let stream: MediaStream | null = null,
		ctx: AudioContext | null = null,
		meter: AnalyserNode | null = null,
		recorder: MediaRecorder | null = null;
	let chunks: Blob[] = [],
		bands = new Uint8Array(128),
		timer: ReturnType<typeof setTimeout> | undefined,
		closed = false,
		opening = false;
	const abort = new AbortController();
	function stopTracks() {
		stream?.getTracks().forEach((track) => track.stop());
		stream = null;
		void ctx?.close().catch(() => {});
		ctx = null;
		meter = null;
		clearTimeout(timer);
	}
	function spectrum() {
		if (!meter) return null;
		meter.getByteFrequencyData(bands);
		return bands;
	}
	async function start() {
		if (opening || phase !== 'rest') return;
		opening = true;
		error = '';
		try {
			if (!navigator.mediaDevices?.getUserMedia || !window.MediaRecorder)
				throw Error(msg('micUnavailable'));
			const input = await navigator.mediaDevices.getUserMedia({ audio: true });
			if (closed) {
				input.getTracks().forEach((t) => t.stop());
				return;
			}
			stream = input;
			ctx = new AudioContext();
			meter = ctx.createAnalyser();
			meter.fftSize = 256;
			ctx.createMediaStreamSource(input).connect(meter);
			const type = ['audio/webm', 'audio/mp4', 'audio/ogg'].find((v) =>
				MediaRecorder.isTypeSupported(v),
			);
			recorder = new MediaRecorder(input, type ? { mimeType: type } : undefined);
			chunks = [];
			recorder.ondataavailable = (e) => {
				if (e.data.size) chunks.push(e.data);
			};
			recorder.onstop = () => {
				stopTracks();
				if (!closed) phase = 'ready';
			};
			recorder.start();
			phase = 'listen';
			timer = setTimeout(() => {
				if (recorder?.state === 'recording') recorder.stop();
			}, 30000);
		} catch (e) {
			stopTracks();
			if (!closed) error = apiErrorText(e);
		} finally {
			opening = false;
		}
	}
	async function transcribe() {
		phase = 'think';
		try {
			const wav = await toWav16k(new Blob(chunks, { type: recorder?.mimeType }));
			chunks = [];
			const res = await apiFetch('/personal-agent/voice?lang=' + i18n.locale, {
				method: 'POST',
				headers: { 'Content-Type': 'audio/wav', 'X-TitanOS-Use-Recording': 'yes' },
				body: wav,
				signal: abort.signal,
			});
			if (!res.ok) throw await responseError(res);
			const { text } = (await res.json()) as { text: string };
			if (!closed) {
				ontext(text.trim());
				onclose();
			}
		} catch (e) {
			if (!closed) {
				error = apiErrorText(e);
				phase = 'rest';
			}
		}
	}
	onDestroy(() => {
		closed = true;
		abort.abort();
		if (recorder?.state === 'recording') recorder.stop();
		stopTracks();
		chunks = [];
	});
</script>

<Sheet title={msg('mic')} {onclose} {calm}>
	<div class="pa-voice" data-guide="orb">
		<VoiceSpectrum listen={phase === 'listen'} {spectrum} {calm} />
	</div>
	<p class="pa-muted">{msg('micExplain')}</p>
	{#if error}<p role="alert">{error}</p>{/if}
	{#if phase === 'think'}<p role="status">{msg('transcribe')}</p>{:else}
		<button
			class="pa-primary"
			onclick={() =>
				phase === 'listen'
					? recorder?.stop()
					: phase === 'ready'
						? void transcribe()
						: void start()}
			>{msg(
				phase === 'listen' ? 'stopRecording' : phase === 'ready' ? 'micStop' : 'micStart',
			)}</button
		>
	{/if}
</Sheet>

<style>
	.pa-voice {
		margin: auto;
		width: min(180px, 70%);
		aspect-ratio: 1;
		--ag-warm: var(--pa-orb);
	}
</style>
