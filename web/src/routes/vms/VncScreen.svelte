<script lang="ts">
	// The screen of a running VM: noVNC in the browser, a WebSocket to
	// titand, QEMU's VNC server behind it.
	import { untrack } from 'svelte';
	import RFB from '@novnc/novnc';
	import { t } from '#lib/i18n/index.svelte.ts';

	export type ScaleMode = 'scale' | 'remote' | 'full';

	// resizable: the guest can take a resolution from the browser. That needs
	// the virtio graphics of the Linux template; the plain VGA of Windows and
	// other systems cannot, so their picture is scaled instead.
	type Props = { vm: string; mode: ScaleMode; resizable: boolean };
	let { vm, mode, resizable }: Props = $props();

	let target = $state<HTMLElement>();
	let phase = $state<'connecting' | 'connected' | 'lost'>('connecting');
	// What the guest sends and how large it shows, in CSS pixels.
	let guest = $state({ width: 0, height: 0 });
	let shown = $state({ width: 0, height: 0 });

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

		// noVNC draws on one canvas: its attributes are the guest's resolution,
		// its box the size on screen.
		let canvas: HTMLCanvasElement | null = null;
		const measure = () => {
			canvas = element.querySelector('canvas');
			if (!canvas) return;
			guest = { width: canvas.width, height: canvas.height };
			const box = canvas.getBoundingClientRect();
			shown = { width: Math.round(box.width), height: Math.round(box.height) };
		};
		const watch = new MutationObserver(measure);
		const sizes = new ResizeObserver(measure);
		watch.observe(element, { subtree: true, childList: true, attributes: true, attributeFilter: ['width', 'height', 'style'] });
		sizes.observe(element);

		const connect = () => {
			const connection = new RFB(element, `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/v1/vms/${vm}/screen`);
			rfb = connection;
			connection.background = 'var(--screen-bg)';
			applyMode();
			connection.addEventListener('connect', () => {
				phase = 'connected';
				connection.focus();
				measure();
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
			watch.disconnect();
			sizes.disconnect();
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

	const size = (s: { width: number; height: number }) => `${s.width} × ${s.height}`;
</script>

<div class="screen" class:full={effective === 'full'} bind:this={target}></div>
{#if phase !== 'connected'}
	<p class="state" role="status">{t(phase === 'lost' ? 'vms.screenLost' : 'vms.screenConnecting')}</p>
{:else if guest.width}
	<span class="resolution mono" aria-live="polite">
		{#if effective === 'remote'}
			{t('vms.resolution.remote', { size: size(guest) })}
		{:else if effective === 'full'}
			{t('vms.resolution.full', { size: size(guest) })}
		{:else}
			{t('vms.resolution.scaled', { size: size(guest), shown: size(shown) })}
		{/if}
	</span>
{/if}

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
	.state {
		position: absolute;
		inset: 0;
		display: flex;
		align-items: center;
		justify-content: center;
		margin: 0;
		color: var(--screen-muted);
		font-size: 0.875rem;
		pointer-events: none;
	}
	/* Small and out of the way; it does not catch the mouse. */
	.resolution {
		position: absolute;
		top: 8px;
		right: 8px;
		padding: 2px 8px;
		background: var(--screen-chip);
		border-radius: var(--radius-ctl);
		color: var(--screen-muted);
		font-size: 0.6875rem;
		pointer-events: none;
	}
</style>
