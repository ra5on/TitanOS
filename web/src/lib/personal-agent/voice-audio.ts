// One turn owns one microphone stream. Playback and recording never overlap.
import { pcmWav } from '#lib/speech-audio.ts';

export class VoiceAudio {
	private output?: AudioContext;
	private stream?: MediaStream;
	private recorder?: MediaRecorder;
	private meter?: AnalyserNode;
	private finishPCM?: () => void;
	private playback?: AudioBufferSourceNode;
	private timer?: ReturnType<typeof setInterval>;
	private closed = false;
	private bins = new Uint8Array(128);

	async enable() {
		this.output ??= new AudioContext();
		if (this.output.state !== 'running') await this.output.resume();
	}

	async record(autoEnd: boolean, finished: (audio: Blob | null) => void) {
		// Open both from the user's gesture. Keep a reference to a late grant so
		// leaving the sheet or a failed playback context always releases its tracks.
		const opening = navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true } });
		let stream: MediaStream;
		try {
			[stream] = await Promise.all([opening, this.enable()]);
		} catch (error) {
			void opening.then(
				(s) => s.getTracks().forEach((t) => t.stop()),
				() => {},
			);
			throw error;
		}
		if (this.closed) {
			stream.getTracks().forEach((track) => track.stop());
			return;
		}
		this.stream = stream;
		const ctx = this.output!;
		this.meter = ctx.createAnalyser();
		this.meter.fftSize = 256;
		const input = ctx.createMediaStreamSource(stream);
		input.connect(this.meter);
		let silent = false;
		const done = (clip: Blob | null) => {
			this.releaseInput();
			input.disconnect();
			if (!this.closed) finished(silent ? null : clip);
		};
		if (window.MediaRecorder) {
			const mimeType = ['audio/webm', 'audio/mp4', 'audio/ogg'].find((v) =>
				MediaRecorder.isTypeSupported(v),
			);
			const recorder = new MediaRecorder(stream, mimeType ? { mimeType } : undefined);
			this.recorder = recorder;
			const chunks: Blob[] = [];
			recorder.ondataavailable = ({ data }) => {
				if (data.size) chunks.push(data);
			};
			recorder.onstop = () =>
				done(chunks.length ? new Blob(chunks, { type: recorder.mimeType }) : null);
			recorder.start();
		} else {
			// On WebKit builds without a recording codec, collect bounded mono PCM.
			// The muted output clocks this input node without playing the microphone.
			const tap = ctx.createScriptProcessor(2048, 1, 1);
			const mute = ctx.createGain();
			mute.gain.value = 0;
			input.connect(tap);
			tap.connect(mute);
			mute.connect(ctx.destination);
			const chunks: Float32Array[] = [];
			let frames = 0;
			tap.onaudioprocess = ({ inputBuffer }) => {
				const samples = inputBuffer.getChannelData(0);
				const count = Math.min(samples.length, Math.ceil(ctx.sampleRate * 30) - frames);
				if (count > 0) {
					chunks.push(samples.slice(0, count));
					frames += count;
				}
			};
			this.finishPCM = () => {
				this.finishPCM = undefined;
				tap.onaudioprocess = null;
				tap.disconnect();
				mute.disconnect();
				if (this.closed || !frames) {
					done(null);
					return;
				}
				const samples = new Float32Array(frames);
				let offset = 0;
				for (const chunk of chunks) {
					samples.set(chunk, offset);
					offset += chunk.length;
				}
				done(pcmWav(samples, ctx.sampleRate));
			};
		}
		const start = performance.now();
		let lastSpeech = 0;
		const samples = new Float32Array(256);
		this.timer = setInterval(() => {
			const now = performance.now();
			if (autoEnd && this.meter) {
				this.meter.getFloatTimeDomainData(samples);
				let energy = 0;
				for (const sample of samples) energy += sample * sample;
				if (energy / samples.length > 0.0004) lastSpeech = now;
				if (!lastSpeech && now - start > 12000) silent = true;
			}
			if (silent || (autoEnd && lastSpeech && now - lastSpeech > 1250) || now - start >= 30000)
				this.finish();
		}, 120);
	}

	spectrum() {
		if (!this.meter) return null;
		this.meter.getByteFrequencyData(this.bins);
		return this.bins;
	}
	finish() {
		if (this.recorder?.state === 'recording') this.recorder.stop();
		this.finishPCM?.();
	}
	async play(wav: ArrayBuffer) {
		const ctx = this.output;
		if (!ctx || this.closed) return;
		const buffer = await ctx.decodeAudioData(wav);
		if (this.closed) return;
		if (ctx.state !== 'running') throw new Error('playback_suspended');
		const source = ctx.createBufferSource();
		this.playback = source;
		source.buffer = buffer;
		this.meter = ctx.createAnalyser();
		this.meter.fftSize = 256;
		source.connect(this.meter);
		this.meter.connect(ctx.destination);
		await new Promise<void>((resolve) => {
			source.onended = () => resolve();
			source.start();
		});
		this.meter?.disconnect();
		this.meter = undefined;
		this.playback = undefined;
	}
	releaseInput() {
		clearInterval(this.timer);
		this.stream?.getTracks().forEach((track) => track.stop());
		this.stream = undefined;
		this.recorder = undefined;
		this.meter = undefined;
	}
	close() {
		this.closed = true;
		this.finish();
		this.releaseInput();
		this.playback?.stop();
		if (this.output && this.output.state !== 'closed') void this.output.close().catch(() => {});
	}
}
