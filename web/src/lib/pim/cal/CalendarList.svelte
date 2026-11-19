<script lang="ts">
	// 1.11 (step 15): the calendars with color **and** name (draft 01, phone
	// 40): a check box shows or hides one, a hidden one says "ausgeblendet"
	// in words and keeps only the colored edge. "Geburtstage" comes from the
	// contacts and is read-only (no ⋯). "Neuer Kalender" and the iPhone page
	// below.
	// 1.13: sorted by dragging the grip or with the keys (SortGrip; Alt+↑/↓
	// on the name), kept per account on the NAS (orderOf('calendar')). In the
	// desktop's side bar "Neuer Kalender" and the iPhone page stand at its
	// foot (`adds` false).
	// 1.15 (draft 34): on the phone a sheet as in Apple Kalender – the own
	// calendars under "TitanOS · <account>", the birthdays under "Weitere",
	// a tick shows one (the word "ausgeblendet" when not), the arrow opens
	// its settings (name, color, import, export, delete); below "Neuer
	// Kalender", "Alle ausblenden"/"Alle einblenden" and the iPhone page.
	import Icon from '#lib/components/Icon.svelte';
	import SortGrip, { sortKey } from '#lib/components/SortGrip.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { calColor, type Calendar } from '../calendar.ts';
	import { calName } from '../calendar-text.ts';

	type Props = {
		calendars: Calendar[];
		phone?: boolean;
		ontoggle: (c: Calendar) => void;
		onedit: (c: Calendar) => void;
		onnew: () => void;
		adds?: boolean;
		/** The calendars in a new order (ids). */
		onsort?: (ids: string[]) => void;
		/** 1.15, phone: the account's name over its calendars. */
		account?: string;
		/** 1.15, phone: hides (true) or shows every calendar. */
		onall?: (hide: boolean) => void;
	};
	let {
		calendars,
		phone = false,
		ontoggle,
		onedit,
		onnew,
		adds = true,
		onsort,
		account = '',
		onall,
	}: Props = $props();
	const keys = $derived(calendars.map((c) => c.id));
	const groups = $derived(
		[
			{
				key: 'own',
				label: account ? t('calendar.group.own', { name: account }) : t('calendar.myCalendars'),
				list: calendars.filter((c) => c.kind === 'own'),
			},
			{
				key: 'more',
				label: t('calendar.group.more'),
				list: calendars.filter((c) => c.kind !== 'own'),
			},
		].filter((g) => g.list.length),
	);
	const anyShown = $derived(calendars.some((c) => !c.hidden));
</script>

{#snippet sub(c: Calendar)}
	{#if c.kind === 'birthdays'}
		<small>{t('calendar.birthdaysNote')}</small>
	{/if}
	{#if c.unavailable}
		<small>{t('calendar.birthdaysUnreadable')}</small>
	{/if}
	{#if c.hidden}<small class="hidden-word">{t('calendar.hidden')}</small>{/if}
{/snippet}

{#if phone}
	<div class="cals phone sheet">
		{#each groups as g (g.key)}
			<h3 class="gh">{g.label}</h3>
			<div class="card">
				{#each g.list as c (c.id)}
					<div
						class="calrow"
						class:off={c.hidden}
						style:--c={calColor(c.color)}
						data-calendar={c.id}
					>
						<label>
							<input type="checkbox" checked={!c.hidden} onchange={() => ontoggle(c)} />
							<span class="tick" aria-hidden="true"
								>{#if !c.hidden}<Icon name="check" size={18} />{/if}</span
							>
							<span class="dot" aria-hidden="true"></span>
							<span class="cn"><span class="name">{calName(c)}</span>{@render sub(c)}</span>
						</label>
						{#if !c.readOnly && c.kind !== 'birthdays'}
							<button
								type="button"
								class="ib quiet"
								aria-label={t('calendar.settingsOf', { name: c.name })}
								title={t('calendar.settingsOf', { name: c.name })}
								onclick={() => onedit(c)}><Icon name="chevronRight" size={18} /></button
							>
						{/if}
					</div>
				{/each}
			</div>
		{/each}
		{#if adds}
			<div class="card actions">
				<button type="button" class="act" onclick={onnew}>{t('calendar.newCalendar')}</button>
				{#if onall}
					<button type="button" class="act" onclick={() => onall(anyShown)}
						>{anyShown ? t('calendar.hideAll') : t('calendar.showAll')}</button
					>
				{/if}
				<a class="act" href="/calendar/iphone">{t('calendar.side.iphone')}</a>
			</div>
		{/if}
	</div>
{:else}
	<div class="cals">
		<h3>{phone ? t('calendar.phoneCalendars') : t('calendar.myCalendars')}</h3>
		<div class="list" data-sort-list>
			{#each calendars as c (c.id)}
				<div
					class="calrow"
					class:off={c.hidden}
					style:--c={calColor(c.color)}
					data-calendar={c.id}
					data-sort-group={onsort ? '' : undefined}
					data-sort-key={onsort ? c.id : undefined}
				>
					<label>
						<input
							type="checkbox"
							checked={!c.hidden}
							onchange={() => ontoggle(c)}
							onkeydown={(e) => onsort && sortKey(e, keys, c.id, calName(c), onsort)}
						/>
						<span class="sw" aria-hidden="true"></span>
						<span class="cn">
							<span class="name">{calName(c)}</span>
							{@render sub(c)}
						</span>
					</label>
					{#if !c.readOnly && c.kind !== 'birthdays'}
						<button
							type="button"
							class="ib quiet"
							aria-label={t('calendar.settingsOf', { name: c.name })}
							title={t('calendar.settingsOf', { name: c.name })}
							onclick={() => onedit(c)}><Icon name="more" size={16} /></button
						>
					{/if}
					{#if onsort && keys.length > 1}
						<SortGrip group="" key={c.id} {keys} name={calName(c)} onmove={onsort} />
					{/if}
				</div>
			{/each}
		</div>
		{#if adds}<div class="adds">
				<button type="button" class="btn add" onclick={onnew}
					><Icon name="plus" size={16} />{t('calendar.newCalendar')}</button
				>
				<a class="btn add" href="/calendar/iphone"
					><Icon name="device" size={16} />{t('calendar.side.iphone')}</a
				>
			</div>{/if}
	</div>
{/if}

<style>
	.cals {
		display: flex;
		flex-direction: column;
		gap: 3px;
	}
	h3 {
		margin: 0 0 var(--space-1);
		font-size: var(--text-base);
		font-weight: 600;
	}
	.list {
		display: flex;
		flex-direction: column;
		gap: 3px;
	}
	.calrow {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		min-height: 44px;
		padding: 0 var(--space-1) 0 var(--space-2);
		border-radius: var(--radius-ctl);
		background: var(--srow-bg);
	}
	label {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 44px;
		cursor: pointer;
	}
	input {
		margin: 0;
		width: 16px;
		height: 16px;
		accent-color: var(--accent);
		flex: none;
	}
	.phone input {
		width: 20px;
		height: 20px;
	}
	.sw {
		flex: none;
		width: 12px;
		height: 12px;
		border-radius: 3px;
		background: var(--c);
	}
	.off .sw {
		background: none;
		border: 2px solid var(--c);
	}
	.cn {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		padding: 3px 0;
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.cn small,
	.off .name {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.ib {
		display: inline-grid;
		place-items: center;
		flex: none;
		min-width: 34px;
		min-height: 34px;
		padding: 0;
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--muted);
		cursor: pointer;
	}
	.phone .ib {
		min-width: 44px;
		min-height: 44px;
	}
	.ib:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.ib:focus-visible {
		outline: 2px solid var(--accent);
	}
	.adds {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-2);
		margin-top: var(--space-2);
	}
	.add {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		min-height: 36px;
	}

	/* ---- Phone: the sheet of draft 34 ---- */
	.sheet {
		gap: var(--space-2);
	}
	.gh {
		margin: var(--space-3) var(--space-3) 0;
		color: var(--muted);
		font-size: var(--text-sm);
		font-weight: 600;
		letter-spacing: 0.04em;
		text-transform: uppercase;
		overflow-wrap: anywhere;
	}
	.card {
		display: flex;
		flex-direction: column;
		border-radius: 12px;
		background: var(--surface);
		overflow: clip;
	}
	.sheet .calrow {
		gap: 0;
		min-height: 52px;
		padding: 0 var(--space-1) 0 var(--space-2);
		border-radius: 0;
		background: none;
	}
	.sheet .calrow + .calrow {
		border-top: 1px solid var(--srow-line);
	}
	.sheet label {
		position: relative;
		gap: var(--space-3);
		min-height: 52px;
	}
	.sheet input {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		opacity: 0;
		cursor: pointer;
	}
	.sheet input:focus-visible + .tick {
		outline: 2px solid var(--accent);
		border-radius: 4px;
	}
	.tick {
		flex: none;
		display: grid;
		place-items: center;
		width: 22px;
		color: var(--accent);
	}
	.dot {
		flex: none;
		width: 14px;
		height: 14px;
		border-radius: 50%;
		background: var(--c);
	}
	.off .dot {
		background: none;
		border: 2px solid var(--c);
	}
	.sheet .name {
		font-size: var(--text-md);
	}
	.sheet .off .name {
		color: var(--text);
	}
	.actions .act {
		display: flex;
		align-items: center;
		justify-content: center;
		min-height: 50px;
		padding: var(--space-2) var(--space-3);
		border: 0;
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-md);
		text-align: center;
		text-decoration: none;
		cursor: pointer;
	}
	.actions .act + .act {
		border-top: 1px solid var(--srow-line);
	}
</style>
