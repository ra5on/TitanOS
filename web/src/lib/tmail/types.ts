// 1.05: the shapes of the T-Mail API (docs/TMAIL.md, "API"). Every field
// here is named in the contract; where the contract left a shape open, the
// expectation is written down there under "Offen für das Backend".

export type Security = 'tls' | 'starttls';

export type Server = { host: string; port: number; security: Security };

export type MailState = 'ok' | 'connecting' | 'auth_failed' | 'unreachable' | 'off';

export type MailStatus = { state: MailState; code?: string; since?: string };

export type MailAccount = {
	id: string;
	name: string;
	email: string;
	user: string;
	imap: Server;
	smtp: Server;
	passwordSet: boolean;
	notify: boolean;
	/** The mailbox's default signature (id), '' for none. */
	signature: string;
	status: MailStatus;
	unread: number;
};

/** What POST /accounts and PUT /accounts/{id} take: the fields above and the password. */
export type AccountInput = Omit<MailAccount, 'id' | 'passwordSet' | 'status' | 'unread'> & {
	/** Empty on PUT keeps the stored password. */
	password: string;
};

export type FolderRole = 'inbox' | 'sent' | 'drafts' | 'trash' | 'junk' | 'archive' | '';

export type MailFolder = {
	path: string;
	name: string;
	role: FolderRole;
	unread: number;
	total: number;
};

export type Address = { name: string; addr: string };

export type Flags = { seen: boolean; flagged: boolean; answered: boolean };

export type MessageRow = {
	uid: number;
	from: Address;
	to: Address[];
	subject: string;
	date: string;
	flags: Flags;
	size: number;
	attachments: boolean;
};

export type MessagePage = { messages: MessageRow[]; next: number | null; total: number };

export type Attachment = {
	part: string;
	name: string;
	type: string;
	size: number;
	inline: boolean;
	cid: string;
};

export type Message = {
	uid: number;
	folder: string;
	from: Address;
	to: Address[];
	cc: Address[];
	replyTo: Address[];
	subject: string;
	date: string;
	messageId: string;
	inReplyTo: string;
	references: string[];
	flags: Flags;
	text: string;
	html: string;
	externalImages: boolean;
	attachments: Attachment[];
	truncated: boolean;
};

export type TestResult = {
	imap: { ok: boolean; code?: string };
	smtp: { ok: boolean; code?: string };
};

export type Provider = {
	imap: Server;
	smtp: Server;
	/** The provider wants an app password (Gmail, iCloud …). */
	appPassword: boolean;
};

export type Contact = { name: string; addr: string; count: number; last: string };

export type Signature = { id: string; name: string; text: string };

export type Rule = {
	id: string;
	account: string;
	enabled: boolean;
	when: { from: string; to: string; subject: string };
	then: { moveTo: string; markRead: boolean; flag: boolean };
};

export type MailSettings = { signatures: Signature[]; rules: Rule[] };

/** The `meta` part of POST /send and POST /draft. */
export type SendMeta = {
	to: string[];
	cc: string[];
	bcc: string[];
	subject: string;
	text: string;
	inReplyTo: string;
	references: string[];
	signature: string;
	draftUid: number;
	/**
	 * The message answered or passed on (open for the backend in docs/TMAIL.md):
	 * a reply marks it "answered", a forward takes its attachments along.
	 */
	source?: { folder: string; uid: number; mode: 'reply' | 'forward' };
};

/** How a new e-mail starts from an open one. */
export type ComposeMode = 'reply' | 'replyAll' | 'forward';
