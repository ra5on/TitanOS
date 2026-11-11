<script lang="ts">
	// After "Install": the log of the job until the app runs, and what to do then.
	import { formatBytesOf } from '#lib/format.ts';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import Icon from '#lib/components/Icon.svelte';
	import type { InstallForm } from '#lib/compose/template.ts';
	import type { Installer } from './install-form.svelte.ts';

	let { ins, form }: { ins: Installer; form: InstallForm } = $props();
</script>

<section class="tile log-tile" aria-live="polite">
	<div class="log-head">
		<span class="state {ins.phase}"
			>{#if ins.phase === 'installing'}<span class="spinner" aria-hidden="true"></span>{t(
					'appform.installing',
					{ name: form.name },
				)}{:else if ins.phase === 'done'}{t('appform.installed', {
					name: form.name,
				})}{:else}{t('appform.installFailed', { name: form.name })}{/if}</span
		>
		<span class="muted mono"
			>{Math.floor(ins.elapsed / 60)}:{String(ins.elapsed % 60).padStart(2, '0')}</span
		>
	</div>
	<div class="log mono">
		<p>
			<span class="muted">{t('appform.logFolders')}</span>
			{form.paths.map((path) => path.host).join(', ') || '–'}
			<span class="ok">✓</span>
		</p>
		{#if ins.progress && ins.progress.images.length}
			<p>
				<span class="muted">{t('appform.logImages')}</span>
				{#each ins.progress.images as image, index (image.name)}{index ? ', ' : ''}{image.name}
					<span class={image.state === 'done' ? 'ok' : image.state === 'failed' ? 'bad' : 'muted'}
						>{image.state === 'done' ? '✓' : image.state === 'failed' ? '✕' : '…'}</span
					>{/each}
			</p>
			{#if ins.progress.totalBytes > 0}
				<p class="bytes">
					<span class="bar" aria-hidden="true"
						><span
							style:width="{Math.min(
								100,
								(100 * ins.progress.doneBytes) / ins.progress.totalBytes,
							)}%"
						></span></span
					>{formatBytesOf(ins.progress.doneBytes, ins.progress.totalBytes)}
				</p>
			{/if}
		{/if}
		{#if Object.values(ins.selectedHardware).some((list) => list.length)}
			<p>
				<span class="muted">{t('appform.logDevices')}</span>
				{Object.entries(ins.selectedHardware)
					.filter(([, list]) => list.length)
					.map(([service, list]) => `${service}: ${list.length}`)
					.join(', ')}
			</p>
		{/if}
		<p>
			<span class="muted">{t('appform.logCommand')}</span>
			<span class="accent">docker compose -p {form.name} up -d</span>
		</p>
		{#each ins.progress?.other ?? [] as line, index (index)}<p class="indent">{line}</p>{/each}
		{#if ins.phase === 'done'}
			{#if ins.httpsResult}<p>
					<span class="muted">{t('appform.httpsName')}:</span>
					{#if ins.httpsResult.ok}{ins.https.name}.{ins.proxy?.suffixes[0] ?? ''}
						<span class="ok">✓</span>{:else}<span class="bad">{ins.httpsResult.text}</span>{/if}
				</p>{/if}
			<p class="ok">{t('appform.logSuccess')}</p>
		{:else if ins.phase === 'failed'}
			<p class="bad">{t('appform.logFailed')}</p>
		{/if}
	</div>
	<div class="foot">
		{#if ins.phase === 'installing'}
			<button type="button" class="btn" onclick={() => ins.leave('/docker')}
				>{t('appform.background')}</button
			>
			<span class="muted note">{t('appform.backgroundHint')}</span>
		{:else if ins.phase === 'done'}
			<button type="button" class="btn btn-primary" onclick={() => ins.leave('/docker')}
				>{t('common.done')}</button
			>
			{#each ins.links.slice(0, 1) as link (link.href)}<a
					class="btn"
					href={link.href}
					target="_blank"
					rel="noreferrer"
					><Icon name="external" size={15} />{t('appform.open', { name: form.name })}</a
				>{/each}
			<a class="btn" href="/docker/apps/{form.name}" onclick={() => (ins.allowLeave = true)}
				>{t('docker.edit')}</a
			>
		{:else}
			<a
				class="btn btn-primary"
				href="/docker/apps/{form.name}"
				onclick={() => (ins.allowLeave = true)}>{t('docker.edit')}</a
			>
			<button type="button" class="btn" onclick={() => ins.leave('/docker')}
				>{t('docker.title')}</button
			>
		{/if}
	</div>
</section>

<style>
	.foot {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		margin-top: var(--space-2);
		padding: var(--space-3) var(--space-4) var(--space-4) calc(min(17rem, 30%) + 2 * var(--space-4));
		border-top: 1px solid var(--border);
	}
	.log-tile {
		display: flex;
		flex-direction: column;
	}
	.log-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4);
		border-bottom: 1px solid var(--border);
	}
	.state {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2-5);
		font-weight: 600;
	}
	.state.done {
		color: var(--ok-text);
	}
	.state.failed {
		color: var(--error-text);
	}
	.spinner {
		width: 14px;
		height: 14px;
		border: 2px solid var(--accent);
		border-right-color: transparent;
		border-radius: 50%;
		animation: spin 0.9s linear infinite;
	}
	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.spinner {
			animation: none;
		}
	}
	.log {
		padding: var(--space-3) var(--space-4);
		font-size: var(--text-sm);
		line-height: 1.75;
		overflow-wrap: anywhere;
	}
	.log p {
		margin: 0;
	}
	.log .indent {
		padding-left: 1.5em;
		color: var(--muted);
	}
	.ok {
		color: var(--ok-text);
	}
	.bad {
		color: var(--error-text);
	}
	.accent {
		color: var(--accent);
	}
	.bytes {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		padding-left: 1.5em;
	}
	.bar {
		position: relative;
		display: inline-block;
		width: 12rem;
		height: 6px;
		border-radius: 3px;
		background: var(--track);
		overflow: hidden;
	}
	.bar span {
		position: absolute;
		inset: 0 auto 0 0;
		background: var(--accent);
	}
	.log-tile .foot {
		padding-left: var(--space-4);
	}
	.note {
		font-size: var(--text-sm);
	}
	@media (max-width: 760px) {
		.foot {
			padding-left: var(--space-3);
		}
	}
</style>
