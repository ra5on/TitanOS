<script lang="ts">
	// The state of a mailbox in words with a symbol – never by colour alone.
	import Icon, { type IconName } from '../components/Icon.svelte';
	import { i18n, t, type MessageKey } from '../i18n/index.svelte.ts';
	import type { MailStatus } from './types.ts';

	let { status, showOk = false }: { status: MailStatus; showOk?: boolean } = $props();

	const LOOK: Record<MailStatus['state'], { icon: IconName; key: MessageKey; tone: string }> = {
		ok: { icon: 'check', key: 'mail.state.ok', tone: 'ok' },
		connecting: { icon: 'refresh', key: 'mail.state.connecting', tone: 'busy' },
		auth_failed: { icon: 'lock', key: 'mail.state.auth_failed', tone: 'error' },
		unreachable: { icon: 'globe', key: 'mail.state.unreachable', tone: 'warn' },
		off: { icon: 'pause', key: 'mail.state.off', tone: 'off' },
	};
	const look = $derived(LOOK[status.state] ?? LOOK.off);
	const since = $derived.by(() => {
		if (!status.since || status.state === 'ok' || status.state === 'connecting') return '';
		const d = new Date(status.since);
		if (Number.isNaN(d.getTime())) return '';
		return new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'short', timeStyle: 'short' }).format(
			d,
		);
	});
</script>

<!-- "off" is no fault: no lasting connection (notices and rules off); lists still work. -->
{#if showOk || (status.state !== 'ok' && status.state !== 'off')}
	<span class="state {look.tone}" data-state={status.state}>
		<Icon name={look.icon} size={14} />
		<span
			>{t(look.key)}{#if since}<span class="since">
					· {t('mail.state.since', { time: since })}</span
				>{/if}</span
		>
	</span>
{/if}

<style>
	.state {
		display: inline-flex;
		align-items: flex-start;
		gap: var(--space-1);
		padding: 1px var(--space-1-5);
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		font-size: var(--text-sm);
		line-height: 1.4;
		overflow-wrap: break-word;
		min-width: 0;
	}
	.state :global(svg) {
		flex: none;
		margin-top: 2px;
	}
	.ok {
		color: var(--ok-text);
		border-color: var(--ok-border);
		background: var(--ok-bg);
	}
	.warn {
		color: var(--warn-text);
		border-color: var(--warn-border);
		background: var(--warn-bg);
	}
	.error {
		color: var(--error-text);
		border-color: var(--error-border);
		background: var(--error-bg);
	}
	.busy,
	.off {
		color: var(--muted);
	}
	.since {
		font-weight: 400;
	}
</style>
