<script lang="ts">
	// PLAN 87: the key bar under the VM screen on a touch screen – the phone's
	// keyboard, keys a phone keyboard lacks (Esc, Tab, Ctrl, Alt, arrows,
	// F1–F12, Ctrl+Alt+Del) and how a finger works on the picture.
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type VncScreen from './VncScreen.svelte';
	import type { Modifiers, PointerMode } from './VncScreen.svelte';
	import { fKey, KEYSYM } from './vnc-input.ts';

	type Props = {
		screen: VncScreen | undefined;
		mods: Modifiers;
		pointer: PointerMode;
		keyboard: boolean;
		/** The screen fills the phone (with this bar below it). */
		full?: boolean;
		onfull?: () => void;
	};
	let {
		screen,
		mods = $bindable(),
		pointer = $bindable(),
		keyboard,
		full = false,
		onfull,
	}: Props = $props();
	let fkeys = $state(false);

	// A button of the bar must not take the focus from the hidden text field
	// (the phone's keyboard would close on every key). Where a browser moves
	// it anyway, the tap gives it back (inside the tap, so iOS keeps the keyboard).
	let wasOpen = false;
	const keep = (event: Event) => {
		wasOpen = keyboard;
		event.preventDefault();
	};
	const act = (run: () => void) => () => {
		run();
		if (wasOpen && !document.activeElement?.matches('[data-vnc-keyboard]')) screen?.openKeyboard();
		wasOpen = false;
	};
</script>

<div class="keybar" role="toolbar" aria-label={t('vms.touch.bar')} data-vnc-keybar>
	<div class="row">
		<button
			type="button"
			class="key wide"
			aria-pressed={keyboard}
			title={t('vms.touch.keyboardHint')}
			data-key="keyboard"
			onpointerdown={keep}
			onmousedown={keep}
			onclick={() => (keyboard ? screen?.closeKeyboard() : screen?.openKeyboard())}
			><Icon name="keyboard" size={18} /><span>{t('vms.touch.keyboard')}</span></button
		>
		<button
			type="button"
			class="key"
			data-key="esc"
			onpointerdown={keep}
			onmousedown={keep}
			onclick={act(() => screen?.press(KEYSYM.Escape))}>{t('vms.touch.esc')}</button
		>
		<button
			type="button"
			class="key"
			data-key="tab"
			onpointerdown={keep}
			onmousedown={keep}
			onclick={act(() => screen?.press(KEYSYM.Tab))}>{t('vms.touch.tab')}</button
		>
		<button
			type="button"
			class="key mod"
			aria-pressed={mods.ctrl}
			data-key="ctrl"
			onpointerdown={keep}
			onmousedown={keep}
			onclick={act(() => (mods = { ...mods, ctrl: !mods.ctrl }))}
			>{t('shortcuts.ctrl')}{#if mods.ctrl}<small>{t('vms.touch.held')}</small>{/if}</button
		>
		<button
			type="button"
			class="key mod"
			aria-pressed={mods.alt}
			data-key="alt"
			onpointerdown={keep}
			onmousedown={keep}
			onclick={act(() => (mods = { ...mods, alt: !mods.alt }))}
			>{t('shortcuts.alt')}{#if mods.alt}<small>{t('vms.touch.held')}</small>{/if}</button
		>
		<button
			type="button"
			class="key"
			aria-pressed={fkeys}
			title={t('vms.touch.fkeysHint')}
			data-key="fkeys"
			onpointerdown={keep}
			onmousedown={keep}
			onclick={act(() => (fkeys = !fkeys))}>{t('vms.touch.fkeys')}</button
		>
	</div>
	{#if fkeys}
		<div class="row fkeys">
			{#each Array.from({ length: 12 }, (_, i) => i + 1) as n (n)}
				<button
					type="button"
					class="key"
					data-key="f{n}"
					onpointerdown={keep}
					onmousedown={keep}
					onclick={act(() => screen?.press(fKey(n)))}>F{n}</button
				>
			{/each}
		</div>
	{/if}
	<div class="row">
		{#each [['left', KEYSYM.Left, 'arrowLeft'], ['up', KEYSYM.Up, 'arrowUp'], ['down', KEYSYM.Down, 'arrowDown'], ['right', KEYSYM.Right, 'arrowRight']] as const as [name, keysym, icon] (name)}
			<button
				type="button"
				class="key"
				aria-label={t(`vms.touch.${name}`)}
				title={t(`vms.touch.${name}`)}
				data-key={name}
				onpointerdown={keep}
				onmousedown={keep}
				onclick={act(() => screen?.press(keysym))}><Icon name={icon} size={18} /></button
			>
		{/each}
		<button
			type="button"
			class="key wide"
			data-key="cad"
			onpointerdown={keep}
			onmousedown={keep}
			onclick={act(() => screen?.ctrlAltDel())}>{t('vms.ctrlAltDel')}</button
		>
		{#if onfull}
			<button
				type="button"
				class="key"
				aria-pressed={full}
				aria-label={t(full ? 'vms.touch.fullClose' : 'vms.touch.full')}
				title={t(full ? 'vms.touch.fullClose' : 'vms.touch.full')}
				data-key="full"
				onpointerdown={keep}
				onmousedown={keep}
				onclick={onfull}><Icon name="maximize" size={18} /></button
			>
		{/if}
	</div>
	<div class="row mode" role="radiogroup" aria-label={t('vms.touch.mode')}>
		<span class="mode-label">{t('vms.touch.mode')}</span>
		{#each ['trackpad', 'direct'] as const as entry (entry)}
			<button
				type="button"
				class="key seg"
				role="radio"
				aria-checked={pointer === entry}
				data-pointer={entry}
				onpointerdown={keep}
				onmousedown={keep}
				onclick={act(() => (pointer = entry))}>{t(`vms.touch.${entry}`)}</button
			>
		{/each}
	</div>
	<p class="hint muted">
		{t(pointer === 'trackpad' ? 'vms.touch.trackpadHint' : 'vms.touch.directHint')}
	</p>
</div>

<style>
	.keybar {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		padding: var(--space-2) var(--space-2) calc(var(--space-2) + env(safe-area-inset-bottom));
		background: color-mix(in srgb, var(--surface) 94%, var(--bg));
		border-top: 1px solid var(--border);
		-webkit-user-select: none;
		user-select: none;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
	}
	/* Labels stay on one line; a key too wide for the row moves to the next. */
	.key {
		flex: 1 0 44px;
		min-height: 44px;
		white-space: nowrap;
		display: inline-flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 2px;
		padding: 0 var(--space-2);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--chip);
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		touch-action: manipulation;
		cursor: pointer;
	}
	.key.wide {
		flex: 2 0 88px;
		flex-direction: row;
		gap: var(--space-1-5);
	}
	.key small {
		font-size: var(--text-xs);
		line-height: 1;
	}
	.key[aria-pressed='true'],
	.key[aria-checked='true'] {
		background: color-mix(in srgb, var(--accent) 26%, var(--surface));
		border-color: var(--accent);
		font-weight: 600;
	}
	.key:active {
		background: color-mix(in srgb, var(--accent) 18%, var(--surface));
	}
	.fkeys .key {
		flex: 1 0 calc(16.6% - 8px);
	}
	.mode {
		align-items: center;
	}
	.mode-label {
		flex: 1 1 100%;
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.seg {
		flex: 1 0 120px;
	}
	.hint {
		margin: 0;
		font-size: var(--text-sm);
		overflow-wrap: break-word;
	}
</style>
