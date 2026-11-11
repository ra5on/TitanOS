<script lang="ts">
	// PLAN 63/66: the controls of the remote view – on the phone a bar of its
	// own, in a window a part of the window's ⋯ menu, else a bar above the
	// picture: state, sound, quality, the clipboard, uploads and downloads.
	import Icon from '../components/Icon.svelte';
	import Dropdown from '../components/Dropdown.svelte';
	import { apiPath } from '../cluster-node.ts';
	import { formatBytes, formatPercent } from '../format.ts';
	import { t, type MessageKey } from '../i18n/index.svelte.ts';
	import { QUALITIES, TRANSPORTS } from './remote.ts';
	import type { RemoteSession } from './remote-session.svelte.ts';
	import RemoteVolume from './RemoteVolume.svelte';

	let {
		s,
		title,
		phone,
		onopentab,
		onback,
		inTitle,
		menuHost,
	}: {
		s: RemoteSession;
		title: string;
		phone: boolean;
		onopentab: () => void;
		onback?: () => void;
		inTitle: boolean;
		menuHost: HTMLElement | null;
	} = $props();

	/** Moves the menu's part into the window's ⋯ menu (it stays this component's). */
	function portal(node: HTMLElement, target: HTMLElement) {
		target.appendChild(node);
		return { destroy: () => node.remove() };
	}
	let section = $state<'' | 'files' | 'clip' | 'quality'>('');
</script>

{#snippet qualityItems(close: () => void)}
	{#each QUALITIES as q (q)}
		<button
			type="button"
			role="menuitemradio"
			aria-checked={s.quality === q}
			onclick={() => {
				s.setQuality(q, s.prefs.fps ?? 30);
				close();
			}}><span>{t(`viewer.q.${q}` as MessageKey)}</span></button
		>
	{/each}
	<hr />
	{#each s.FPS as fps (fps)}
		<button
			type="button"
			role="menuitemradio"
			aria-checked={(s.prefs.fps ?? 30) === fps}
			onclick={() => {
				s.setQuality(s.quality, fps);
				close();
			}}><span>{t('viewer.fpsValue', { fps })}</span></button
		>
	{/each}
	<hr />
	{#each TRANSPORTS as tr (tr)}
		<button
			type="button"
			role="menuitemradio"
			aria-checked={(s.prefs.transport ?? 'auto') === tr}
			onclick={() => {
				s.setQuality(s.quality, s.prefs.fps ?? 30, tr);
				close();
			}}
			><span
				>{t(`viewer.transport.${tr}` as MessageKey)}<small class="muted hint-line"
					>{t(`viewer.transport.${tr}Hint` as MessageKey)}</small
				></span
			></button
		>
	{/each}
{/snippet}

{#snippet clipboardPanel()}
	<div class="clip">
		<b>{t('viewer.clipboardRemote')}</b>
		{#if s.remoteClip}
			<pre class="remote-clip">{s.remoteClip}</pre>
			<button type="button" class="btn" onclick={s.copyRemote}
				><Icon name="copy" size={15} />{s.copied ? t('viewer.copied') : t('viewer.copy')}</button
			>
		{:else}<p class="muted">{t('viewer.clipboardEmpty')}</p>{/if}
	</div>
	<hr />
	<form class="clip" onsubmit={s.sendClip}>
		<label class="stack"
			><span>{t('viewer.clipboardField')}</span>
			<textarea class="field" rows="3" bind:value={s.clipDraft} oninput={() => (s.clipSent = false)}
			></textarea>
		</label>
		<button type="submit" class="btn" disabled={!s.clipDraft || !s.live}
			>{t('viewer.clipboardSend')}</button
		>
		{#if s.clipSent}<p class="muted small" role="status">{t('viewer.clipboardSent')}</p>{/if}
	</form>
{/snippet}

{#snippet filesPanel()}
	<button type="button" class="btn" disabled={!s.live} onclick={() => s.fileInput?.click()}
		><Icon name="upload" size={15} />{t('viewer.upload')}</button
	>
	<p class="muted small">{t('viewer.uploadHint')}</p>
	{#if s.uploads.length}
		<ul class="list">
			{#each s.uploads as u (u.id)}
				<li>
					<span class="name">{u.name}</span>
					<span class="muted small" role="status"
						>{u.state === 'done'
							? t('viewer.uploadDone')
							: u.state === 'failed'
								? u.error
									? t(`viewer.uploadError.${u.error}` as 'viewer.uploadError.failed')
									: t('viewer.uploadFailed')
								: formatPercent(u.size ? (100 * u.sent) / u.size : 100)}</span
					>
					{#if u.state === 'run'}<progress max={u.size || 1} value={u.sent}></progress>{/if}
				</li>
			{/each}
		</ul>
	{/if}
	<hr />
	<b>{t('viewer.downloads')}</b>
	{#if s.downloads.length}
		<ul class="list">
			{#each s.downloads as d (d.id)}
				<li>
					<a
						href={apiPath(
							`/viewer/${encodeURIComponent(s.app)}/downloads/${encodeURIComponent(d.id)}`,
						)}
						download={d.name}
						><Icon name="download" size={14} /> <span class="name">{d.name}</span></a
					>
					<span class="muted small">{formatBytes(d.size)}</span>
				</li>
			{/each}
		</ul>
	{:else}<p class="muted small">{t('viewer.noDownloads')}</p>{/if}
{/snippet}

{#if phone}
	<header class="phone-head">
		<button type="button" class="icon-btn big" onclick={onback} aria-label={t('desktop.phone.home')}
			><Icon name="chevronLeft" size={22} /></button
		>
		<b class="title">{title}</b>
		<span class="pill" class:live={s.live} class:ended={!s.live && s.phase !== 'connecting'}
			><span class="dot" aria-hidden="true"></span>{t('viewer.pill')}{#if s.stateWord}<span
					class="sr-only">: {s.stateWord}</span
				>{/if}</span
		>
		<span class="fill"></span>
		<button
			type="button"
			class="icon-btn big"
			onclick={s.toggleMute}
			aria-pressed={s.prefs.muted}
			aria-label={s.prefs.muted ? t('viewer.unmute') : t('viewer.mute')}
			><Icon name="volume" size={19} />{#if s.prefs.muted}<span class="slash" aria-hidden="true"
				></span>{/if}</button
		>
		<button
			type="button"
			class="icon-btn big"
			onclick={s.openKeyboard}
			disabled={!s.live}
			aria-label={t('viewer.keyboard')}><Icon name="keyboard" size={19} /></button
		>
		<Dropdown label={t('viewer.more')} icon="more" align="right" kind="panel">
			{#snippet children(close)}
				<b>{t('viewer.quality')}</b>
				<div class="menu-list" role="menu">{@render qualityItems(close)}</div>
				<hr />
				{@render clipboardPanel()}
				<hr />
				{@render filesPanel()}
				<hr />
				<button type="button" class="btn" onclick={onopentab}
					><Icon name="external" size={15} />{t('desktop.window.openTab')}</button
				>
			{/snippet}
		</Dropdown>
	</header>
{:else if inTitle}
	{#if menuHost}
		<div class="title-menu" use:portal={menuHost}>
			{#if s.hello?.audio && !s.noAudio}
				<span class="vol"><RemoteVolume {s} /></span>
			{:else if s.noAudio}<p class="muted small pad">{t('viewer.noAudio')}</p>{/if}
			<hr />
			<button type="button" role="menuitem" disabled={!s.live} onclick={() => s.fileInput?.click()}
				><Icon name="upload" size={16} /><span>{t('viewer.menu.open')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				aria-expanded={section === 'files'}
				onclick={() => (section = section === 'files' ? '' : 'files')}
				><Icon name="download" size={16} /><span>{t('viewer.downloads')}</span><kbd
					>{s.downloads.length}</kbd
				></button
			>
			{#if section === 'files'}<div class="sub">{@render filesPanel()}</div>{/if}
			<button
				type="button"
				role="menuitem"
				aria-expanded={section === 'clip'}
				onclick={() => (section = section === 'clip' ? '' : 'clip')}
				><Icon name="paste" size={16} /><span>{t('viewer.clipboard')}</span></button
			>
			{#if section === 'clip'}<div class="sub">{@render clipboardPanel()}</div>{/if}
			<button
				type="button"
				role="menuitem"
				aria-expanded={section === 'quality'}
				onclick={() => (section = section === 'quality' ? '' : 'quality')}
				><Icon name="monitor" size={16} /><span>{t('viewer.quality')}</span><kbd
					>{t(`viewer.q.${s.quality}` as MessageKey)} ›</kbd
				></button
			>
			{#if section === 'quality'}<div class="sub" role="group">
					{@render qualityItems(() => (section = ''))}
				</div>{/if}
			<hr />
			<button type="button" role="menuitem" onclick={s.reconnect}
				><Icon name="restart" size={16} /><span>{t('viewer.reconnect')}</span></button
			>
			<p class="menu-line muted">
				<span class="pill" class:live={s.live} class:ended={!s.live && s.phase !== 'connecting'}
					><span class="dot" aria-hidden="true"></span>{t('viewer.pill')}{#if s.stateWord}
						· {s.stateWord}{/if}</span
				>{#if s.live && s.statsText}<span class="stats">{s.statsText}</span>{/if}
			</p>
		</div>
	{/if}
{:else}
	<div class="view-bar" role="toolbar" aria-label={t('viewer.toolbar', { app: title })}>
		<span class="pill" class:live={s.live} class:ended={!s.live && s.phase !== 'connecting'}
			><span class="dot" aria-hidden="true"></span>{t('viewer.pill')}{#if s.stateWord}
				· {s.stateWord}{/if}</span
		>
		{#if s.live && s.statsText}<span class="stats muted">{s.statsText}</span>{/if}
		<span class="fill"></span>
		{#if s.hello?.audio && !s.noAudio}
			<span class="vol"><RemoteVolume {s} /></span>
		{:else if s.noAudio}<span class="muted small">{t('viewer.noAudio')}</span>{/if}
		<Dropdown
			label={t('viewer.quality')}
			text={t('viewer.qualityValue', { mode: t(`viewer.q.${s.quality}` as MessageKey) })}
			align="right"
			compact
			icon="monitor"
		>
			{#snippet children(close)}{@render qualityItems(close)}{/snippet}
		</Dropdown>
		<Dropdown
			label={t('viewer.clipboard')}
			text={t('viewer.clipboard')}
			icon="paste"
			kind="panel"
			align="right"
			compact
		>
			{#snippet children()}{@render clipboardPanel()}{/snippet}
		</Dropdown>
		<Dropdown
			label={t('viewer.files')}
			text={t('viewer.files')}
			icon="download"
			kind="panel"
			align="right"
			compact
		>
			{#snippet children()}{@render filesPanel()}{/snippet}
		</Dropdown>
	</div>
{/if}

<style>
	/* PLAN 66: the part of the window's ⋯ menu. */
	.title-menu {
		display: flex;
		flex-direction: column;
		gap: 2px;
		max-height: min(70vh, 560px);
		overflow-y: auto;
	}
	.title-menu .vol {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-1) var(--space-2-5);
	}
	.title-menu .sub {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		margin: 2px 0 var(--space-1-5) 26px;
		padding: var(--space-1-5) 0 var(--space-1-5) var(--space-2-5);
		border-left: 2px solid var(--border);
	}
	.title-menu .menu-line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1) var(--space-2-5);
		margin: var(--space-1-5) var(--space-2-5) 2px;
		font-size: var(--text-xs);
	}
	.title-menu .pad {
		margin: var(--space-1) var(--space-2-5);
	}
	.view-bar,
	.phone-head {
		flex: none;
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 40px;
		padding: var(--space-1) var(--space-2-5);
		background: var(--header);
		border-bottom: 1px solid var(--border);
		color: var(--text);
		font-size: var(--text-sm);
		user-select: none;
		-webkit-user-select: none;
	}
	.view-bar :global(.dropdown-panel),
	.phone-head :global(.dropdown-panel) {
		user-select: text;
		-webkit-user-select: text;
	}
	.view-bar :global(.btn) {
		min-height: 1.875rem;
		height: 1.875rem;
		font-size: var(--text-sm);
	}
	.phone-head {
		min-height: calc(52px + env(safe-area-inset-top));
		padding: env(safe-area-inset-top) var(--space-1-5) 0;
		gap: var(--space-1);
	}
	.title {
		min-width: 0;
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.fill {
		flex: 1;
	}
	.pill {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		padding: 2px var(--space-2-5);
		border-radius: 999px;
		background: var(--chip);
		color: var(--muted);
		font-weight: 600;
		white-space: nowrap;
	}
	.pill.live {
		background: color-mix(in srgb, var(--ok-text) 16%, transparent);
		color: var(--ok-text);
	}
	.pill.ended {
		color: var(--muted);
	}
	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: currentColor;
	}
	.stats {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.vol {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	.icon-btn {
		position: relative;
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		border: 0;
		border-radius: 7px;
		background: none;
		color: var(--text);
		cursor: pointer;
		flex: none;
	}
	.icon-btn.big {
		width: 40px;
		height: 40px;
	}
	.icon-btn:hover {
		background: color-mix(in srgb, var(--text) 9%, transparent);
	}
	.icon-btn:disabled {
		opacity: 0.5;
	}
	.slash {
		position: absolute;
		width: 2px;
		height: 60%;
		background: currentColor;
		transform: rotate(45deg);
	}
	.clip {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		align-items: flex-start;
	}
	.clip label {
		align-self: stretch;
	}
	.remote-clip {
		margin: 0;
		max-height: 8rem;
		max-width: 100%;
		overflow: auto;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-size: var(--text-sm);
		padding: var(--space-2);
		border-radius: var(--radius-small);
		background: var(--chip);
		align-self: stretch;
	}
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
	}
	.list li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1) var(--space-2);
	}
	.list .name {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.list a {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		flex: 1;
		min-width: 0;
	}
	.list progress {
		width: 100%;
	}
	.menu-list {
		display: flex;
		flex-direction: column;
	}
	.muted {
		color: var(--muted);
	}
	.small {
		font-size: var(--text-sm);
	}
	@container (max-width: 760px) {
		.stats {
			display: none;
		}
	}
	@container (max-width: 600px) {
		.view-bar :global(.pct),
		.view-bar :global(.volume) {
			display: none;
		}
	}
	.hint-line {
		display: block;
		font-size: var(--text-sm);
	}
</style>
