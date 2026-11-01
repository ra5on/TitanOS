<script lang="ts">
	// Devices no driver took and firmware the kernel did not find, with a
	// hint for each (Einstellungen › Hardware and the hardware report).
	import type { FirmwareMissing, UnboundDevice } from '#lib/hwcheck.ts';
	import { attention } from '#lib/hwcheck.ts';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = { unbound: UnboundDevice[]; firmware: FirmwareMissing[] };
	let { unbound, firmware }: Props = $props();

	const important = $derived(attention(unbound));
	const quiet = $derived(unbound.filter((device) => device.hint === 'not_needed'));

	function hintText(device: UnboundDevice): string {
		switch (device.hint) {
			case 'plugin': {
				const installed = device.plugins?.find((plugin) => plugin.installed);
				if (installed) return t('hwcheck.hint.pluginInstalled', { plugin: installed.title });
				return t('hwcheck.hint.plugin', {
					plugins: (device.plugins ?? []).map((plugin) => plugin.title).join(', '),
				});
			}
			case 'kernel':
				return t('hwcheck.hint.kernel', {
					version: device.kernel ?? '',
					driver: device.driver ?? '',
				});
			case 'driver_failed': {
				const parts = [t('hwcheck.hint.driver_failed', { driver: device.driver ?? '' })];
				if (device.kernel) parts.push(t('hwcheck.hint.driverFixed', { version: device.kernel }));
				if (device.driver === 'vmd') parts.push(t('hwcheck.hint.driverVmd'));
				return parts.join(' ');
			}
			default:
				return t(`hwcheck.hint.${device.hint}`);
		}
	}
	const chipClass = (device: UnboundDevice) =>
		device.hint === 'plugin'
			? 'info'
			: device.hint === 'unsupported' || device.hint === 'driver_failed'
				? 'warn'
				: '';
</script>

{#snippet row(device: UnboundDevice)}
	<li class="device">
		<div class="main">
			<span class="name">{device.name}</span>
			<span class="muted mono"
				>{device.bus.toUpperCase()} · {device.address} · {device.id}{device.class
					? ` · ${device.class}`
					: ''}</span
			>
		</div>
		<div class="hint">
			<span class="chip {chipClass(device)}"
				>{t(`hwcheck.hintChip.${device.hint}`, { version: device.kernel ?? '' })}</span
			>
			<span>{hintText(device)}</span>
			{#if device.hint === 'plugin'}<a href="/plugins">{t('hwcheck.openPlugins')}</a>{/if}
			{#if device.messages?.length}
				<ul class="messages">
					{#each device.messages as message, index (index)}<li class="mono muted">
							{message}
						</li>{/each}
				</ul>
			{/if}
		</div>
	</li>
{/snippet}

<p class="muted intro">{t('hwcheck.unboundIntro')}</p>
{#if unbound.length === 0}
	<p class="notice-ok">{t('hwcheck.unboundNone')}</p>
{:else}
	<ul class="list">
		{#each important as device (device.bus + device.address)}{@render row(device)}{/each}
	</ul>
	{#if quiet.length > 0}
		<details>
			<summary>{t('hwcheck.unboundHidden', { count: quiet.length })}</summary>
			<ul class="list">
				{#each quiet as device (device.bus + device.address)}{@render row(device)}{/each}
			</ul>
		</details>
	{/if}
{/if}

<h3 class="sub">{t('hwcheck.firmware')}</h3>
{#if firmware.length === 0}
	<p class="muted">{t('hwcheck.firmwareNone')}</p>
{:else}
	<p class="muted intro">{t('hwcheck.firmwareIntro')}</p>
	<ul class="list">
		{#each firmware as entry (entry.file)}
			<li class="device">
				<div class="main">
					<span class="name mono">{entry.file}</span>
					{#if entry.device}<span class="muted mono">{entry.device}</span>{/if}
				</div>
			</li>
		{/each}
	</ul>
{/if}

<style>
	.intro {
		margin: 0;
		font-size: var(--text-md);
		max-width: 60rem;
	}
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.device {
		display: flex;
		flex-wrap: wrap;
		gap: 8px 20px;
		padding: 12px 0;
		border-top: 1px solid var(--panel-border);
		font-size: var(--text-md);
	}
	.main {
		flex: 1 1 280px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 3px;
	}
	.mono {
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.name {
		font-weight: 600;
	}
	.hint {
		flex: 2 1 320px;
		min-width: 0;
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 6px 10px;
	}
	.messages {
		list-style: none;
		margin: 0;
		padding: 0;
		flex-basis: 100%;
	}
	.sub {
		margin: 6px 0 0;
		font-size: var(--text-md);
	}
	summary {
		cursor: pointer;
		font-size: var(--text-md);
		padding: 6px 0;
	}
	p {
		margin: 0;
	}
</style>
