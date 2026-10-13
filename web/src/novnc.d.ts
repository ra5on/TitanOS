// noVNC ships without types; this is the part of it the VM screen uses.
declare module '@novnc/novnc' {
	export default class RFB extends EventTarget {
		constructor(target: HTMLElement, url: string, options?: { wsProtocols?: string[]; shared?: boolean });
		scaleViewport: boolean;
		resizeSession: boolean;
		clipViewport: boolean;
		focusOnClick: boolean;
		background: string;
		disconnect(): void;
		focus(): void;
		sendCtrlAltDel(): void;
		clipboardPasteFrom(text: string): void;
		toDataURL(type?: string): string;
	}
}
