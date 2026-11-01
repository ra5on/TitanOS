<script lang="ts" module>
	// Basic or advanced view of the app form, remembered in this browser.
	const KEY = 'titanos.appform.advanced';
	export function storedAdvanced(): boolean {
		try {
			return localStorage.getItem(KEY) === '1';
		} catch {
			return false;
		}
	}
	function store(advanced: boolean) {
		try {
			localStorage.setItem(KEY, advanced ? '1' : '0');
		} catch {
			// Private windows may refuse; the view then resets on reload.
		}
	}
</script>

<script lang="ts">
	import { t } from '#lib/i18n/index.svelte.ts';
	let { advanced = $bindable(false) }: { advanced: boolean } = $props();
	function toggle() {
		advanced = !advanced;
		store(advanced);
	}
</script>

<button
	type="button"
	class="view-switch"
	role="switch"
	aria-checked={advanced}
	aria-label={t('appform.advanced')}
	onclick={toggle}
	><span class:on={!advanced}>{t('appform.basic')}</span><span class="track" aria-hidden="true"
		><span class="knob"></span></span
	><span class:on={advanced}>{t('appform.advanced')}</span></button
>

<style>
	.view-switch {
		display: inline-flex;
		align-items: center;
		gap: 10px;
		padding: 4px 2px;
		background: none;
		border: 0;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-md);
		cursor: pointer;
	}
	.on {
		color: var(--text);
		font-weight: 500;
	}
	.track {
		position: relative;
		width: 34px;
		height: 19px;
		border-radius: 10px;
		background: var(--track);
	}
	.knob {
		position: absolute;
		top: 2px;
		left: 2px;
		width: 15px;
		height: 15px;
		border-radius: 50%;
		background: var(--muted);
		transition: left 0.15s;
	}
	[aria-checked='true'] .track {
		background: var(--accent);
	}
	[aria-checked='true'] .knob {
		left: 17px;
		background: var(--on-accent);
	}
	.view-switch:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
		border-radius: var(--radius-small);
	}
</style>
