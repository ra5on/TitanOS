<script lang="ts">
	// 1.11: Kontakte (docs/KALENDER.md). Step 11 builds the frame and lists
	// the address books (GET /contacts/books); the list of contacts and the
	// card follow in step 16 in place of the body.
	import { api } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import ProgramShell from '#lib/pim/ProgramShell.svelte';
	import { sanitizeBooks, shellOfError, type Book, type ShellState } from '#lib/pim/shell.ts';

	let shown = $state<ShellState>({ kind: 'loading' });
	let books = $state<Book[]>([]);
	$effect(() => {
		api
			.get<unknown>('/contacts/books')
			.then((raw) => {
				const list = sanitizeBooks(raw);
				if (list === null) {
					shown = shellOfError(new Error('books'));
					return;
				}
				books = list;
				shown = { kind: 'ready' };
			})
			.catch((err) => (shown = shellOfError(err)));
	});
</script>

<PageHead title={t('desktop.app.contacts')} />

<ProgramShell program="contacts" view={shown}>
	<div class="ready" data-pim-ready>
		<ul class="books">
			{#each books as book (book.id)}
				<li>
					<Icon name="book" size={17} /><span class="name">{book.name}</span><span class="muted"
						>{t('contacts.shell.count', { count: book.count })}</span
					>
				</li>
			{/each}
		</ul>
		<p class="text">{t('contacts.shell.text')}</p>
		<a class="btn" href="/contacts/iphone"
			><Icon name="device" size={16} />{t('calendar.side.iphone')}</a
		>
	</div>
</ProgramShell>

<style>
	.ready {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-3);
	}
	.ready p {
		margin: 0;
	}
	.books {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
	}
	.books li {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.books :global(svg) {
		color: var(--muted);
	}
	.name {
		font-weight: 600;
		overflow-wrap: anywhere;
	}
</style>
