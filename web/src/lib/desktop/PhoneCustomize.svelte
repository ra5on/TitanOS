<script lang="ts">
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import PhoneDockSize from './PhoneDockSize.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import { goto } from '$app/navigation';
	import {
		PHONE_BOTTOMS,
		PHONE_DOCK_DEFAULT,
		PHONE_DOCK_MAX,
		PHONE_FAVORITES_DEFAULT,
		phoneBottom,
		phoneDockSize,
		startSections,
		type StartSection,
	} from './phone-home.ts';
	import { programs } from './programs.svelte.ts';
	import { desktop, savePrefs } from './wm.svelte.ts';
	import { phoneChrome } from './immersive-phone.svelte.ts';
	// §121: the start page's sections, its apps (favorites) and what stands at the bottom.
	let section = $state<'sections' | 'favorites' | 'dock'>(
		phoneChrome.customizeDock ? 'dock' : 'sections',
	);
	const ordered = $derived(startSections(desktop.prefs.phone));
	function saveSections(next: { id: StartSection; shown: boolean }[]) {
		savePrefs((p) => {
			p.phone = {
				...p.phone,
				start: {
					...p.phone.start,
					order: next.map((s) => s.id),
					hidden: next.filter((s) => !s.shown).map((s) => s.id),
				},
			};
		});
	}
	function moveSection(i: number, direction: number) {
		const next = [...ordered];
		const j = i + direction;
		if (j < 0 || j >= next.length) return;
		[next[i], next[j]] = [next[j], next[i]];
		saveSections(next);
	}
	let choice = $state('');
	const all = $derived(programs());
	const byKey = $derived(new Map(all.map((app) => [app.key, app])));
	const phone = $derived(desktop.prefs.phone);
	const list = $derived(
		section === 'favorites'
			? (phone.favorites ?? PHONE_FAVORITES_DEFAULT)
			: phone.dock.custom
				? phone.dock.apps
				: PHONE_DOCK_DEFAULT,
	);
	const available = $derived(all.filter((app) => !list.includes(app.key)));
	const pinned = $derived(list.filter((k) => byKey.has(k)));
	function update(keys: string[]) {
		savePrefs((p) => {
			p.phone =
				section === 'favorites'
					? { ...p.phone, favorites: keys }
					: { ...p.phone, dock: { ...p.phone.dock, apps: keys, custom: true } };
		});
	}
	function move(key: string, direction: number) {
		const other = pinned[pinned.indexOf(key) + direction];
		const next = [...list];
		const a = next.indexOf(key),
			b = next.indexOf(other);
		if (a < 0 || b < 0) return;
		[next[a], next[b]] = [next[b], next[a]];
		update(next);
	}
</script>

<Dialog title={t('phone.nord.customize')} onclose={() => (phoneChrome.customize = false)} dismiss>
	<div class="switches">
		{#each ['sections', 'favorites', 'dock'] as const as key (key)}
			<button
				type="button"
				aria-pressed={section === key}
				onclick={() => {
					section = key;
					choice = '';
				}}
				>{t(
					key === 'sections'
						? 'start.sections'
						: key === 'favorites'
							? 'phone.tabs.apps'
							: 'phone.settings.bottom',
				)}</button
			>
		{/each}
	</div>
	{#if section === 'sections'}
		<ul>
			{#each ordered as item, i (item.id)}
				{@const name = t(`start.section.${item.id}`)}
				<li>
					<input
						type="checkbox"
						role="switch"
						class="switch"
						aria-label={t('start.section.show', { name })}
						checked={item.shown}
						onchange={(e) =>
							saveSections(
								ordered.map((s) =>
									s.id === item.id ? { ...s, shown: e.currentTarget.checked } : s,
								),
							)}
					/><span>{name}</span>
					<button
						type="button"
						aria-label={t('phone.nord.earlier', { name })}
						disabled={i === 0}
						onclick={() => moveSection(i, -1)}><Icon name="arrowUp" size={17} /></button
					>
					<button
						type="button"
						aria-label={t('phone.nord.later', { name })}
						disabled={i === ordered.length - 1}
						onclick={() => moveSection(i, 1)}><Icon name="arrowDown" size={17} /></button
					>
				</li>
			{/each}
		</ul>
		<button
			type="button"
			class="btn pages"
			onclick={() => {
				phoneChrome.customize = false;
				void goto('/?phone=pages');
			}}><Icon name="grid" size={16} />{t('start.pages')}</button
		>
	{/if}
	{#if section === 'dock'}
		<div class="size">
			<Segments
				compact
				label={t('phone.settings.bottom')}
				value={phoneBottom(phone)}
				options={PHONE_BOTTOMS.map((value) => ({
					value,
					label: t(`phone.settings.bottom.${value}`),
				}))}
				onchange={(bottom) => savePrefs((p) => (p.phone = { ...p.phone, bottom }))}
			/>
		</div>
		{#if phoneBottom(phone) === 'dock'}<div class="size">
				<b>{t('phone.nord.size')}</b><PhoneDockSize
					size={phoneDockSize(phone.dock.size)}
					onchange={(s) => savePrefs((p) => (p.phone.dock.size = s))}
				/>
			</div>{/if}
	{/if}
	{#if section !== 'sections' && (section === 'favorites' || phoneBottom(phone) === 'dock')}
		<ul>
			{#each pinned as key, i (key)}
				{@const app = byKey.get(key)!}
				<li>
					<DesktopIcon
						glyph={app.glyph}
						gradient={app.gradient}
						image={app.image}
						size={29}
						name=""
					/><span>{app.label}</span>
					<button
						type="button"
						aria-label={t('phone.nord.earlier', { name: app.label })}
						disabled={i === 0}
						onclick={() => move(key, -1)}><Icon name="arrowUp" size={17} /></button
					>
					<button
						type="button"
						aria-label={t('phone.nord.later', { name: app.label })}
						disabled={i === pinned.length - 1}
						onclick={() => move(key, 1)}><Icon name="arrowDown" size={17} /></button
					>
					<button
						type="button"
						aria-label={t('phone.nord.remove', { name: app.label })}
						onclick={() => update(list.filter((k) => k !== key))}
						><Icon name="close" size={18} /></button
					>
				</li>
			{/each}
		</ul>
		{#if available.length && list.length < PHONE_DOCK_MAX}
			<div class="add">
				<select class="field" aria-label={t('phone.nord.chooseApp')} bind:value={choice}
					><option value="">{t('phone.nord.chooseApp')}</option>
					{#each available as app (app.key)}<option value={app.key}>{app.label}</option>{/each}
				</select><button
					type="button"
					class="btn"
					disabled={!choice}
					aria-label={t('phone.nord.add')}
					onclick={() => {
						if (choice && !list.includes(choice)) update([...list, choice]);
						choice = '';
					}}><Icon name="plus" size={18} /></button
				>
			</div>
		{/if}
	{/if}
	<div class="dialog-actions">
		<button type="button" class="btn btn-primary" onclick={() => (phoneChrome.customize = false)}
			>{t('phone.edit.done')}</button
		>
	</div>
</Dialog>

<style>
	.switches {
		display: flex;
		padding: 3px;
		gap: 2px;
		background: var(--chip);
		border-radius: 14px;
	}
	.switches button {
		flex: 1;
		min-height: 44px;
		color: inherit;
		border: 0;
		border-radius: 11px;
		background: transparent;
		font: inherit;
		cursor: pointer;
	}
	.switches button[aria-pressed='true'] {
		background: var(--surface);
		box-shadow: 0 1px 4px #0004;
		font-weight: 650;
	}
	ul {
		list-style: none;
		padding: 0;
		margin: 12px 0;
	}
	li,
	.add,
	.size {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	li {
		border-bottom: 1px solid var(--border);
		min-height: 48px;
		gap: 3px;
	}
	li span {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
		margin-left: 5px;
	}
	li button {
		display: grid;
		place-content: center;
		width: 44px;
		height: 44px;
		flex: none;
		background: none;
		border: 0;
		color: inherit;
		cursor: pointer;
	}
	.pages {
		display: flex;
		align-items: center;
		gap: 8px;
		margin: 4px 0 8px;
	}
	button:disabled {
		opacity: 0.35;
	}
	.size {
		margin: 14px 0;
		justify-content: space-between;
		flex-wrap: wrap;
	}
	select {
		flex: 1;
		min-width: 0;
	}
</style>
