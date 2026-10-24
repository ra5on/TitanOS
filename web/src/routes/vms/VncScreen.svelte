<script lang="ts">
	// The screen of a running VM: noVNC in the browser, a WebSocket to
	// titand, QEMU's VNC server behind it.
	import { untrack } from 'svelte';
	import RFB from '@novnc/novnc';

	export type ScaleMode = 'scale' | 'remote' | 'full';
	export type ConnectionPhase = 'connecting' | 'connected' | 'lost';

	// resizable: the guest can take a resolution from the browser. That needs
	// the virtio graphics of the Linux template; the plain VGA of Windows and
	// other systems cannot, so their picture is scaled instead.
	type Props = {
		vm: string;
		mode: ScaleMode;
		resizable: boolean;
		onstatus?: (phase: ConnectionPhase) => void;
	};
	let { vm, mode, resizable, onstatus = () => {} }: Props = $props();

	let target = $state<HTMLElement>();

	let rfb: RFB | undefined;
	const effective = $derived<ScaleMode>(mode === 'remote' && !resizable ? 'scale' : mode);

	function applyMode() {
		if (!rfb) return;
		rfb.resizeSession = effective === 'remote';
		rfb.scaleViewport = effective !== 'full';
		rfb.clipViewport = false;
	}

	$effect(() => {
		if (!target) return;
		const element = target;
		let retry: ReturnType<typeof setTimeout> | undefined;
		let closed = false;

		const connect = () => {
			onstatus('connecting');
			const connection = new RFB(
				element,
				`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/v1/vms/${vm}/screen`,
			);
			rfb = connection;
			connection.background = 'var(--screen-bg)';
			applyMode();
			connection.addEventListener('connect', () => {
				onstatus('connected');
				connection.focus({ preventScroll: true });
			});
			connection.addEventListener('disconnect', () => {
				rfb = undefined;
				if (closed) return;
				// The VM restarts, or it is off; the page above knows which.
				onstatus('lost');
				retry = setTimeout(connect, 2000);
			});
		};
		// Only the element decides when to connect anew: neither the display
		// mode nor the state of the connection may start this over.
		untrack(connect);

		return () => {
			closed = true;
			clearTimeout(retry);
			rfb?.disconnect();
			rfb = undefined;
		};
	});

	$effect(() => {
		effective;
		applyMode();
	});

	export function ctrlAltDel() {
		rfb?.sendCtrlAltDel();
		rfb?.focus({ preventScroll: true });
	}

	/** Presses keys together, as X keysyms with their key codes, and lets go in reverse. */
	export function sendKeys(keys: [number, string][]) {
		if (!rfb) return;
		for (const [keysym, code] of keys) rfb.sendKey(keysym, code, true);
		for (const [keysym, code] of [...keys].reverse()) rfb.sendKey(keysym, code, false);
		rfb.focus({ preventScroll: true });
	}

	/** Types text into the VM, as the clipboard of the guest. */
	export function paste(text: string) {
		rfb?.clipboardPasteFrom(text);
		rfb?.focus({ preventScroll: true });
	}

	/** The picture as it is right now, as a PNG. */
	export function screenshot(): string {
		return rfb?.toDataURL('image/png') ?? '';
	}
</script>

<div class="screen" class:full={effective === 'full'} bind:this={target}></div>

<style>
	.screen {
		position: absolute;
		inset: 0;
		overflow: hidden;
		background: var(--screen-bg);
	}
	/* Pixel for pixel: a picture larger than the window scrolls. */
	.screen.full {
		overflow: auto;
	}
</style>
