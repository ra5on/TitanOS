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
	};
	let { calendars, phone = false, ontoggle, onedit, onnew, adds = true, onsort }: Props = $props();
	const keys = $derived(calendars.map((c) => c.id));
</script>

<div class="cals" class:phone>
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
						{#if c.kind === 'birthdays'}
							<small>{t('calendar.birthdaysNote')}</small>
						{/if}
						{#if c.unavailable}
							<small>{t('calendar.birthdaysUnreadable')}</small>
						{/if}
						{#if c.hidden}<small class="hidden-word">{t('calendar.hidden')}</small>{/if}
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
	.phone .add {
		min-height: 44px;
	}
</style>
