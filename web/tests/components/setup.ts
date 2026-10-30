import { afterEach } from 'vitest';
import { cleanup } from '@testing-library/svelte';

// jsdom has <dialog> but not its modal methods.
const proto = HTMLDialogElement.prototype;
if (!proto.showModal) {
	proto.showModal = function (this: HTMLDialogElement) {
		this.setAttribute('open', '');
	};
}
if (!proto.close) {
	proto.close = function (this: HTMLDialogElement) {
		this.removeAttribute('open');
	};
}

afterEach(() => cleanup());
