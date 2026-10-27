// Status filter and lock rules of the users page. The server checks the same
// rules again; this only decides what the page offers.
export type AccountStatusFilter = 'all' | 'active' | 'locked';
export const ACCOUNT_STATUS_FILTERS: AccountStatusFilter[] = ['all', 'active', 'locked'];

type Lockable = { name: string; role: 'owner' | 'admin' | 'user'; disabled?: boolean };

export function sanitizeStatusFilter(value: unknown): AccountStatusFilter {
	return ACCOUNT_STATUS_FILTERS.includes(value as AccountStatusFilter)
		? (value as AccountStatusFilter)
		: 'all';
}

export function filterAccounts<T extends Lockable>(
	accounts: T[],
	filter: AccountStatusFilter,
): T[] {
	if (filter === 'all') return accounts;
	return accounts.filter((account) => Boolean(account.disabled) === (filter === 'locked'));
}

/**
 * Whether me may lock or unlock account: never oneself, owners only by
 * owners, and the last owner that is not locked stays unlocked.
 */
export function mayLock<T extends Lockable>(
	me: { name: string; role: string } | null | undefined,
	account: T,
	accounts: T[],
): boolean {
	if (!me || account.name === me.name) return false;
	if (me.role !== 'owner' && (me.role !== 'admin' || account.role === 'owner')) return false;
	if (account.disabled || account.role !== 'owner') return true;
	return accounts.filter((entry) => entry.role === 'owner' && !entry.disabled).length > 1;
}
