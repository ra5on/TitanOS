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
	/** The name in "From" ('' = the address alone); `name` only labels the mailbox in T-Mail. */
	senderName: string;
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
	/** 1.12: what separates the levels of `path` ('' or missing: unknown). */
	delim?: string;
};

export type Address = { name: string; addr: string };

/** `forwarded` (1.12, $Forwarded) only shows; older NAS leave it out. */
export type Flags = { seen: boolean; flagged: boolean; answered: boolean; forwarded?: boolean };

export type MessageRow = {
	uid: number;
	from: Address;
	to: Address[];
	subject: string;
	date: string;
	flags: Flags;
	size: number;
	attachments: boolean;
	/** The head is too large to list: only the uid is sure, the other fields may be missing. */
	tooLarge?: boolean;
	/** 1.12: the beginning of the text, at most 140 characters cut at a word by titand (plain text). */
	preview?: string;
	/** 1.12: the heads of the conversation (Message-IDs; `references` separated by spaces). */
	messageId?: string;
	inReplyTo?: string;
	references?: string;
};

/** 1.12: a hit of the search over all folders. */
export type SearchHit = MessageRow & { folder: string };

/** `truncated`: not every folder was searched (more than 100, out of time, a folder refused). */
export type SearchResult = {
	messages: SearchHit[];
	total: number;
	folders: number;
	truncated: boolean;
};

/** 1.12: the contacts with a picture by lower-case address (GET /mail/avatars). */
export type Avatars = { book: string; photos: Record<string, string> };

/** `next`: the uid for `before` of the next page, 0 (or null) at the end. */
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
	/** Only known for one's own mail (drafts). */
	bcc?: Address[];
	replyTo: Address[];
	subject: string;
	date: string;
	messageId: string;
	inReplyTo: string;
	/** Message-IDs separated by spaces. */
	references: string;
	flags: Flags;
	text: string;
	html: string;
	externalImages: boolean;
	attachments: Attachment[];
	truncated: boolean;
	/** The structure was too large to read (a digest): text is the
	 *  beginning of the body as it came, without HTML or attachments. */
	rawText?: boolean;
};

export type TestResult = {
	imap: { ok: boolean; code?: string };
	smtp: { ok: boolean; code?: string };
};

/** GET /mail/providers?email= (flat; `known: false` with empty fields). */
export type Provider = {
	known: boolean;
	id: string;
	name: string;
	domains: string[];
	imap: Server;
	smtp: Server;
	/** The provider wants an app password (Gmail, iCloud …). */
	appPassword: boolean;
	/** false: the provider takes no password any more (only OAuth, not in this version). */
	supported: boolean;
	/** Key of the hint text: imap_enable, app_password, app_password_2fa, mail_password, oauth_only. */
	hint: string;
};

/** `source` (1.11): from the account's contacts or learned from sent mail; older NAS leave it out. */
export type Contact = {
	name: string;
	addr: string;
	count: number;
	last: string;
	source?: 'contact' | 'learned';
};

/** `default`: the mailboxes that propose this signature (the same as accounts[].signature). */
export type Signature = { id: string; name: string; text: string; default?: string[] };

export type Rule = {
	id: string;
	account: string;
	enabled: boolean;
	when: { from: string; to: string; subject: string };
	then: { moveTo: string; markRead: boolean; flag: boolean };
};

/** `pushContent`: push messages name sender and subject (from the start on). */
export type MailSettings = { signatures: Signature[]; rules: Rule[]; pushContent?: boolean };

/** The `meta` part of POST /send and POST /draft. */
export type SendMeta = {
	to: string[];
	cc: string[];
	bcc: string[];
	subject: string;
	text: string;
	inReplyTo: string;
	references: string;
	signature: string;
	draftUid: number;
	/**
	 * Parts of an existing mail taken along (forwarding, a draft opened
	 * again). Without `uid` (and folder): the draft this save replaces
	 * (`draftUid`), so uploaded files need not come again.
	 */
	attachFrom?: { folder?: string; uid?: number; parts: string[] };
	/** The mail answered: it is marked "answered" once this one is sent. */
	answers?: { folder: string; uid: number };
	/** 1.12: the mail forwarded: it is marked $Forwarded once this one is sent. */
	forwards?: { folder: string; uid: number };
};

/** A file of the NAS for nasFiles (roots of GET /files/roots). */
export type NasFile = { root: string; path: string };

/** How a new e-mail starts from an open one. */
export type ComposeMode = 'reply' | 'replyAll' | 'forward';

/** The answer of POST …/draft: the new draft and its attachments. */
export type DraftSaved = { uid: number; attachments?: Attachment[] };
