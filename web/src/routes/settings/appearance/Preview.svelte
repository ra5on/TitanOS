<script lang="ts">
	// A small piece of TitanOS drawn with the settings being edited. The
	// settings only apply inside this box: its CSS variables override the page's.
	import Backdrop from '#lib/components/Backdrop.svelte';
	import Logo from '#lib/components/Logo.svelte';
	import { formatBytesOf, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { appearance } from '#lib/theme/appearance.svelte.ts';
	import { cssVariables, dataAttributes, type Appearance } from '#lib/theme/themes.ts';

	let { draft }: { draft: Appearance } = $props();

	const style = $derived(
		Object.entries(cssVariables(draft, appearance.systemLight))
			.map(([name, value]) => `${name}: ${value}`)
			.join('; ')
	);
	const attributes = $derived(dataAttributes(draft, appearance.systemLight));

	const user = $derived(session.data?.user);
	const info = $derived(system.info);
	const memoryPercent = $derived(info && info.memory.totalBytes > 0 ? (100 * info.memory.usedBytes) / info.memory.totalBytes : 0);
</script>

<div class="preview" {style} {...attributes}>
	<Backdrop appearance={draft} contained />

	<div class="content">
		<div class="tile bar-top">
			<Logo size={24} />
			<span class="spacer"></span>
			<span class="nav current">{t('nav.dashboard')}</span>
			<span class="nav">{t('nav.docker')}</span>
			<span class="nav">{t('nav.vms')}</span>
			<span class="account">{user?.name} ▾</span>
		</div>

		<div class="two">
			<div class="tile pad">
				<span class="tile-title">{t('tile.system')}</span>
				<div class="row"><span>{t('system.cpu')}</span><span class="mono">{formatPercent(info?.cpu.usagePercent ?? 0)}</span></div>
				<div class="bar"><span style:width="{info?.cpu.usagePercent ?? 0}%"></span></div>
				<div class="row"><span>{t('system.memory')}</span><span class="mono">{info ? formatBytesOf(info.memory.usedBytes, info.memory.totalBytes) : '–'}</span></div>
				<div class="bar"><span style:width="{memoryPercent}%"></span></div>
			</div>
			<div class="tile pad">
				<span class="tile-title">{t('preview.list')}</span>
				{#each [1, 2, 3] as n (n)}
					<div class="item">
						<span class="dot" class:ok={n < 3} aria-hidden="true"></span>
						<span class="grow">{t('preview.item', { n })}</span>
						<span class="muted small">{t(n < 3 ? 'preview.running' : 'preview.stopped')}</span>
					</div>
				{/each}
			</div>
		</div>

		<div class="wrap">
			<div class="menu floating-menu" role="group" aria-label={t('preview.menu')}>
				<span class="menu-head">{t('preview.signedIn', { name: user?.name ?? '', role: user ? t(`role.${user.role}`) : '' })}</span>
				<span class="menu-item current">{t('menu.appearance')}</span>
				<span class="menu-item">{t('menu.restart')}</span>
				<span class="menu-item">{t('menu.logout')}</span>
			</div>
			<div class="buttons">
				<span class="btn btn-primary">{t('appearance.apply')}</span>
				<span class="btn tile">{t('common.cancel')}</span>
			</div>
		</div>

		<!-- A dialog over the dimmed page, with the settings for dialog windows. -->
		<div class="dialog-stage">
			<div class="tile pad stage-content" aria-hidden="true">
				{#each [1, 2] as n (n)}
					<div class="item"><span class="grow">{t('preview.item', { n })}</span><span class="muted small">{t('preview.running')}</span></div>
				{/each}
			</div>
			<span class="dim-layer" aria-hidden="true"></span>
			<div class="dialog-sample" role="group" aria-label={t('preview.dialog')}>
				<strong>{t('preview.dialog')}</strong>
				<span class="muted">{t('preview.dialogText')}</span>
				<div class="buttons end">
					<span class="btn">{t('common.cancel')}</span>
					<span class="btn btn-primary">{t('appearance.apply')}</span>
				</div>
			</div>
		</div>
	</div>
</div>

<style>
	.preview {
		position: relative;
		overflow: hidden;
		min-height: 560px;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		color: var(--text);
		font-size: calc(0.8125rem * var(--font-scale));
	}
	.content {
		position: relative;
		z-index: 1;
		display: flex;
		flex-direction: column;
		gap: var(--gap);
		padding: var(--gap);
	}
	.bar-top {
		display: flex;
		align-items: center;
		gap: 14px;
		padding: 10px 14px;
	}
	.spacer,
	.grow {
		flex: 1 1 auto;
	}
	.nav {
		color: var(--muted);
	}
	.nav.current,
	.menu-item.current {
		background: var(--chip);
		color: var(--text);
	}
	.nav.current {
		padding: 6px 12px;
		border-radius: var(--radius-ctl);
	}
	.account {
		padding: 6px 10px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.two {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: var(--gap);
	}
	.pad {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: var(--tile-pad);
	}
	.row {
		display: flex;
		justify-content: space-between;
		gap: 10px;
	}
	.item {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: var(--row-pad) 0;
		border-top: 1px solid var(--panel-border);
	}
	.small {
		font-size: 0.92em;
	}
	.wrap {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: var(--gap);
	}
	.menu {
		flex: 0 1 240px;
		display: flex;
		flex-direction: column;
		padding: 6px;
	}
	.dialog-stage {
		position: relative;
		display: grid;
		min-height: 150px;
		border-radius: var(--radius-tile);
		overflow: hidden;
	}
	.stage-content,
	.dim-layer,
	.dialog-sample {
		grid-area: 1 / 1;
	}
	.dim-layer {
		background: var(--overlay);
	}
	.dialog-sample {
		align-self: center;
		justify-self: center;
		width: min(320px, calc(100% - 24px));
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: var(--tile-pad);
		background: var(--dialog-bg);
		border: 1px solid var(--dialog-border);
		border-radius: var(--radius-tile);
		-webkit-backdrop-filter: var(--dialog-backdrop);
		backdrop-filter: var(--dialog-backdrop);
		box-shadow: var(--dialog-shadow);
	}
	.buttons.end {
		justify-content: flex-end;
	}
	.menu-head {
		padding: 8px 10px;
		font-size: 0.92em;
		color: var(--muted);
	}
	.menu-item {
		padding: 9px 10px;
		border-radius: var(--radius-ctl);
	}
	.buttons {
		display: flex;
		gap: 8px;
	}
	.buttons .btn {
		height: 40px;
		font-size: inherit;
		cursor: default;
	}
	@media (max-width: 520px) {
		.two {
			grid-template-columns: 1fr;
		}
	}
</style>
