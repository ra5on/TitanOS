// Disk tests that a restart stopped: what the tools page offers for them.

export type DiskTestState = {
	disk: string;
	mode: 'read' | 'write';
	result: string;
	autoResume?: boolean;
	persistent?: boolean;
	notPersistent?: string;
	resumes?: number;
};

/** How a waiting test is continued: an overwriting one asks first (and the
 * page sends the disk name as confirmation), a reading one just goes on. */
export function resumeKind(test: DiskTestState): 'confirm' | 'direct' | null {
	if (test.result !== 'interrupted') return null;
	return test.mode === 'write' ? 'confirm' : 'direct';
}

/** The body of POST /storage/tests/{disk}/resume. */
export function resumeBody(test: DiskTestState): { confirm: string } {
	return { confirm: test.mode === 'write' ? test.disk : '' };
}

/** Why a running test would end with a restart; null if it can be continued. */
export function restartHint(
	test: DiskTestState,
): 'kept' | 'no_serial' | 'no_volume' | 'write_failed' | null {
	if (test.result) return null;
	if (test.persistent) return 'kept';
	if (
		test.notPersistent === 'no_serial' ||
		test.notPersistent === 'no_volume' ||
		test.notPersistent === 'write_failed'
	)
		return test.notPersistent;
	return null;
}
