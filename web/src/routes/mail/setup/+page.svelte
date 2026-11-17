<script lang="ts">
	// 1.05, T-Mail: setting up a mailbox (lib/tmail/SetupWizard.svelte).
	import { goto } from '$app/navigation';
	import PageHead from '#lib/components/PageHead.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { mailHref } from '#lib/tmail/levels.ts';
	import SetupWizard from '#lib/tmail/SetupWizard.svelte';
	import { loadAccounts, loadFolders } from '#lib/tmail/store.svelte.ts';
	import type { MailAccount } from '#lib/tmail/types.ts';

	async function saved(account: MailAccount) {
		await loadAccounts();
		void loadFolders(account.id);
		void goto(mailHref({ a: account.id, f: 'INBOX' }), { replace: true });
	}
</script>

<PageHead title={t('mail.setup.title')} path={[{ label: t('desktop.app.mail'), href: '/mail' }]} />

<p class="muted intro">{t('mail.setup.intro')}</p>
<SetupWizard onsaved={saved} />

<style>
	.intro {
		margin: 0 0 var(--space-3);
		max-width: 40rem;
	}
</style>
