// PLAN 79-7: what happens right now – the NVR's feed (WebSocket /nvr/feed,
// docs/NVR.md "Live-Meldungen"): new and ending events and what each camera
// sees at the moment. One connection per page, shared by the live tiles and
// the events panel; it reconnects on its own and stays quiet on a NAS whose
// NVR has no feed yet.
import { api } from '../api.ts';
import { apiSocket } from '../cluster-node.ts';
import { PAGE_VISIBILITY, pageHidden } from '../embedded.ts';
import type { EventList, FeedMessage, NvrEvent } from './types.ts';

export type Activity = { motion: boolean; objects: string[] };

export const feed = $state<{
	/** Newest first. */
	events: NvrEvent[];
	activity: Record<string, Activity>;
	connected: boolean;
	/** The list of events could be read (false: this NAS has none to offer). */
	available: boolean;
}>({ events: [], activity: {}, connected: false, available: true });

const KEEP = 40;

function put(event: NvrEvent) {
	const rest = feed.events.filter((e) => e.id !== event.id);
	feed.events = [event, ...rest].sort((a, b) => b.start - a.start).slice(0, KEEP);
}

async function seed() {
	try {
		const list = await api.get<EventList>(`/nvr/events?limit=${KEEP}`);
		feed.events = list.events ?? [];
		feed.available = true;
	} catch {
		feed.available = false;
	}
}

/** Follows the feed while the page is shown; the result stops it. */
export function watchFeed(): () => void {
	let socket: WebSocket | null = null;
	let timer: ReturnType<typeof setTimeout> | undefined;
	let stopped = false;
	let wait = 1000;
	let final = false;

	function close() {
		clearTimeout(timer);
		const old = socket;
		socket = null;
		if (old) {
			old.onclose = old.onmessage = old.onerror = null;
			old.close();
		}
		feed.connected = false;
	}

	function connect() {
		if (stopped || final || socket || pageHidden()) return;
		let ws: WebSocket;
		try {
			ws = new WebSocket(apiSocket('/nvr/feed'));
		} catch {
			return;
		}
		socket = ws;
		ws.onopen = () => {
			feed.connected = true;
			wait = 1000;
		};
		ws.onmessage = (e) => {
			if (typeof e.data !== 'string') return;
			let msg: FeedMessage;
			try {
				msg = JSON.parse(e.data) as FeedMessage;
			} catch {
				return;
			}
			if (msg.t === 'event' && msg.event?.id) {
				put(msg.event);
				feed.available = true;
			} else if (msg.t === 'activity' && typeof msg.camera === 'string')
				feed.activity = {
					...feed.activity,
					[msg.camera]: { motion: msg.motion === true, objects: msg.objects ?? [] },
				};
			else if (msg.t === 'hello') {
				if (!msg.running) feed.activity = {};
			} else if (msg.t === 'bye') final = true;
		};
		ws.onclose = () => {
			if (socket !== ws) return;
			socket = null;
			feed.connected = false;
			if (stopped || final) return;
			// Slower each time: a NAS without the feed is asked twice a minute at most.
			timer = setTimeout(connect, wait);
			wait = Math.min(wait * 2, 30000);
		};
	}

	const visibility = () => {
		if (pageHidden()) close();
		else {
			void seed();
			connect();
		}
	};
	document.addEventListener('visibilitychange', visibility);
	document.addEventListener(PAGE_VISIBILITY, visibility);
	void seed();
	connect();
	return () => {
		stopped = true;
		close();
		document.removeEventListener('visibilitychange', visibility);
		document.removeEventListener(PAGE_VISIBILITY, visibility);
	};
}
