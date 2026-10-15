<script lang="ts">
	// The screen of a running VM: noVNC in the browser, a WebSocket to
	// titand, QEMU's VNC server behind it.
	import { untrack } from 'svelte';
	import RFB from '@novnc/novnc';
	import { t } from '#lib/i18n/index.svelte.ts';

	export type ScaleMode = 'scale' | 'remote' | 'full';

	type Props = { vm: string; mode: ScaleMode };
	let { vm, mode }: Props = $props();

	let target = $state<HTMLElement>();
	let phase = $state<'connecting' | 'connected' | 'lost'>('connecting');

	let rfb: RFB | undefined;

	function applyMode() {
		if (!rfb) return;
		// "remote" asks the VM to take the size of the window as its
		// resolution. Where the guest cannot do that, the picture is scaled.
		rfb.resizeSession = mode === 'remote';
		rfb.scaleViewport = mode !== 'full';
		rfb.clipViewport = false;
	}


	$effect(() => {
		if (!target) return;
		const element = target;
		let retry: ReturnType<typeof setTimeout> | undefined;
		let closed = false;

		const connect = () => {
			const connection = new RFB(element, `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/v1/vms/${vm}/screen`);
			rfb = connection;
			connection.background = 'transparent';
			applyMode();
			connection.addEventListener('connect', () => {
				phase = 'connected';
				connection.focus();
			});
			connection.addEventListener('disconnect', () => {
				rfb = undefined;
				if (closed) return;
				// The VM restarts, or it is off; the page above knows which.
				phase = 'lost';
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
		mode;
		applyMode();
	});

	export function ctrlAltDel() {
		rfb?.sendCtrlAltDel();
		rfb?.focus();
	}

	/** Types text into the VM, as the clipboard of the guest. */
	export function paste(text: string) {
		rfb?.clipboardPasteFrom(text);
		rfb?.focus();
	}

	/** The picture as it is right now, as a PNG. */
	export function screenshot(): string {
		return rfb?.toDataURL('image/png') ?? '';
	}
</script>

<div class="screen" class:full={mode === 'full'} bind:this={target}></div>
{#if phase !== 'connected'}
	<p class="state" role="status">{t(phase === 'lost' ? 'vms.screenLost' : 'vms.screenConnecting')}</p>
{/if}

<style>
	.screen {
		position: absolute;
		inset: 0;
		overflow: hidden;
	}
	/* Pixel for pixel: a picture larger than the window scrolls. */
	.screen.full {
		overflow: auto;
	}
	.state {
		position: absolute;
		inset: 0;
		display: flex;
		align-items: center;
		justify-content: center;
		color: var(--muted);
		font-size: 0.875rem;
		pointer-events: none;
	}
</style>
