<script lang="ts">
	// 1.13: the folded side bar of Kontakte (ColumnFrame's rail): all
	// contacts, each group, the suggestions and the duplicates as icons,
	// named for screen readers and as tooltip.
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { Group } from '../contacts.ts';
	import type { SidePlace } from './ContactSide.svelte';

	type Props = {
		groups: Group[];
		place: SidePlace;
		mail: boolean;
		duplicates: number;
		onpick: (place: SidePlace) => void;
	};
	let { groups, place, mail, duplicates, onpick }: Props = $props();

	const entries = $derived<{ key: string; icon: IconName; label: string; to: SidePlace }[]>([
		{ key: 'book', icon: 'book', label: t('contacts.side.contacts'), to: { kind: 'book' } },
		...groups.map((g) => ({
			key: 'g:' + g.id,
			icon: 'users' as IconName,
			label: g.name || t('contacts.groups.unnamed'),
			to: { kind: 'group', id: g.id } as SidePlace,
		})),
		...(mail
			? [
					{
						key: 'sug',
						icon: 'mail' as IconName,
						label: t('contacts.suggestions'),
						to: { kind: 'suggestions' } as SidePlace,
					},
				]
			: []),
		...(duplicates > 0
			? [
					{
						key: 'dup',
						icon: 'copy' as IconName,
						label: t('contacts.dup.title'),
						to: { kind: 'duplicates' } as SidePlace,
					},
				]
			: []),
	]);
	const isHere = (to: SidePlace) =>
		to.kind === place.kind &&
		(to.kind !== 'group' || (place.kind === 'group' && place.id === to.id));
</script>

<nav class="rail" aria-label={t('desktop.app.contacts')}>
	{#each entries as e (e.key)}
		<button
			type="button"
			class="ri"
			class:current={isHere(e.to)}
			aria-current={isHere(e.to) ? 'page' : undefined}
			aria-label={e.label}
			title={e.label}
			onclick={() => onpick(e.to)}><Icon name={e.icon} size={18} /></button
		>
	{/each}
</nav>

<style>
	.rail {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 2px;
	}
	.ri {
		display: inline-grid;
		place-items: center;
		width: 44px;
		height: 40px;
		border: 0;
		border-radius: var(--radius-small);
		background: none;
		color: var(--muted);
		cursor: pointer;
	}
	.ri:hover {
		background: var(--chip);
		color: var(--text);
	}
	.ri.current {
		background: color-mix(in srgb, var(--accent) 16%, transparent);
		color: var(--text);
	}
	.ri:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
</style>
