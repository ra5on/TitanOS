import { test } from 'node:test';
import assert from 'node:assert/strict';
import { horizontalNav } from '../src/lib/horizontal-nav.ts';

globalThis.window = undefined;
globalThis.ResizeObserver = undefined;

function fixture(t) {
	const window = new EventTarget();
	let resize;
	let disconnected = false;
	t.mock.property(globalThis, 'window', window);
	t.mock.property(
		globalThis,
		'ResizeObserver',
		class {
			constructor(callback) {
				resize = callback;
			}
			observe() {}
			disconnect() {
				disconnected = true;
			}
		},
	);
	const classes = new Set();
	const nav = Object.assign(new EventTarget(), {
		scrollLeft: 0,
		clientWidth: 300,
		scrollWidth: 900,
		clientHeight: 40,
		classList: { add: (c) => classes.add(c), remove: (c) => classes.delete(c) },
		getBoundingClientRect: () => ({ left: 100, right: 400, top: 0, width: 300 }),
		setPointerCapture: (id) => {
			nav.captured = id;
		},
		hasPointerCapture: (id) => nav.captured === id,
		releasePointerCapture: () => {
			nav.captured = undefined;
		},
	});
	const items = { scrollWidth: 900, querySelector: () => null };
	nav.firstElementChild = items;
	const shell = { clientWidth: 360, querySelector: () => nav };
	let state;
	const action = horizontalNav(shell, (s) => {
		state = s;
	});
	t.after(() => action.destroy());
	const fire = (target, type, props = {}) => {
		const event = Object.assign(new Event(type, { cancelable: true }), {
			pointerType: 'mouse',
			pointerId: 1,
			button: 0,
			buttons: 1,
			clientX: 300,
			clientY: 20,
			detail: 1,
			...props,
		});
		target.dispatchEvent(event);
		return event;
	};
	return {
		window,
		nav,
		items,
		shell,
		action,
		fire,
		resize: () => resize(),
		state: () => state,
		classes,
		disconnected: () => disconnected,
	};
}

test('ordinary clicks, modified clicks, touch and native scrollbar keep their native behavior', (t) => {
	const f = fixture(t);
	for (const props of [{}, { ctrlKey: true }, { pointerType: 'touch' }, { clientY: 45 }]) {
		f.fire(f.nav, 'pointerdown', props);
		f.fire(f.window, 'pointermove', { ...props, clientX: Object.keys(props).length ? 200 : 298 });
		f.fire(f.window, 'pointerup', props);
		assert.equal(f.fire(f.nav, 'click').defaultPrevented, false);
		assert.equal(f.nav.scrollLeft, 0);
	}
});

test('mouse drag scrolls and suppresses navigation only for the resulting click', (t) => {
	const f = fixture(t);
	f.fire(f.nav, 'pointerdown');
	f.fire(f.window, 'pointermove', { clientX: 200 });
	assert.equal(f.nav.scrollLeft, 100);
	assert.equal(f.nav.captured, 1);
	assert.ok(f.classes.has('dragging'));
	f.fire(f.window, 'pointerup');
	assert.equal(f.nav.captured, undefined);
	assert.equal(f.classes.has('dragging'), false);
	assert.equal(f.fire(f.nav, 'click').defaultPrevented, true);
	f.fire(f.nav, 'pointerdown');
	f.fire(f.window, 'pointerup');
	assert.equal(f.fire(f.nav, 'click').defaultPrevented, false);
});

test('cancelled drag does not block keyboard navigation or leave listeners behind', (t) => {
	const f = fixture(t);
	f.fire(f.nav, 'pointerdown');
	f.fire(f.window, 'pointermove', { clientX: 200 });
	f.fire(f.window, 'blur');
	assert.equal(f.nav.captured, undefined);
	assert.equal(f.fire(f.nav, 'click', { detail: 0 }).defaultPrevented, false);
	f.action.destroy();
	assert.ok(f.disconnected());
	f.fire(f.nav, 'pointerdown');
	f.fire(f.window, 'pointermove', { clientX: 50 });
	assert.equal(f.nav.scrollLeft, 100);
});

test('resizing keeps active tab visible and reports both scroll boundaries', (t) => {
	const f = fixture(t);
	assert.deepEqual(f.state(), { overflow: true, left: false, right: true });
	f.items.querySelector = () => ({
		getBoundingClientRect: () => ({ left: 420, right: 500, width: 80 }),
	});
	f.resize();
	assert.equal(f.nav.scrollLeft, 100);
	f.nav.scrollLeft = 600;
	f.fire(f.nav, 'scroll');
	assert.deepEqual(f.state(), { overflow: true, left: true, right: false });
	f.shell.clientWidth = 1000;
	f.items.querySelector = () => null;
	f.resize();
	assert.equal(f.state().overflow, false);
});
