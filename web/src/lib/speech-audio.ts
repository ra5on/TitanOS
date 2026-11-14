// Audio for the speech services of the NAS (PLAN 92): what the browser
// recorded (any format it chose) becomes a WAV file of 16 kHz, 16 bit,
// mono – what speech to text takes, without a converter on the NAS.

/** Turns a recording into a WAV file of 16 kHz, 16 bit, mono. */
export async function toWav16k(recording: Blob): Promise<Blob> {
	const Ctx =
		window.AudioContext ??
		(window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
	const ctx = new Ctx();
	let decoded: AudioBuffer;
	try {
		decoded = await ctx.decodeAudioData(await recording.arrayBuffer());
	} finally {
		void ctx.close();
	}
	const rate = 16000;
	const frames = Math.max(1, Math.ceil(decoded.duration * rate));
	const offline = new OfflineAudioContext(1, frames, rate);
	const source = offline.createBufferSource();
	source.buffer = decoded;
	source.connect(offline.destination);
	source.start();
	const mono = (await offline.startRendering()).getChannelData(0);
	return pcmWav(mono, rate);
}

/** A WAV file from samples between -1 and 1. */
export function pcmWav(samples: Float32Array, rate: number): Blob {
	const data = new DataView(new ArrayBuffer(44 + samples.length * 2));
	const text = (at: number, s: string) =>
		[...s].forEach((c, i) => data.setUint8(at + i, c.charCodeAt(0)));
	text(0, 'RIFF');
	data.setUint32(4, 36 + samples.length * 2, true);
	text(8, 'WAVEfmt ');
	data.setUint32(16, 16, true);
	data.setUint16(20, 1, true);
	data.setUint16(22, 1, true);
	data.setUint32(24, rate, true);
	data.setUint32(28, rate * 2, true);
	data.setUint16(32, 2, true);
	data.setUint16(34, 16, true);
	text(36, 'data');
	data.setUint32(40, samples.length * 2, true);
	for (let i = 0; i < samples.length; i++) {
		const s = Math.max(-1, Math.min(1, samples[i]));
		data.setInt16(44 + i * 2, s < 0 ? s * 0x8000 : s * 0x7fff, true);
	}
	return new Blob([data], { type: 'audio/wav' });
}

/** A recording from the microphone; stop() ends it and gives what was said. */
export async function record(): Promise<{ stop: () => Promise<Blob> }> {
	const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
	const type = ['audio/webm', 'audio/mp4', 'audio/ogg'].find(
		(t) => typeof MediaRecorder !== 'undefined' && MediaRecorder.isTypeSupported?.(t),
	);
	const recorder = new MediaRecorder(stream, type ? { mimeType: type } : undefined);
	const chunks: Blob[] = [];
	recorder.ondataavailable = (e) => e.data.size && chunks.push(e.data);
	recorder.start();
	return {
		stop: () =>
			new Promise((resolve) => {
				recorder.onstop = () => {
					stream.getTracks().forEach((track) => track.stop());
					resolve(new Blob(chunks, { type: recorder.mimeType || type || 'audio/webm' }));
				};
				recorder.stop();
			}),
	};
}
