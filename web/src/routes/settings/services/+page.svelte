<script lang="ts">
	// Settings › Services (decided 31.10.2026): Docker, VMs and system
	// containers can be switched off; the service then does not run and its
	// tab is hidden. Data always stays.
	import PageHead from '#lib/components/PageHead.svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { loadServices } from '#lib/session.svelte.ts';

	type Service = { enabled: boolean; running: number; total: number; available: boolean };
	type Name = 'docker' | 'vms' | 'lxc';
	const NAMES: Name[] = ['docker', 'vms', 'lxc'];

	let services = $state<Partial<Record<Name, Service>>>({});
	let error = $state('');
	let busy = $state<Name | ''>('');
	// Switching off with something running asks first.
	let asking = $state<Name | ''>('');

	async function load() {
		try {
			services = await api.get<Partial<Record<Name, Service>>>('/services');
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	$effect(() => {
		void load();
	});

	async function set(name: Name, enabled: boolean) {
		busy = name;
		error = '';
		asking = '';
		try {
			services = await api.put<Partial<Record<Name, Service>>>(`/services/${name}`, { enabled });
			await loadServices();
		} catch (e) {
			error = apiErrorText(e);
			await load();
		} finally {
			busy = '';
		}
	}
	function toggle(name: Name, enabled: boolean) {
		const service = services[name];
		if (!enabled && service && service.running > 0) {
			asking = name;
			return;
		}
		void set(name, enabled);
	}
</script>

<PageHead title={t('services.title')}>
	<p class="muted">{t('services.intro')}</p>
</PageHead>

{#if error}<div class="notice-error" role="alert">{error}</div>{/if}

<section class="tile panel">
	{#each NAMES as name (name)}
		{@const service = services[name]}
		{#if service}
			<div class="service">
				<div>
					<strong>{t(`services.${name}` as MessageKey)}</strong>
					<span class="chip {service.enabled ? 'ok' : ''}"
						>{service.enabled ? '● ' + t('services.on') : '○ ' + t('services.off')}</span
					>
					<p class="muted small">{t(`services.${name}Desc` as MessageKey)}</p>
					<p class="small">
						{#if !service.available}{t('services.unavailable')}{:else if service.enabled}{t(
								'services.counts',
								{ running: service.running, total: service.total },
							)}{:else}{t('services.offHint')}{/if}
					</p>
				</div>
				<label class="check"
					><input
						type="checkbox"
						checked={service.enabled}
						disabled={busy !== '' || (!service.available && !service.enabled)}
						onchange={(e) => {
							const on = e.currentTarget.checked;
							e.currentTarget.checked = service.enabled;
							toggle(name, on);
						}}
					/><span>{t(busy === name ? 'services.working' : 'services.enabled')}</span></label
				>
			</div>
			{#if asking === name}
				<div class="notice-warn" role="alert">
					<p>
						{t('services.confirm', {
							name: t(`services.${name}` as MessageKey),
							count: service.running,
						})}
					</p>
					<div class="row-actions">
						<button type="button" class="btn btn-danger" onclick={() => set(name, false)}
							>{t('services.switchOff', { name: t(`services.${name}` as MessageKey) })}</button
						>
						<button type="button" class="btn" onclick={() => (asking = '')}
							>{t('common.cancel')}</button
						>
					</div>
				</div>
			{/if}
		{/if}
	{/each}
</section>

<style>
	.service {
		display: grid;
		grid-template-columns: 1fr auto;
		gap: 6px 16px;
		align-items: center;
		padding: 14px 0;
		border-top: 1px solid var(--border);
	}
	.service:first-child {
		border-top: 0;
		padding-top: 0;
	}
	.service p {
		margin: 4px 0 0;
	}
	.service .check {
		justify-self: end;
	}
	.notice-warn p {
		margin: 0 0 8px;
	}
</style>
