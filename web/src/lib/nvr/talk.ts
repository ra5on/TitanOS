// PLAN 79-7: talking to a camera (push to talk). titand passes go2rtc's
// WebRTC signalling for the stream "<id>_twoway" through (docs/NVR.md
// "Gegensprechen"): {"type":"webrtc/offer","value":sdp} →
// {"type":"webrtc/answer",…}, "webrtc/candidate" both ways. The sound itself
// goes straight to the NAS (port 8555), not through titand. Browsers hand
// out the microphone only on https or localhost.
import { apiSocket } from '../cluster-node.ts';

export type TalkProblem =
	| 'insecure' // the page is not served over https
	| 'denied' // the person or the browser refused the microphone
	| 'no_microphone'
	| 'unsupported'
	| 'failed'; // signalling or the connection to the NAS failed

/** Why talking cannot work in this browser at all; '' when it can. */
export function talkBlocked(): TalkProblem | '' {
	if (typeof window === 'undefined') return 'unsupported';
	if (!window.isSecureContext) return 'insecure';
	if (!navigator.mediaDevices?.getUserMedia || typeof RTCPeerConnection === 'undefined')
		return 'unsupported';
	return '';
}

export type Talk = {
	/** The microphone is sent only while this is true. */
	speak: (on: boolean) => void;
	close: () => void;
};

/**
 * Opens the microphone and the connection to the camera. Resolves once the
 * camera can hear; the microphone stays muted until speak(true).
 */
export async function openTalk(
	camera: string,
	onlost: (problem: TalkProblem) => void,
): Promise<Talk> {
	const blocked = talkBlocked();
	if (blocked) throw blocked;
	let stream: MediaStream;
	try {
		stream = await navigator.mediaDevices.getUserMedia({
			audio: { echoCancellation: true, noiseSuppression: true },
		});
	} catch (err) {
		const name = (err as DOMException)?.name;
		throw (
			name === 'NotFoundError' || name === 'OverconstrainedError' ? 'no_microphone' : 'denied'
		) satisfies TalkProblem;
	}
	const track = stream.getAudioTracks()[0];
	track.enabled = false;

	const pc = new RTCPeerConnection({ iceServers: [] });
	const ws = new WebSocket(apiSocket(`/nvr/cameras/${encodeURIComponent(camera)}/talk`));
	let closed = false;
	const close = () => {
		if (closed) return;
		closed = true;
		track.stop();
		ws.onclose = ws.onmessage = ws.onerror = null;
		try {
			ws.close();
		} catch {
			/* already closed */
		}
		pc.close();
	};
	const lost = (problem: TalkProblem) => {
		if (closed) return;
		close();
		onlost(problem);
	};

	pc.addTransceiver(track, { direction: 'sendonly', streams: [stream] });
	pc.onicecandidate = (event) => {
		if (ws.readyState === WebSocket.OPEN)
			ws.send(
				JSON.stringify({ type: 'webrtc/candidate', value: event.candidate?.candidate ?? '' }),
			);
	};
	pc.onconnectionstatechange = () => {
		if (pc.connectionState === 'failed' || pc.connectionState === 'closed') lost('failed');
	};

	await new Promise<void>((resolve, reject) => {
		const timer = setTimeout(() => reject('failed' satisfies TalkProblem), 15000);
		const fail = () => {
			clearTimeout(timer);
			reject('failed' satisfies TalkProblem);
		};
		ws.onerror = fail;
		ws.onclose = fail;
		ws.onopen = async () => {
			try {
				const offer = await pc.createOffer();
				await pc.setLocalDescription(offer);
				ws.send(JSON.stringify({ type: 'webrtc/offer', value: offer.sdp }));
			} catch {
				fail();
			}
		};
		ws.onmessage = async (event) => {
			if (typeof event.data !== 'string') return;
			let msg: { type?: string; value?: string; t?: string };
			try {
				msg = JSON.parse(event.data);
			} catch {
				return;
			}
			try {
				if (msg.type === 'webrtc/answer' && msg.value) {
					await pc.setRemoteDescription({ type: 'answer', sdp: msg.value });
					clearTimeout(timer);
					resolve();
				} else if (msg.type === 'webrtc/candidate' && msg.value)
					await pc.addIceCandidate({ candidate: msg.value, sdpMid: '0' });
				else if (msg.t === 'error' || msg.t === 'bye' || msg.type === 'error') fail();
			} catch {
				fail();
			}
		};
	}).catch((problem: TalkProblem) => {
		close();
		throw problem;
	});
	ws.onclose = () => lost('failed');
	ws.onerror = () => lost('failed');

	return {
		speak: (on) => {
			if (!closed) track.enabled = on;
		},
		close,
	};
}
