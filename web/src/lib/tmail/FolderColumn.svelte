<script lang="ts">
	// The first column (on the phone the first level): every mailbox of the
	// account with its state and its folders. A mailbox whose login failed
	// offers "Passwort ändern" right there.
	import Icon, { type IconName } from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import StatusBadge from './StatusBadge.svelte';
	import { folderName, mail, sortFolders } from './store.svelte.ts';
	import type { FolderRole, MailFolder } from './types.ts';

	let {
		account = '',
		folder = '',
		onpick,
	}: {
		account?: string;
		folder?: string;
		onpick: (account: string, folder: string) => void;
	} = $props();

	const ICONS: Record<string, IconName> = {
		inbox: 'mail',
		drafts: 'edit',
		sent: 'send',
		archive: 'archive',
		junk: 'shield',
		trash: 'trash',
	};

	const roleIcon = (role: FolderRole): IconName => ICONS[role] ?? 'folder';
	/** Indent by the folder's depth (IMAP names it with "/" or "."). */
	const depth = (f: MailFolder) => (f.role ? 0 : Math.min(3, f.path.split(/[/.]/).length - 1));
</script>

<nav class="folders" aria-label={t('mail.folders')}>
	{#each mail.accounts ?? [] as a (a.id)}
		<section class="box" aria-labelledby="mail-acc-{a.id}">
			<div class="head">
				<h2 id="mail-acc-{a.id}">{a.name || a.email}</h2>
				{#if a.name && a.name !== a.email}<span class="addr">{a.email}</span>{/if}
				<StatusBadge status={a.status} />
				{#if a.status.state === 'auth_failed'}
					<a class="btn fix" href="/mail/settings?edit={encodeURIComponent(a.id)}"
						>{t('mail.changePassword')}</a
					>
				{/if}
			</div>
			{#if mail.folderErrors[a.id]}
				<p class="notice-error small" role="alert">{mail.folderErrors[a.id]}</p>
			{/if}
			{#if !mail.folders[a.id]}
				<p class="muted small" role="status">{t('common.loading')}</p>
			{:else}
				<ul>
					{#each sortFolders(mail.folders[a.id] ?? []) as f (f.path)}
						{@const current = a.id === account && f.path === folder}
						<li>
							<button
								type="button"
								class="folder"
								class:current
								style:padding-left="calc(var(--space-2-5) + {depth(f)} * 14px)"
								aria-current={current ? 'page' : undefined}
								onclick={() => onpick(a.id, f.path)}
							>
								<Icon name={roleIcon(f.role)} size={16} />
								<span class="name">{folderName(f)}</span>
								{#if f.unread > 0}
									<span class="count"
										><span aria-hidden="true">{f.unread}</span><span class="sr-only"
											>{t('mail.unreadCount', { count: f.unread })}</span
										></span
									>
								{/if}
							</button>
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	{/each}
</nav>

<style>
	.folders {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		min-width: 0;
	}
	.box {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		min-width: 0;
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1) var(--space-2);
		padding: 0 var(--space-2-5);
		min-width: 0;
	}
	h2 {
		margin: 0;
		flex: 1 1 100%;
		font-size: var(--text-md);
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.addr {
		flex: 1 1 100%;
		color: var(--muted);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.fix {
		min-height: 32px;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.folder {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		width: 100%;
		min-height: 36px;
		padding: var(--space-1-5) var(--space-2-5);
		border: 0;
		border-radius: var(--radius-small);
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		text-align: left;
		cursor: pointer;
	}
	.folder:hover {
		background: var(--chip);
	}
	.folder.current {
		background: color-mix(in srgb, var(--accent) 16%, transparent);
		font-weight: 600;
	}
	.folder :global(svg) {
		flex: none;
		color: var(--muted);
	}
	.name {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.count {
		flex: none;
		min-width: 1.6em;
		padding: 0 var(--space-1-5);
		border-radius: 999px;
		background: var(--accent);
		color: var(--on-accent);
		font-size: var(--text-sm);
		font-weight: 600;
		text-align: center;
	}
	.small {
		margin: 0 var(--space-2-5);
		font-size: var(--text-sm);
	}
	@media (max-width: 899px) {
		.folder {
			min-height: 48px;
		}
		.fix {
			min-height: 44px;
		}
	}
</style>
