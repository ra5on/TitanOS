// Colors for the code blocks in the answers of the KI-Agent: comments,
// strings, numbers, keywords, the keys of YAML and JSON and the variables
// of the shell. A small tokenizer of its own, no library: every piece of
// the code is escaped here, the only markup that comes out are <span>s
// with a fixed class. A language it does not know stays plain text.

function escape(text: string): string {
	return text.replace(
		/[&<>"']/g,
		(c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!,
	);
}

type Family = {
	/** "# …" is a comment. */
	hash?: boolean;
	/** "// …" and "/* … *​/" are comments. */
	slash?: boolean;
	/** "-- …" is a comment. */
	dash?: boolean;
	/** "key:" at the start of a line is a key. */
	keys?: boolean;
	/** "$NAME" is a variable. */
	vars?: boolean;
	/** Keywords are matched without regard to case. */
	nocase?: boolean;
	words: Set<string>;
};

const words = (list: string) => new Set(list.split(' '));
const SHELL: Family = {
	hash: true,
	vars: true,
	words: words(
		'if then else elif fi for while until do done case esac in function return exit export local ' +
			'sudo cd echo set unset source docker systemctl apt npm git curl wget ssh cat grep sed awk ' +
			'mkdir rm cp mv chmod chown tar',
	),
};
const SCRIPT: Family = {
	slash: true,
	words: words(
		'const let var function return if else for while do switch case break continue new class ' +
			'extends import from export default async await try catch finally throw typeof instanceof ' +
			'in of this interface type enum implements public private readonly static void',
	),
};
const FAMILIES: Record<string, Family> = {
	yaml: { hash: true, keys: true, words: new Set() },
	json: { words: new Set() },
	shell: SHELL,
	script: SCRIPT,
	python: {
		hash: true,
		words: words(
			'def class return if elif else for while in not and or is import from as with try except ' +
				'finally raise pass break continue lambda yield async await global nonlocal assert del self',
		),
	},
	go: {
		slash: true,
		words: words(
			'func package import return if else for range switch case default break continue go defer ' +
				'var const type struct interface map chan select fallthrough goto',
		),
	},
	conf: { hash: true, keys: true, words: new Set() },
	docker: {
		hash: true,
		vars: true,
		words: words(
			'FROM RUN CMD COPY ADD ENV ARG EXPOSE ENTRYPOINT WORKDIR USER VOLUME LABEL HEALTHCHECK AS',
		),
	},
	sql: {
		dash: true,
		nocase: true,
		words: words(
			'select from where and or not insert into values update set delete create table alter drop ' +
				'join left right inner outer on group by order having limit as distinct union index ' +
				'primary key null default',
		),
	},
};
const NAMES: Record<string, string> = {
	yaml: 'yaml',
	yml: 'yaml',
	json: 'json',
	bash: 'shell',
	sh: 'shell',
	shell: 'shell',
	zsh: 'shell',
	console: 'shell',
	js: 'script',
	javascript: 'script',
	ts: 'script',
	typescript: 'script',
	jsx: 'script',
	tsx: 'script',
	java: 'script',
	c: 'script',
	cpp: 'script',
	rust: 'script',
	py: 'python',
	python: 'python',
	go: 'go',
	golang: 'go',
	ini: 'conf',
	toml: 'conf',
	conf: 'conf',
	env: 'conf',
	dockerfile: 'docker',
	docker: 'docker',
	sql: 'sql',
};
const CONSTANT = new Set(['true', 'false', 'null', 'none', 'nil', 'yes', 'no', 'on', 'off']);

const TOKEN =
	/(\/\*[\s\S]*?\*\/)|(\/\/[^\n]*)|(#[^\n]*)|(--[^\n]*)|("(?:\\.|[^"\\\n])*"|'(?:\\.|[^'\\\n])*'|`(?:\\.|[^`\\])*`)|(\$\{?[A-Za-z_]\w*\}?)|(\b\d+(?:\.\d+)?\b)|([A-Za-z_][\w.-]*)/g;

/** The code as HTML with colored pieces; plain (escaped) for an unknown language. */
export function highlight(code: string, lang: string): string {
	const family = FAMILIES[NAMES[lang.toLowerCase()] ?? ''];
	if (!family) return escape(code);
	const span = (cls: string, text: string) => `<span class="t-${cls}">${escape(text)}</span>`;
	let out = '';
	let at = 0;
	TOKEN.lastIndex = 0;
	for (let m = TOKEN.exec(code); m; m = TOKEN.exec(code)) {
		const text = m[0];
		const before = code[m.index - 1];
		const lineStart = code.lastIndexOf('\n', m.index - 1) + 1;
		const lead = code.slice(lineStart, m.index);
		const after = code.slice(m.index + text.length);
		let piece = '';
		if (m[1] || m[2]) piece = family.slash ? span('c', text) : '';
		else if (m[3])
			piece = family.hash && (before === undefined || /\s/.test(before)) ? span('c', text) : '';
		else if (m[4]) piece = family.dash ? span('c', text) : '';
		else if (m[5]) piece = span(/^\s*:/.test(after) && !family.keys ? 'a' : 's', text);
		else if (m[6]) piece = family.vars ? span('v', text) : '';
		else if (m[7]) piece = span('n', text);
		else if (m[8]) {
			const word = family.nocase ? text.toLowerCase() : text;
			if (family.keys && /^\s*(?:-\s+)?$/.test(lead) && /^\s*[:=]/.test(after))
				piece = span('a', text);
			else if (family.words.has(word)) piece = span('k', text);
			else if (CONSTANT.has(text.toLowerCase())) piece = span('n', text);
			else piece = escape(text);
		}
		out += escape(code.slice(at, m.index));
		if (piece) {
			out += piece;
			at = m.index + text.length;
		} else {
			// Not a token in this language (a "#" in JSON, "//" in a URL of a
			// YAML file): the sign stays plain and the search goes on behind it.
			out += escape(text[0]);
			at = m.index + 1;
			TOKEN.lastIndex = at;
		}
	}
	return out + escape(code.slice(at));
}
