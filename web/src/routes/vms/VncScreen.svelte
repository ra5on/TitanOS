<script lang="ts">
	import { apiSocket } from '#lib/cluster-node.ts';
	// The screen of a running VM: noVNC in the browser, a WebSocket to
	// titand, QEMU's VNC server behind it.
	import { untrack } from 'svelte';
	import RFB from '@novnc/novnc';
	import { api } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import {
		accelerate,
		diffInput,
		KEY_CODE,
		KEYSYM,
		keysymForChar,
		namedKeysym,
		SENTINEL,
	} from './vnc-input.ts';

	export type ScaleMode = 'scale' | 'remote' | 'full';
	export type ConnectionPhase = 'connecting' | 'connected' | 'lost' | 'disabled';
	/** PLAN 87: how a finger works on the screen – as a trackpad with a cursor, or directly where it touches. */
	export type PointerMode = 'trackpad' | 'direct';
	/** PLAN 87: modifier keys of the key bar, held for the next key. */
	export type Modifiers = { ctrl: boolean; alt: boolean };

	// resizable: the guest can take a resolution from the browser. That needs
	// the virtio graphics of the Linux template; the plain VGA of Windows and
	// other systems cannot, so their picture is scaled instead.
	type Props = {
		vm: string;
		mode: ScaleMode;
		resizable: boolean;
		onstatus?: (phase: ConnectionPhase) => void;
		/** PLAN 87: a touch screen (phone, tablet): the hidden text field and the finger as mouse. */
		touch?: boolean;
		pointer?: PointerMode;
		mods?: Modifiers;
		/** The phone's keyboard opened or closed. */
		onkeyboard?: (open: boolean) => void;
	};
	let {
		vm,
		mode,
		resizable,
		onstatus = () => {},
		touch = false,
		pointer = 'trackpad',
		mods = $bindable({ ctrl: false, alt: false }),
		onkeyboard = () => {},
	}: Props = $props();

	let target = $state<HTMLElement>();
	let root = $state<HTMLElement>();
	let field = $state<HTMLTextAreaElement>();

	let rfb: RFB | undefined;
	const effective = $derived<ScaleMode>(mode === 'remote' && !resizable ? 'scale' : mode);

	function applyMode() {
		if (!rfb) return;
		rfb.resizeSession = effective === 'remote';
		rfb.scaleViewport = effective !== 'full';
		rfb.clipViewport = false;
		// PLAN 87: a touch on the picture must not take the focus from the
		// hidden text field (the phone's keyboard would close).
		rfb.focusOnClick = !touch;
	}

	$effect(() => {
		if (!target) return;
		const element = target;
		let retry: ReturnType<typeof setTimeout> | undefined;
		let closed = false;

		/** Whether titand will open the screen now: consoles switched on. */
		const allowed = async (): Promise<'yes' | 'disabled' | 'unavailable'> => {
			try {
				const info = await api.get<{ enabled: boolean }>('/terminal');
				return info.enabled ? 'yes' : 'disabled';
			} catch {
				return 'unavailable';
			}
		};

		const connect = async () => {
			onstatus('connecting');
			const answer = await allowed();
			if (closed) return;
			if (answer !== 'yes') {
				// Switched off or not reachable: look again in a while.
				onstatus(answer === 'disabled' ? 'disabled' : 'lost');
				retry = setTimeout(connect, 5000);
				return;
			}
			const connection = new RFB(element, apiSocket(`/vms/${vm}/screen`));
			rfb = connection;
			connection.background = 'var(--screen-bg)';
			applyMode();
			connection.addEventListener('connect', () => {
				onstatus('connected');
				if (!touch || document.activeElement !== field) connection.focus({ preventScroll: true });
				centerCursor();
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
		touch;
		applyMode();
	});

	/** Hands the keyboard back to the picture, unless the phone's keyboard is in use. */
	function refocus() {
		if (touch && document.activeElement === field) return;
		rfb?.focus({ preventScroll: true });
	}

	export function ctrlAltDel() {
		rfb?.sendCtrlAltDel();
		refocus();
	}

	/** Presses keys together, as X keysyms with their key codes, and lets go in reverse. */
	export function sendKeys(keys: [number, string][]) {
		if (!rfb) return;
		for (const [keysym, code] of keys) rfb.sendKey(keysym, code, true);
		for (const [keysym, code] of [...keys].reverse()) rfb.sendKey(keysym, code, false);
		refocus();
	}

	/** PLAN 87: one key, with Ctrl/Alt of the key bar if they are held; they are let go afterwards. */
	export function press(keysym: number, extra: { ctrl?: boolean; alt?: boolean } = {}) {
		if (!rfb) return;
		const held: number[] = [];
		if (mods.ctrl || extra.ctrl) held.push(KEYSYM.Control);
		if (mods.alt || extra.alt) held.push(KEYSYM.Alt);
		const keys = [...held, keysym].map((k) => [k, KEY_CODE[k] ?? null] as const);
		for (const [k, code] of keys) rfb.sendKey(k, code, true);
		for (const [k, code] of [...keys].reverse()) rfb.sendKey(k, code, false);
		if (mods.ctrl || mods.alt) mods = { ctrl: false, alt: false };
	}

	/** Types text into the VM, as the clipboard of the guest. */
	export function paste(text: string) {
		rfb?.clipboardPasteFrom(text);
		refocus();
	}

	/** The picture as it is right now, as a PNG. */
	export function screenshot(): string {
		return rfb?.toDataURL('image/png') ?? '';
	}

	// --- PLAN 87: the phone's keyboard -----------------------------------

	let keyboardOpen = $state(false);
	let composing = false;
	let before = SENTINEL;

	function resetField() {
		if (!field) return;
		field.value = SENTINEL;
		before = SENTINEL;
		field.setSelectionRange(1, 1);
	}

	/**
	 * Opens the phone's keyboard: the hidden text field takes the focus. Must
	 * run inside the tap itself (iOS opens the keyboard only then).
	 */
	export function openKeyboard() {
		if (!field) return;
		resetField();
		field.focus({ preventScroll: true });
	}
	export function closeKeyboard() {
		field?.blur();
	}

	/** Characters from the field: each one a key press, with the held modifiers on the first. */
	function typeText(text: string) {
		for (const ch of text) press(keysymForChar(ch));
	}

	function onFieldInput() {
		if (!field) return;
		const now = field.value;
		const { back, text } = diffInput(before, now);
		for (let i = 0; i < back; i++) press(KEYSYM.BackSpace);
		typeText(text);
		before = now;
		// The field is kept short and never empty, except while a word is composed.
		if (!composing && (now.length === 0 || now.length > 40 || !now.startsWith(SENTINEL)))
			resetField();
	}

	function onFieldKey(event: KeyboardEvent) {
		// Phone keyboards send "Unidentified" (229) while composing: the text follows as input.
		if (event.isComposing || event.keyCode === 229) return;
		const named = namedKeysym(event.key);
		const shortcut =
			(event.ctrlKey || event.altKey || event.metaKey) && event.key.length === 1 && !named;
		if (named === null && !shortcut) return;
		event.preventDefault();
		if (named !== null) press(named, { ctrl: event.ctrlKey, alt: event.altKey });
		else press(keysymForChar(event.key.toLowerCase()), { ctrl: event.ctrlKey, alt: event.altKey });
	}

	// --- PLAN 87: the finger as mouse (trackpad) ---------------------------

	// The cursor in pixels of the picture's box on the page (canvas), kept in it.
	let cursor = $state({ x: 0, y: 0 });
	let cursorShown = $state(false);
	const trackpad = $derived(touch && pointer === 'trackpad');

	function canvasBox(): DOMRect | null {
		return target?.querySelector('canvas')?.getBoundingClientRect() ?? null;
	}
	function centerCursor() {
		const box = canvasBox();
		if (!box) return;
		cursor = { x: box.width / 2, y: box.height / 2 };
	}
	/** Where the cursor is drawn inside this element. */
	const cursorAt = $derived.by(() => {
		void cursor;
		const box = canvasBox();
		const own = root?.getBoundingClientRect();
		if (!box || !own) return { left: 0, top: 0 };
		return { left: box.left - own.left + cursor.x, top: box.top - own.top + cursor.y };
	});

	let buttonsHeld = 0;
	/**
	 * A mouse event on noVNC's canvas, at the cursor. While a button is held
	 * noVNC catches the mouse with a cover of its own, which passes events it
	 * gets on the window to the canvas and lets go at the button's release.
	 */
	function mouse(type: 'mousemove' | 'mousedown' | 'mouseup', button = 0, buttons = buttonsHeld) {
		const canvas = target?.querySelector('canvas');
		const box = canvas?.getBoundingClientRect();
		if (!canvas || !box) return;
		const init: MouseEventInit = {
			clientX: box.left + cursor.x,
			clientY: box.top + cursor.y,
			button,
			buttons,
			bubbles: true,
			cancelable: true,
		};
		const cover = document.getElementById('noVNC_mouse_capture_elem');
		const caught = !!cover && cover.style.display !== 'none' && type !== 'mousedown';
		(caught ? window : canvas).dispatchEvent(new MouseEvent(type, init));
	}
	function click(button: 0 | 2) {
		const bit = button === 0 ? 1 : 2;
		buttonsHeld = bit;
		mouse('mousedown', button, bit);
		buttonsHeld = 0;
		mouse('mouseup', button, 0);
	}
	function wheel(dx: number, dy: number) {
		const canvas = target?.querySelector('canvas');
		const box = canvas?.getBoundingClientRect();
		if (!canvas || !box) return;
		canvas.dispatchEvent(
			new WheelEvent('wheel', {
				clientX: box.left + cursor.x,
				clientY: box.top + cursor.y,
				deltaX: dx,
				deltaY: dy,
				deltaMode: 0,
				bubbles: true,
				cancelable: true,
			}),
		);
	}

	type Finger = { x: number; y: number; sx: number; sy: number; t: number };
	const fingers = new Map<number, Finger>();
	let gesture: 'none' | 'one' | 'drag' | 'two' | 'scroll' = 'none';
	let started = 0;
	let moved = 0;
	let lastTap = { t: -1e9, x: 0, y: 0 };
	let keepKeyboard = false;

	function centroid() {
		let x = 0,
			y = 0;
		for (const f of fingers.values()) {
			x += f.x;
			y += f.y;
		}
		return { x: x / fingers.size, y: y / fingers.size };
	}

	function down(event: PointerEvent) {
		if (event.pointerType === 'mouse') return;
		event.preventDefault();
		(event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId);
		keepKeyboard = document.activeElement === field;
		fingers.set(event.pointerId, {
			x: event.clientX,
			y: event.clientY,
			sx: event.clientX,
			sy: event.clientY,
			t: event.timeStamp,
		});
		cursorShown = true;
		if (fingers.size === 1) {
			gesture = 'one';
			started = event.timeStamp;
			moved = 0;
			// A touch right after a tap that then moves holds the left button (drag).
			if (
				event.timeStamp - lastTap.t < 320 &&
				Math.hypot(event.clientX - lastTap.x, event.clientY - lastTap.y) < 40
			)
				gesture = 'drag';
		} else if (fingers.size === 2) {
			if (gesture === 'drag' && buttonsHeld) {
				buttonsHeld = 0;
				mouse('mouseup', 0, 0);
			}
			gesture = 'two';
			started = event.timeStamp;
			moved = 0;
		}
	}

	let dragging = false;
	function move(event: PointerEvent) {
		const f = fingers.get(event.pointerId);
		if (!f) return;
		event.preventDefault();
		if (gesture === 'two' || gesture === 'scroll') {
			const c0 = centroid();
			f.x = event.clientX;
			f.y = event.clientY;
			const c1 = centroid();
			moved += Math.hypot(c1.x - c0.x, c1.y - c0.y);
			if (moved > 10) gesture = 'scroll';
			// Natural scrolling: the content follows the fingers.
			if (gesture === 'scroll') wheel(-(c1.x - c0.x) * 2, -(c1.y - c0.y) * 2);
			return;
		}
		const dt = event.timeStamp - f.t;
		const dx = event.clientX - f.x;
		const dy = event.clientY - f.y;
		f.x = event.clientX;
		f.y = event.clientY;
		f.t = event.timeStamp;
		moved += Math.hypot(dx, dy);
		if (moved < 4) return;
		const box = canvasBox();
		if (!box) return;
		cursor = {
			x: Math.max(0, Math.min(box.width - 1, cursor.x + accelerate(dx, dt))),
			y: Math.max(0, Math.min(box.height - 1, cursor.y + accelerate(dy, dt))),
		};
		if (gesture === 'drag' && !dragging) {
			dragging = true;
			buttonsHeld = 1;
			mouse('mousedown', 0, 1);
		}
		mouse('mousemove');
	}

	function up(event: PointerEvent) {
		if (!fingers.has(event.pointerId)) return;
		event.preventDefault();
		const quick = event.timeStamp - started < 300;
		if (gesture === 'two' && quick && moved < 10 && fingers.size === 2) {
			click(2);
			gesture = 'none';
		}
		fingers.delete(event.pointerId);
		if (fingers.size > 0) return;
		if (dragging) {
			dragging = false;
			buttonsHeld = 0;
			mouse('mouseup', 0, 0);
		} else if ((gesture === 'one' || gesture === 'drag') && quick && moved < 8) {
			click(0);
			lastTap = { t: event.timeStamp, x: event.clientX, y: event.clientY };
		}
		gesture = 'none';
		// The phone's keyboard stays open while the picture is touched.
		if (keepKeyboard && document.activeElement !== field) field?.focus({ preventScroll: true });
	}

	$effect(() => {
		if (trackpad && rfb) centerCursor();
	});
</script>

<div class="vnc" bind:this={root}>
	<div class="screen" class:full={effective === 'full'} class:touch bind:this={target}></div>
	{#if trackpad}
		<!-- A touch here moves the cursor; tap = click, two fingers = right click or scroll. -->
		<div
			class="touch-layer"
			data-touch-layer
			role="presentation"
			onpointerdown={down}
			onpointermove={move}
			onpointerup={up}
			onpointercancel={up}
			ontouchstart={(event) => event.preventDefault()}
		></div>
		{#if cursorShown}
			<svg
				class="cursor"
				data-trackpad-cursor
				style:left="{cursorAt.left}px"
				style:top="{cursorAt.top}px"
				width="18"
				height="26"
				viewBox="0 0 18 26"
				aria-hidden="true"
				><path
					d="M1 1 L1 20 L6 15.5 L9.5 24 L13 22.5 L9.5 14.5 L16 14.5 Z"
					fill="#fff"
					stroke="#000"
					stroke-width="1.5"
					stroke-linejoin="round"
				/></svg
			>
		{/if}
	{/if}
	{#if touch}
		<!-- PLAN 87: the phone's keyboard types into this field; every change goes to the VM. -->
		<textarea
			class="keyboard-field"
			bind:this={field}
			data-vnc-keyboard
			rows="1"
			autocomplete="off"
			autocapitalize="off"
			spellcheck="false"
			aria-label={t('vms.touch.keyboardField')}
			oninput={onFieldInput}
			onkeydown={onFieldKey}
			oncompositionstart={() => (composing = true)}
			oncompositionend={() => {
				composing = false;
				onFieldInput();
			}}
			onfocus={() => {
				keyboardOpen = true;
				onkeyboard(true);
			}}
			onblur={() => {
				keyboardOpen = false;
				onkeyboard(false);
			}}
			{...{ autocorrect: 'off' }}></textarea>
	{/if}
</div>

<style>
	.vnc {
		position: absolute;
		inset: 0;
		overflow: hidden;
	}
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
	/* PLAN 87: a touch on the picture belongs to the VM, never to the page's
	   scrolling (else the browser may refuse noVNC's preventDefault). */
	.screen.touch :global(canvas) {
		touch-action: none;
	}
	.touch-layer {
		position: absolute;
		inset: 0;
		z-index: 1;
		touch-action: none;
		-webkit-user-select: none;
		user-select: none;
		-webkit-touch-callout: none;
	}
	.cursor {
		position: absolute;
		z-index: 2;
		pointer-events: none;
		filter: drop-shadow(0 1px 2px rgb(0 0 0 / 0.5));
	}
	/* In sight for the phone (else iOS will not open the keyboard), but not to be seen. */
	.keyboard-field {
		position: absolute;
		left: 0;
		bottom: 0;
		width: 1px;
		height: 1px;
		padding: 0;
		border: 0;
		opacity: 0;
		resize: none;
		font-size: 16px;
		color: transparent;
		background: transparent;
		caret-color: transparent;
		pointer-events: none;
	}
</style>
