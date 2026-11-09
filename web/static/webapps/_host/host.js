// TitanOS web app host (PLAN 66, docs/WEBAPPS.md). Runs inside the app's own
// document, which is sandboxed (opaque origin: no cookies, no storage of
// TitanOS, no access to its API). It relays files between the app and the
// TitanOS window around it by postMessage; the window reads and writes the
// files with the rights of the account.
//
// bridge=host: the Craft host events on the canvas (`data-host="page"`):
//   app → host  <id>:ready, <id>:save {name, bytes, saveAs?, id?},
//               <id>:dirty {dirty}, <id>:failed {error}, <id>:handoff {name, bytes}
//   host → app  <id>:open {name, bytes}, <id>:saved {id}
// bridge=adapter: apps without the bridge. Open = a dropped file on the
// canvas; save = the download the app starts is caught and handed over.
(function () {
	'use strict';

	var script = document.currentScript;
	var query = new URLSearchParams(location.search);
	var data = (script && script.dataset) || {};
	var fromPath = /\/webapps\/([a-z0-9][a-z0-9-]*)\//.exec(location.pathname);
	var app = data.app || query.get('app') || (fromPath && fromPath[1]) || '';
	if (!/^[a-z0-9][a-z0-9-]{0,63}$/.test(app)) return;
	var bridge = (data.bridge || query.get('bridge')) === 'adapter' ? 'adapter' : 'host';
	var parent = window.parent;
	// Opened in a tab of its own: no TitanOS window to talk to.
	var framed = parent !== window;

	function post(message, transfer) {
		if (framed) parent.postMessage(message, '*', transfer || []);
	}

	// The app's own files come by requests without the cookies of the page
	// (its origin is opaque): a gate in front of the NAS that wants a cookie
	// (Cloudflare Access) turns them away, and the app never starts. Asked
	// once the same way the app asks; turned away, the window says why.
	if (framed && window.fetch) {
		fetch(location.href, { cache: 'no-store' })
			.then(function (r) {
				if (!r.ok) throw new Error(String(r.status));
			})
			.catch(function () {
				post({ type: 'webapp-failed', text: 'files_blocked' });
			});
	}

	// The element the app announced itself on (its canvas): opens go there.
	var announced = null;
	function canvas() {
		return (
			(announced && announced.isConnected ? announced : null) ||
			document.querySelector('canvas[data-host="page"]') ||
			document.getElementById(app + '_canvas') ||
			document.querySelector('canvas')
		);
	}

	/** An ArrayBuffer of exactly the bytes (a view's slice, not its whole buffer). */
	function buffer(bytes) {
		if (bytes instanceof ArrayBuffer) return bytes;
		if (ArrayBuffer.isView(bytes))
			return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
		return null;
	}

	/** Same rule as the Craft contract: last part of a path, no control characters. */
	function cleanName(raw, fallback) {
		var base = String(raw == null ? '' : raw)
			.split(/[\\/]/)
			.pop()
			// eslint-disable-next-line no-control-regex
			.replace(/[\u0000-\u001f\u007f]/g, '')
			.trim();
		if (!base || base === '.' || base === '..') return fallback;
		return Array.from(base).slice(0, 255).join('');
	}

	/** Outside a TitanOS window a file the app hands over is downloaded as usual. */
	function downloadHere(name, bytes) {
		var url = originalCreate(new Blob([bytes]));
		var a = document.createElement('a');
		a.href = url;
		a.download = name;
		originalClick.call(a);
		setTimeout(function () {
			URL.revokeObjectURL(url);
		}, 60000);
	}

	var originalCreate = URL.createObjectURL.bind(URL);
	var originalRevoke = URL.revokeObjectURL.bind(URL);
	var originalClick = HTMLAnchorElement.prototype.click;

	var ready = false;
	var waiting = [];
	var saveIds = [];

	function becomeReady() {
		if (ready) return;
		ready = true;
		post({ type: 'webapp-ready' });
		var queued = waiting;
		waiting = [];
		queued.forEach(open);
	}

	function open(message) {
		if (!ready) return void waiting.push(message);
		var target = canvas();
		if (!target) return void post({ type: 'webapp-failed', text: 'no canvas' });
		if (bridge === 'host') {
			target.dispatchEvent(
				new CustomEvent(app + ':open', { detail: { name: message.name, bytes: message.bytes } }),
			);
			return;
		}
		// Adapter: the file is dropped on the app, as if dragged from the desktop.
		var file = new File([message.bytes], message.name);
		var transfer = new DataTransfer();
		transfer.items.add(file);
		var box = target.getBoundingClientRect();
		var at = {
			bubbles: true,
			cancelable: true,
			composed: true,
			clientX: box.left + box.width / 2,
			clientY: box.top + box.height / 2,
			dataTransfer: transfer,
		};
		['dragenter', 'dragover', 'drop'].forEach(function (type) {
			target.dispatchEvent(new DragEvent(type, at));
		});
	}

	// Menu commands of the TitanOS window and the Ctrl key they stand for.
	var KEYS = {
		save: ['s', 'KeyS', 83],
		undo: ['z', 'KeyZ', 90],
		redo: ['y', 'KeyY', 89],
		find: ['f', 'KeyF', 70],
		'select-all': ['a', 'KeyA', 65],
	};

	// Messages of the TitanOS window only (the frame's parent).
	window.addEventListener('message', function (event) {
		if (!framed || event.source !== parent) return;
		var msg = event.data;
		if (!msg || typeof msg !== 'object' || typeof msg.type !== 'string') return;
		if (msg.type === 'webapp-open') {
			if (!(msg.bytes instanceof ArrayBuffer)) return;
			open({ name: cleanName(msg.name, 'document'), bytes: msg.bytes });
		} else if (msg.type === 'webapp-command' && msg.command === 'save-as' && bridge === 'host') {
			// TitanOS extension (proposed upstream as <id>:save-as): the Office key for
			// Save As; the app then posts <id>:save with saveAs true.
			var target = canvas();
			if (!target) return;
			['keydown', 'keyup'].forEach(function (type) {
				target.dispatchEvent(
					new KeyboardEvent(type, {
						key: 'F12',
						code: 'F12',
						keyCode: 123,
						bubbles: true,
						cancelable: true,
					}),
				);
			});
		} else if (msg.type === 'webapp-command' && bridge === 'host' && KEYS[msg.command]) {
			// PLAN 67-1: the menu bar of the window (Speichern, Rückgängig …) presses the app's own keys.
			var key = KEYS[msg.command];
			var into = canvas();
			if (!into) return;
			['keydown', 'keyup'].forEach(function (type) {
				into.dispatchEvent(
					new KeyboardEvent(type, {
						key: key[0],
						code: key[1],
						keyCode: key[2],
						ctrlKey: true,
						bubbles: true,
						cancelable: true,
					}),
				);
			});
		} else if (msg.type === 'webapp-saved') {
			var id = saveIds.shift();
			if (msg.ok === true && id != null && bridge === 'host') {
				var target = canvas();
				if (target) target.dispatchEvent(new CustomEvent(app + ':saved', { detail: { id: id } }));
			}
		}
	});

	// --- File pickers (PLAN 75-20) -------------------------------------------
	// The app's own "Open …" (and picture, font, sound pickers) uses an
	// <input type=file>. In a TitanOS window the window shows its own dialog
	// with the files of the NAS instead (and "Von diesem Gerät …" for a file
	// of this PC); the chosen files come back as webapp-picked and are put
	// into the input, as if picked in the browser's dialog.
	var picking = null;
	function pickFor(input) {
		if (picking && picking !== input) {
			picking.dispatchEvent(new Event('cancel', { bubbles: true }));
		}
		picking = input;
		post({ type: 'webapp-pick', accept: input.accept || '', multiple: input.multiple === true });
	}
	function isFileInput(el) {
		return el instanceof HTMLInputElement && el.type === 'file';
	}
	if (framed) {
		var originalInputClick = HTMLInputElement.prototype.click;
		HTMLInputElement.prototype.click = function () {
			if (isFileInput(this)) return void pickFor(this);
			return originalInputClick.call(this);
		};
		if (typeof HTMLInputElement.prototype.showPicker === 'function') {
			var originalShowPicker = HTMLInputElement.prototype.showPicker;
			HTMLInputElement.prototype.showPicker = function () {
				if (isFileInput(this)) return void pickFor(this);
				return originalShowPicker.call(this);
			};
		}
		// A file input the app shows itself (a visible button) is clicked by the user.
		document.addEventListener(
			'click',
			function (event) {
				var target = event.target;
				var input = isFileInput(target)
					? target
					: target instanceof HTMLLabelElement && isFileInput(target.control)
						? target.control
						: null;
				if (!input) return;
				event.preventDefault();
				pickFor(input);
			},
			true,
		);
		window.addEventListener('message', function (event) {
			if (event.source !== parent) return;
			var msg = event.data;
			if (!msg || msg.type !== 'webapp-picked' || !picking) return;
			var input = picking;
			picking = null;
			var list = Array.isArray(msg.files) ? msg.files : [];
			var files = [];
			list.forEach(function (f) {
				var bytes = f && buffer(f.bytes);
				if (bytes) files.push(new File([bytes], cleanName(f.name, 'document')));
			});
			if (!files.length) {
				input.dispatchEvent(new Event('cancel', { bubbles: true }));
				return;
			}
			var transfer = new DataTransfer();
			files.slice(0, input.multiple ? files.length : 1).forEach(function (file) {
				transfer.items.add(file);
			});
			input.files = transfer.files;
			input.dispatchEvent(new Event('input', { bubbles: true }));
			input.dispatchEvent(new Event('change', { bubbles: true }));
		});
	}

	if (bridge === 'host') {
		// The app's events are dispatched on its canvas without bubbling, and the
		// canvas does not exist yet when this runs: listen on the window in the
		// capture phase, which sees every event on its way to its target.
		var on = function (kind, handle) {
			window.addEventListener(
				app + ':' + kind,
				function (event) {
					handle((event && event.detail) || {}, event);
				},
				true,
			);
		};
		on('ready', function (_detail, event) {
			if (event && event.target instanceof Element) announced = event.target;
			becomeReady();
		});
		on('save', function (detail) {
			var bytes = buffer(detail.bytes);
			if (!bytes) return;
			var name = cleanName(detail.name, 'document');
			if (!framed) return void downloadHere(name, bytes);
			saveIds.push(typeof detail.id === 'string' ? detail.id : null);
			post({ type: 'webapp-save', name: name, saveAs: detail.saveAs === true, bytes: bytes }, [
				bytes,
			]);
		});
		on('dirty', function (detail) {
			post({ type: 'webapp-dirty', dirty: detail.dirty === true });
		});
		on('failed', function (detail) {
			post({ type: 'webapp-failed', text: String(detail.error == null ? '' : detail.error) });
		});
		// A file the app passes on to another app: the window offers to store it.
		on('handoff', function (detail) {
			var bytes = buffer(detail.bytes);
			if (!bytes) return;
			var name = cleanName(detail.name, 'document');
			if (!framed) return void downloadHere(name, bytes);
			post({ type: 'webapp-save', name: name, saveAs: true, bytes: bytes }, [bytes]);
		});
		return;
	}

	// --- Adapter ---------------------------------------------------------------
	if (!framed) return;

	// No file pickers of the browser: the app falls back to downloads, which we catch.
	['showSaveFilePicker', 'showOpenFilePicker', 'showDirectoryPicker'].forEach(function (name) {
		try {
			Object.defineProperty(window, name, { value: undefined, configurable: true });
		} catch (e) {
			// Read-only in this browser: the app may use it.
		}
	});

	// Blob addresses: remembered, so a download can be read after the app revoked it.
	var blobs = new Map();
	URL.createObjectURL = function (object) {
		var url = originalCreate(object);
		if (object instanceof Blob) blobs.set(url, object);
		return url;
	};
	URL.revokeObjectURL = function (url) {
		setTimeout(function () {
			blobs.delete(url);
			originalRevoke(url);
		}, 60000);
	};

	function caught(anchor) {
		if (!(anchor instanceof HTMLAnchorElement)) return false;
		var href = anchor.href || '';
		var isBlob = href.indexOf('blob:') === 0 || href.indexOf('data:') === 0;
		if (!isBlob && !anchor.hasAttribute('download')) return false;
		if (!isBlob) return false;
		var name = cleanName(anchor.getAttribute('download'), 'download');
		var known = blobs.get(href);
		var read = known
			? Promise.resolve(known)
			: fetch(href).then(function (r) {
					return r.blob();
				});
		read
			.then(function (blob) {
				return blob.arrayBuffer();
			})
			.then(function (bytes) {
				post({ type: 'webapp-save', name: name, saveAs: true, bytes: bytes }, [bytes]);
			})
			.catch(function (e) {
				post({ type: 'webapp-failed', text: String((e && e.message) || e) });
			});
		return true;
	}

	HTMLAnchorElement.prototype.click = function () {
		if (caught(this)) return;
		return originalClick.call(this);
	};
	var originalDispatch = EventTarget.prototype.dispatchEvent;
	EventTarget.prototype.dispatchEvent = function (event) {
		if (event && event.type === 'click' && this instanceof HTMLAnchorElement && caught(this))
			return false;
		return originalDispatch.call(this, event);
	};
	document.addEventListener(
		'click',
		function (event) {
			var a = event.target instanceof Element ? event.target.closest('a') : null;
			if (a && caught(a)) event.preventDefault();
		},
		true,
	);

	// Ready: the app took away its loading notice and sized its canvas to the
	// page (a canvas the app does not run yet keeps the browser's 300 × 150).
	var started = Date.now();
	var settled = 0;
	var timer = setInterval(function () {
		var loading =
			document.getElementById(app + '_loading') || document.querySelector('[id$="_loading"]');
		var shown = loading && loading.isConnected && getComputedStyle(loading).display !== 'none';
		var c = canvas();
		var sized =
			c &&
			c.clientWidth > 0 &&
			Math.abs(c.width - Math.round(c.clientWidth * (window.devicePixelRatio || 1))) <= 2;
		settled = sized && !shown ? settled + 1 : 0;
		// Two checks in a row (400 ms): the app's handlers are installed by then.
		if (settled >= 2 || Date.now() - started > 90000) {
			clearInterval(timer);
			becomeReady();
		}
	}, 200);
})();
