<script lang="ts">
	// The servers of a mailbox: user name, IMAP and SMTP with host, port and
	// TLS kind. Only TLS: "tls" (implicit) or "starttls" (contract).
	import { t } from '../i18n/index.svelte.ts';
	import type { Security, Server } from './types.ts';

	let {
		id,
		user = $bindable(''),
		imap = $bindable(),
		smtp = $bindable(),
		onchange,
	}: {
		id: string;
		user: string;
		imap: Server;
		smtp: Server;
		onchange?: () => void;
	} = $props();

	const DEFAULT_PORT: Record<'imap' | 'smtp', Record<Security, number>> = {
		imap: { tls: 993, starttls: 143 },
		smtp: { tls: 465, starttls: 587 },
	};
	/** Changing the TLS kind moves a port that was the old default to the new one. */
	function security(kind: 'imap' | 'smtp', value: Security) {
		const s = kind === 'imap' ? imap : smtp;
		const old = DEFAULT_PORT[kind][s.security];
		const next = {
			...s,
			security: value,
			port: s.port === old ? DEFAULT_PORT[kind][value] : s.port,
		};
		if (kind === 'imap') imap = next;
		else smtp = next;
		onchange?.();
	}
</script>

<div class="servers">
	<label class="wide"
		><span>{t('mail.setup.user')}</span>
		<input
			id="{id}-user"
			class="field"
			type="text"
			autocomplete="off"
			spellcheck="false"
			bind:value={user}
			oninput={onchange}
		/>
	</label>
	{#each [['imap', 'mail.setup.imap'], ['smtp', 'mail.setup.smtp']] as const as [kind, head] (kind)}
		{@const s = kind === 'imap' ? imap : smtp}
		<fieldset>
			<legend>{t(head)}</legend>
			<label class="host"
				><span>{t('mail.setup.host')}</span>
				<input
					id="{id}-{kind}-host"
					class="field"
					type="text"
					autocomplete="off"
					spellcheck="false"
					value={s.host}
					oninput={(e) => {
						const v = e.currentTarget.value.trim();
						if (kind === 'imap') imap = { ...imap, host: v };
						else smtp = { ...smtp, host: v };
						onchange?.();
					}}
				/>
			</label>
			<label class="port"
				><span>{t('mail.setup.port')}</span>
				<input
					id="{id}-{kind}-port"
					class="field"
					type="number"
					min="1"
					max="65535"
					value={s.port}
					oninput={(e) => {
						const v = Number(e.currentTarget.value) || 0;
						if (kind === 'imap') imap = { ...imap, port: v };
						else smtp = { ...smtp, port: v };
						onchange?.();
					}}
				/>
			</label>
			<label class="sec"
				><span>{t('mail.setup.security')}</span>
				<select
					id="{id}-{kind}-security"
					class="field"
					value={s.security}
					onchange={(e) => security(kind, e.currentTarget.value as Security)}
				>
					<option value="tls">{t('mail.setup.tls')}</option>
					<option value="starttls">{t('mail.setup.starttls')}</option>
				</select>
			</label>
		</fieldset>
	{/each}
	<p class="muted hint">{t('mail.setup.tlsOnly')}</p>
</div>

<style>
	.servers {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		min-width: 0;
	}
	label span,
	legend {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	legend {
		padding: 0 var(--space-1);
		font-weight: 600;
	}
	fieldset {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 6.5rem auto;
		gap: var(--space-2);
		margin: 0;
		padding: var(--space-2) var(--space-3) var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		min-width: 0;
	}
	.field {
		width: 100%;
	}
	.sec select {
		width: auto;
	}
	.hint {
		margin: 0;
		font-size: var(--text-sm);
	}
	@media (max-width: 600px) {
		fieldset {
			grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
		}
		.host {
			grid-column: 1 / -1;
		}
		.field {
			min-height: 44px;
		}
	}
</style>
