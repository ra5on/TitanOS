import { afterEach } from 'vitest';
import { cleanup } from '@testing-library/svelte';
import { i18n, loadLocale } from '#lib/i18n/index.svelte.ts';

// Both languages, so that tests can switch between them at once.
await loadLocale('de');
await loadLocale('en');
i18n.ready = true;

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
