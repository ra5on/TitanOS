<script lang="ts">
	import { tick, untrack } from 'svelte';
	import Icon from '#lib/components/Icon.svelte';
	import AgentDots from './AgentDots.svelte';
	import type { Settings } from './api.ts';
	import {
		colorKeys,
		contrast,
		rgb as rgbChannels,
		defaultAppearance,
		type ColorKey,
	} from './appearance.ts';
	import { msg } from './labels.ts';
	let {
		settings,
		ondraft,
		onsave,
		pending,
	}: {
		settings: Settings;
		ondraft: (s: Partial<Settings> | null) => void;
		onsave: (s: Partial<Settings>) => Promise<void>;
		pending: boolean;
	} = $props();
	let theme = $state(untrack(() => settings.theme)),
		motion = $state(untrack(() => settings.motion)),
		dotSize = $state(untrack(() => settings.dotSize)),
		colors = $state<Record<string, string>>(untrack(() => ({ ...settings.colors })));
	const draft = $derived({ theme, motion, dotSize, colors: { ...colors } });
	let preview = $state<HTMLElement>(),
		ratio = $state<number>(0),
		measured = $state<Record<string, string>>({}),
		saved = $state(false);
	let dirty = $state(false);
	$effect(() => {
		if (dirty) return;
		const external = settings;
		untrack(() => {
			theme = external.theme;
			motion = external.motion;
			dotSize = external.dotSize;
			colors = { ...external.colors };
		});
	});
	function edit() {
		dirty = true;
		saved = false;
	}
	$effect(() => {
		const value = draft;
		if (dirty) ondraft(value);
		return () => ondraft(null);
	});
	$effect(() => {
		void draft;
		const el = preview;
		if (!el) return;
		let live = true;
		async function measure() {
			await tick();
			if (!live || !el) return;
			let minimum = Infinity;
			const result: Record<string, string> = {};
			for (const n of el.querySelectorAll<HTMLElement>('[data-color]')) {
				const key = n.dataset.color!,
					style = getComputedStyle(n),
					bg = style.backgroundColor;
				if (
					[
						'background',
						'navigation',
						'card',
						'userBubble',
						'agentBubble',
						'button',
						'accent',
					].includes(key)
				)
					minimum = Math.min(minimum, contrast(style.color, bg));
				const rgb = rgbChannels(bg);
				if (rgb?.length === 3)
					result[key] =
						'#' + rgb.map((c) => Math.round(Number(c)).toString(16).padStart(2, '0')).join('');
			}
			ratio = Number.isFinite(minimum) ? minimum : 0;
			measured = result;
		}
		void measure();
		const observer = new MutationObserver(() => void measure());
		observer.observe(document.documentElement, {
			attributes: true,
			attributeFilter: ['class', 'style', 'data-theme'],
		});
		return () => {
			live = false;
			observer.disconnect();
		};
	});
	async function save() {
		try {
			await onsave(draft);
		} catch {
			return;
		}
		saved = true;
		dirty = false;
	}
	function reset() {
		theme = defaultAppearance.theme;
		motion = defaultAppearance.motion;
		dotSize = defaultAppearance.dotSize;
		colors = {};
		edit();
	}
	function paint(key: ColorKey, value: string) {
		colors = { ...colors, [key]: value };
		edit();
	}
</script>

<h3>{msg('appearance')}</h3>
<div class="pa-formgrid">
	<label
		>{msg('theme')}<select bind:value={theme} onchange={edit}
			>{#each ['system', 'light', 'dark'] as v}<option value={v}>{msg(v)}</option>{/each}</select
		></label
	>
	<label
		>{msg('motion')}<select bind:value={motion} onchange={edit}
			>{#each ['full', 'subtle', 'off'] as v}<option value={v}>{msg(v)}</option>{/each}</select
		></label
	>
	<label
		>{msg('dotSize')} · {dotSize} px<input
			type="range"
			min="4"
			max="48"
			step="1"
			bind:value={dotSize}
			oninput={edit}
		/></label
	>
</div>
<div class="pa-sample" bind:this={preview} aria-label={msg('live')}>
	<div class="pa-title-row"><small>{msg('live')}</small><AgentDots calm={true} /></div>
	<span class="pa-bubble pa-user">{msg('user')}</span><span class="pa-bubble pa-agent">Agent</span>
	<div class="pa-swatches" aria-hidden="true">
		{#each colorKeys as k}<i
				data-color={k}
				style={'background-color:var(--pa-' + k + ');color:var(--pa-text)'}
			></i>{/each}
	</div>
</div>
<details>
	<summary>{msg('colors')}</summary>
	<div class="pa-colors">
		{#each colorKeys as key}
			<label
				><span>{msg('color.' + key)}</span><input
					type="color"
					value={colors[key] || measured[key] || '#7899a7'}
					oninput={(e) => paint(key, e.currentTarget.value)}
				/></label
			>
		{/each}
	</div>
</details>
{#if ratio}<p class:pa-warning={ratio < 4.5} class="pa-status">
		<Icon name={ratio < 4.5 ? 'info' : 'check'} />{msg(ratio < 4.5 ? 'lowContrast' : 'contrast')} · {ratio.toFixed(
			1,
		)} : 1
	</p>
	{#if ratio < 4.5}<small>{msg('contrastBody')}</small>{/if}{/if}
<div class="pa-actions">
	<button class="pa-button" onclick={reset}>{msg('reset')}</button><button
		class="pa-primary"
		disabled={!dirty || pending}
		onclick={save}>{msg('save')}</button
	>{#if saved}<small role="status">{msg('saved')}</small>{/if}
</div>
