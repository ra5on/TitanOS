<script lang="ts">
	// The table of apps – a Compose stack is one row that opens downward – and,
	// in a wide window, the selected app's details beside it (style W4).
	import AppIcon from '#lib/components/AppIcon.svelte';
	import DetailPane from '#lib/components/explorer/DetailPane.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { isRunning, portLabels } from './container.ts';
	import type { DockerPage } from './docker-page.svelte.ts';

	let { p }: { p: DockerPage } = $props();
	const selected = $derived(p.selected);
</script>

<div class="explorer" class:with-pane={!!selected}>
	<section class="tile ui-card apps" aria-label={t('docker.table.title')}>
		<div class="table-wrap">
			<table class="data-table app-table">
				<thead>
					<tr>
						<th scope="col">{t('docker.table.colName')}</th>
						<th scope="col">{t('docker.table.colState')}</th>
						<th scope="col" class="wide">{t('docker.cpu')}</th>
						<th scope="col" class="wide num">{t('docker.ram')}</th>
						<th scope="col" class="wide">{t('docker.column.ports')}</th>
						<th scope="col" class="upd"><span class="sr-only">{t('docker.update')}</span></th>
						<th scope="col"><span class="sr-only">{t('docker.table.colActions')}</span></th>
					</tr>
				</thead>
				<tbody>
					{#each p.shownGroups as group (group.id)}
						{@const state = p.stateOf.get(group.id) ?? 'stopped'}
						{@const on = p.isOn(group) || state === 'restarting'}
						{@const app = p.groupApp(group)}
						{@const icon = group.members.find((c) => c.icon)?.icon ?? ''}
						{@const cpu = p.groupCpu(group)}
						{@const ports = p.groupPorts(group)}
						{@const busy = p.groupBusy(group)}
						<tr
							class:clickable={!!app || (p.wide && !p.phone)}
							class:selected={selected?.id === group.id}
							aria-selected={selected ? selected.id === group.id : undefined}
							tabindex={p.wide && !p.phone ? 0 : undefined}
							data-app={group.name}
							onclick={(event) => p.rowClick(event, group)}
							ondblclick={(event) => p.rowOpen(event, group)}
							onkeydown={(event) => p.rowKey(event, group)}
							oncontextmenu={(event) => {
								event.preventDefault();
								p.rowMenu = { group, x: event.clientX, y: event.clientY };
							}}
						>
							<td class="name">
								<span class="who">
									{#if group.members.length > 1}<button
											type="button"
											class="twisty"
											class:open={p.expanded.has(group.id)}
											aria-expanded={p.expanded.has(group.id)}
											aria-label={t('docker.table.showContainers', { name: group.name })}
											title={t('docker.table.showContainers', { name: group.name })}
											onclick={() => p.toggleMembers(group)}
											><Icon name="chevronRight" size={14} /></button
										>{:else}<span class="twisty-gap" aria-hidden="true"></span>{/if}
									<span
										class="app-icon"
										style:--hue={p.hue(group.name)}
										class:letter={!icon}
										aria-hidden="true"><AppIcon src={icon} name={group.name} /></span
									>
									<span class="lines">
										<span class="title-line"
											>{#if app}<a class="app-link" href="/docker/apps/{encodeURIComponent(app)}"
													>{group.name}</a
												>{:else}<strong>{group.name}</strong>{/if}

											{#if p.groupUpdate(group)}<span class="chip warn narrow-chip"
													>{t('docker.update')}</span
												>{/if}</span
										>
										<span class="sub">{p.subLine(group)}</span>
										<!-- Narrow screens: the figures in the sub-line instead of columns. -->
										<span class="sub narrow"
											>{on
												? `${t('docker.cpu')} ${formatPercent(cpu)} · ${t('docker.ram')} ${formatBytes(p.groupRam(group))}`
												: ''}{on && ports.length ? ' · ' : ''}{ports.join(', ')}</span
										>
									</span>
								</span>
							</td>
							<td class="state {state}">
								<!-- The mark has a shape of its own per state (dot, ring, triangle). -->
								<span class="mark" aria-hidden="true"></span>{state === 'partial'
									? t('docker.stackSummary', {
											running: p.running(group).length,
											total: group.members.length,
										})
									: t(`docker.table.state.${state}`)}
							</td>
							<td class="wide cpu">
								{#if on}<span class="bar" aria-hidden="true"
										><span style:width="{Math.min(100, Math.max(2, cpu))}%"></span></span
									>{formatPercent(cpu)}{:else}<span class="muted">–</span>{/if}
							</td>
							<td class="wide num"
								>{#if on}{formatBytes(p.groupRam(group))}{:else}<span class="muted">–</span
									>{/if}</td
							>
							<td class="wide ports mono">{ports.length ? ports.join(', ') : '–'}</td>
							<td class="upd"
								>{#if p.groupUpdate(group)}<span class="chip warn"
										><Icon name="download" size={12} />{t('docker.update')}</span
									>{/if}</td
							>
							<td class="actions">
								<button
									type="button"
									class="btn icon-btn small"
									disabled={busy || p.globalBusy}
									aria-label={t(on ? 'docker.table.stopNamed' : 'docker.table.startNamed', {
										name: group.name,
									})}
									title={t(on ? 'docker.stop' : 'docker.start')}
									onclick={() => p.groupAction(group, on ? 'stop' : 'start')}
									><Icon name={on ? 'stop' : 'play'} size={14} /></button
								>
								<button
									type="button"
									class="btn icon-btn small"
									aria-label={t('docker.actionsFor', { name: group.name })}
									title={t('docker.actionsFor', { name: group.name })}
									aria-haspopup="menu"
									aria-expanded={p.rowMenu?.group.id === group.id}
									onpointerdown={() =>
										(p.rowMenuWasOpen = p.rowMenu?.group.id === group.id ? group.id : '')}
									onclick={(event) => p.openRowMenu(group, event.currentTarget)}
									><Icon name="more" size={15} /></button
								>
							</td>
						</tr>
						{#if group.members.length > 1 && p.expanded.has(group.id)}
							{#each group.members as c (c.id)}
								{@const cOn = isRunning(c)}
								<tr class="member" data-container={c.name}>
									<td class="name"
										><span class="member-name"
											><strong>{c.service || c.name}</strong><span class="sub">{c.image}</span
											></span
										></td
									>
									<td class="state {p.memberState(c)}"
										><span class="mark" aria-hidden="true"></span>{t(
											`docker.table.state.${p.memberState(c)}`,
										)}</td
									>
									<td class="wide cpu"
										>{#if cOn}{formatPercent(c.cpuPercent)}{:else}<span class="muted">–</span
											>{/if}</td
									>
									<td class="wide num"
										>{#if cOn}{formatBytes(c.memoryBytes)}{:else}<span class="muted">–</span
											>{/if}</td
									>
									<td class="wide ports mono">{portLabels(c).join(', ') || '–'}</td>
									<td class="upd"
										>{#if c.updateAvailable}<span class="chip warn"
												><Icon name="download" size={12} />{t('docker.update')}</span
											>{/if}</td
									>
									<td class="actions"></td>
								</tr>
							{/each}
						{/if}
					{/each}
				</tbody>
			</table>
		</div>
	</section>
	{#if selected}
		{@const sApp = p.groupApp(selected)}
		{@const sOn = p.isOn(selected)}
		{@const sIcon = selected.members.find((c) => c.icon)?.icon ?? ''}
		<DetailPane
			title={selected.name}
			sub={p.stateOf.get(selected.id) === 'partial'
				? t('docker.stackSummary', {
						running: p.running(selected).length,
						total: selected.members.length,
					})
				: t(`docker.table.state.${p.stateOf.get(selected.id) ?? 'stopped'}`)}
			facts={[
				[t('docker.detail.containers'), String(selected.members.length)],
				[t('docker.cpu'), sOn ? formatPercent(p.groupCpu(selected)) : ''],
				[t('docker.ram'), sOn ? formatBytes(p.groupRam(selected)) : ''],
				[t('docker.column.ports'), p.groupPorts(selected).join(', ')],
				[t('docker.detail.image'), [...new Set(selected.members.map((c) => c.image))].join(', ')],
				[t('docker.update'), p.groupUpdate(selected) ? t('docker.detail.updateReady') : ''],
			]}
			onclose={() => (p.selectedId = null)}
		>
			{#snippet icon()}<span
					class="app-icon big-icon"
					style:--hue={p.hue(selected.name)}
					class:letter={!sIcon}
					aria-hidden="true"><AppIcon src={sIcon} name={selected.name} /></span
				>{/snippet}
			{#snippet actions()}
				{#if sApp}<a class="btn btn-primary" href="/docker/apps/{encodeURIComponent(sApp)}"
						>{t('docker.edit')}</a
					>{/if}
				<button
					type="button"
					class="btn"
					disabled={p.groupBusy(selected) || p.globalBusy}
					onclick={() => p.groupAction(selected, sOn ? 'stop' : 'start')}
					>{t(sOn ? 'docker.stop' : 'docker.start')}</button
				>
				<button
					type="button"
					class="btn icon-btn"
					aria-label={t('docker.actionsFor', { name: selected.name })}
					title={t('docker.actionsFor', { name: selected.name })}
					aria-haspopup="menu"
					onclick={(event) => p.openRowMenu(selected, event.currentTarget)}
					><Icon name="more" size={15} /></button
				>
			{/snippet}
		</DetailPane>
	{/if}
</div>

<style>
	.icon-btn {
		width: var(--control-height);
		padding: 0;
	}
	/* PLAN 75-19, style W4: the list and the selected app's details beside it. */
	.explorer {
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		align-items: start;
	}
	.explorer.with-pane {
		grid-template-columns: minmax(0, 1fr) 300px;
	}
	.app-table tbody tr.selected,
	.app-table tbody tr.selected:hover {
		background: color-mix(in srgb, var(--accent) 16%, transparent);
	}
	.app-table tbody tr:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.twisty,
	.twisty-gap {
		flex: none;
		width: 20px;
		height: 20px;
	}
	.twisty {
		display: grid;
		place-items: center;
		margin-right: calc(-1 * var(--space-1-5));
		padding: 0;
		border: 0;
		border-radius: var(--radius-bar);
		background: none;
		color: var(--muted);
		cursor: pointer;
		transition: transform 0.15s;
	}
	.twisty.open {
		transform: rotate(90deg);
	}
	.twisty:hover {
		color: var(--text);
	}
	.twisty-gap {
		margin-right: calc(-1 * var(--space-1-5));
	}
	.app-table tr.member td {
		background: color-mix(in srgb, var(--bg) 30%, transparent);
		font-size: var(--text-base);
	}
	.member-name {
		display: flex;
		flex-direction: column;
		min-width: 0;
		padding-left: calc(20px + 30px + var(--space-3));
	}
	.member-name strong {
		font-weight: 500;
	}
	.big-icon {
		width: 52px;
		height: 52px;
		border-radius: 14px;
		font-size: var(--text-lg);
	}
	/* The table of apps. */
	.app-table tbody tr {
		transition: background 0.12s ease;
	}
	.app-table tbody tr:hover {
		background: color-mix(in srgb, var(--text) 4%, transparent);
	}
	.app-table tr.clickable {
		cursor: pointer;
	}
	.who {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		min-width: 0;
	}
	.app-icon {
		flex: none;
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		overflow: hidden;
		border-radius: 8px;
		background: var(--chip);
		font-size: var(--text-sm);
		font-weight: 700;
		text-transform: uppercase;
	}
	.app-icon.letter {
		background: hsl(var(--hue) 50% 36%);
		color: #fff;
	}
	.lines {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.lines strong,
	.app-link {
		color: var(--text);
		font-weight: 600;
		text-decoration: none;
		overflow-wrap: anywhere;
	}
	.app-link:hover,
	.app-link:focus-visible {
		text-decoration: underline;
	}
	.title-line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 2px var(--space-2);
		min-width: 0;
	}
	.lines .sub {
		max-width: 22rem;
		overflow-wrap: anywhere;
	}
	.narrow,
	.narrow-chip {
		display: none !important;
	}
	.state {
		white-space: nowrap;
	}
	.state .mark {
		display: inline-block;
		width: 8px;
		height: 8px;
		margin-right: 7px;
		border-radius: 50%;
		background: currentColor;
		vertical-align: 1px;
	}
	.state.stopped .mark {
		background: none;
		border: 1.5px solid currentColor;
	}
	.state.restarting .mark,
	.state.error .mark {
		width: 10px;
		height: 9px;
		border-radius: 0;
		clip-path: polygon(50% 0, 100% 100%, 0 100%);
	}
	.state.running,
	.state.partial {
		color: var(--ok-text);
	}
	.state.partial .mark,
	.state.restarting {
		color: var(--warn-text);
	}
	.state.error {
		color: var(--error-text);
	}
	.state.stopped {
		color: var(--muted);
	}
	.cpu {
		white-space: nowrap;
		font-variant-numeric: tabular-nums;
	}
	.bar {
		display: inline-block;
		width: 56px;
		height: 4px;
		margin-right: var(--space-2);
		vertical-align: middle;
		border-radius: 2px;
		background: var(--track);
		overflow: hidden;
	}
	.bar > span {
		display: block;
		height: 100%;
		background: var(--accent);
	}
	.num {
		font-variant-numeric: tabular-nums;
	}
	.ports {
		max-width: 10rem;
		color: var(--muted);
		overflow-wrap: anywhere;
	}
	.upd {
		width: 1%;
		white-space: nowrap;
	}
	.chip :global(svg) {
		vertical-align: -1px;
		margin-right: 3px;
	}
	.actions {
		width: 1%;
		white-space: nowrap;
		text-align: right;
	}
	.actions .btn + .btn {
		margin-left: var(--space-1-5);
	}
	.small {
		min-height: 30px;
		height: 30px;
		width: 30px;
	}
	@media (prefers-reduced-motion: reduce) {
		.app-table tbody tr {
			transition: none;
		}
	}
	/* Fewer columns as the window narrows; the figures move into the sub-line. */
	@media (max-width: 899px) {
		.app-table .ports {
			max-width: 6rem;
		}
		.lines .sub {
			max-width: 13rem;
		}
		.app-table td.upd,
		.app-table th.upd {
			display: none;
		}
		.lines .narrow-chip {
			display: inline-flex !important;
			font-size: var(--text-xs);
		}
		.bar {
			width: 36px;
		}
	}
	@media (max-width: 699px) {
		.app-table .wide {
			display: none;
		}
		.lines .narrow {
			display: block !important;
		}
	}
	/* Phones: each app a stacked row – name with its figures, state, actions. */
	@media (max-width: 639px) {
		.app-table thead {
			display: none;
		}
		.app-table,
		.app-table tbody {
			display: block;
		}
		.app-table tr {
			display: grid;
			grid-template-columns: minmax(0, 1fr) auto;
			grid-template-areas:
				'name actions'
				'state actions';
			align-items: center;
			column-gap: var(--space-2);
			padding: var(--space-2-5) var(--space-4);
			border-bottom: 1px solid var(--border);
		}
		.app-table tr:last-child {
			border-bottom: 0;
		}
		.app-table td {
			display: block;
			padding: 0 !important;
			border: 0 !important;
		}
		.app-table td.name {
			grid-area: name;
		}
		.app-table td.state {
			grid-area: state;
			padding-left: 42px !important;
			white-space: normal;
		}
		.app-table td.upd {
			display: none;
		}
		.app-table td.actions {
			grid-area: actions;
			width: auto;
		}
		.lines .sub {
			max-width: none;
			white-space: normal;
		}
		.actions .small {
			width: 2.75rem;
			height: 2.75rem;
		}
	}
</style>
