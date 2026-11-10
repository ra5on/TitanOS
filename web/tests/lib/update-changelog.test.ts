import { describe, expect, it } from 'vitest';
import {
	counts,
	firstItems,
	inline,
	leadOf,
	parseChangelog,
	projectLink,
	releaseUrl,
} from '#lib/update/changelog.ts';

// Shortened from the real tag messages (git tag -l -n200 v0.75.0, v0.74.0,
// v0.61.0, v0.50.0) – the structure is theirs.
const v075 = `# TitanOS 0.75 — Durchsicht des ganzen Codes: rund 150 Korrekturen, kein abgeschnittener Text mehr

Ein **volles Systemupdate** (neues Abbild in den zweiten Slot, Neustart, drei Probestarts mit Rückfall). Tag und Manifest heißen \`v0.75.0\`, angezeigt wird 0.75.

Dieses Release bringt keine neue große Funktion.

## Neu

**Kein abgeschnittener Text mehr**
- Nirgends steht mehr „…“ am Ende einer Beschriftung.
- Die Seitenleiste eines Fensters klappt in einem schmalen Fenster von selbst zu ihren Symbolen ein.

**Apps**
- Zugangsdaten stehen verdeckt im Formular und in \`.env\` statt im Klartext in der Compose-Datei.

## Behoben

**Sicherheit und Zugang**
- Der Cluster-Proxy reichte WebSocket-Verbindungen ohne Herkunftsprüfung weiter.
- API-Tokens konnten über die Dateirouten verborgene Pfade lesen.

**Speicher**
- Statusabfragen weckten vermutlich schlafende Platten (\`mdadm --detail\`).

## Bekannt und offen

- Die Fundliste nennt, was offen bleibt.

## Rückweg

Einstellungen › Updates › „Auf TitanOS 0.74 zurück“.
`;

const v074 = `# TitanOS 0.74 — neues Design auf allen Seiten

Ein **App-Update**: Es ersetzt titand mit der Weboberfläche.

## Neu

**Design auf allen Seiten**
- Gruppen mit Titel lassen sich jetzt auf jeder Seite auf- und zuklappen.
- **Statusleiste am unteren Fensterrand:**
  - Dateien, Systemprotokoll, Plugins
  - Docker: Images, Volumes, Netzwerke
- Ein Punkt, der in der nächsten
  Zeile weitergeht.

## Behoben

- **Öffnen und Speichern über die Brücke ging bei den T-Apps nicht.** Sie melden sich unter dem Namen ihres Originals (\`wordcraft:ready\`).
`;

const v061 = `# TitanOS 0.61.0 — Fernansicht, neues Fensterdesign (PLAN 63, 64)

## Neu

### Fernansicht für Docker-Apps

**Fernansicht:** Das Fenster einer Docker-App lädt die App nicht mehr im eigenen Browser.

- Das Abbild der Fernansicht ist eine eigene, signierte Release-Datei.

### Fenster im neuen Design (UGOS/DSM-Stil)

- **Keine doppelte Überschrift:** die Titelleiste zeigt „Programm › Seite“.

## Behoben

**VMs, die nicht herunterfahren, werden ausgeschaltet.** Reagiert eine VM nicht, wird sie ausgeschaltet.

1. Erst die Dienste.
2. Dann das Protokoll.
`;

const text = (parts: { text: string }[]) => parts.map((part) => part.text).join('');

describe('parseChangelog', () => {
	it('reads title, lead and intro of a real release (0.75)', () => {
		const log = parseChangelog(v075);
		expect(log.title).toBe(
			'TitanOS 0.75 — Durchsicht des ganzen Codes: rund 150 Korrekturen, kein abgeschnittener Text mehr',
		);
		expect(log.lead).toBe(
			'Durchsicht des ganzen Codes: rund 150 Korrekturen, kein abgeschnittener Text mehr',
		);
		expect(log.sections.map((s) => [s.kind, s.title, s.count])).toEqual([
			['intro', '', 0],
			['new', 'Neu', 3],
			['fixed', 'Behoben', 3],
			['other', 'Bekannt und offen', 1],
			['other', 'Rückweg', 0],
		]);
		const intro = log.sections[0].groups[0];
		expect(intro.text).toHaveLength(2);
		expect(intro.text[0]).toEqual([
			{ text: 'Ein ' },
			{ text: 'volles Systemupdate', bold: true },
			{
				text: ' (neues Abbild in den zweiten Slot, Neustart, drei Probestarts mit Rückfall). Tag und Manifest heißen ',
			},
			{ text: 'v0.75.0', code: true },
			{ text: ', angezeigt wird 0.75.' },
		]);
		expect(counts(log)).toEqual({ new: 3, security: 0, fixed: 3 });
	});

	it('takes a bold-only line as the sub-heading of the bullets after it', () => {
		const neu = parseChangelog(v075).sections[1];
		expect(neu.groups.map((g) => [g.title, g.items.length])).toEqual([
			['Kein abgeschnittener Text mehr', 2],
			['Apps', 1],
		]);
		expect(neu.groups[1].items[0].parts).toEqual([
			{ text: 'Zugangsdaten stehen verdeckt im Formular und in ' },
			{ text: '.env', code: true },
			{ text: ' statt im Klartext in der Compose-Datei.' },
		]);
		// A paragraph without bullets under "other" headings stays text.
		const back = parseChangelog(v075).sections[4];
		expect(text(back.groups[0].text[0])).toBe(
			'Einstellungen › Updates › „Auf TitanOS 0.74 zurück“.',
		);
	});

	it('keeps indented bullets below their bullet and joins continued lines (0.74)', () => {
		const log = parseChangelog(v074);
		const neu = log.sections.find((s) => s.kind === 'new')!;
		// The indented ones are not counted.
		expect(neu.count).toBe(3);
		const items = neu.groups[0].items;
		expect(items[1].parts).toEqual([{ text: 'Statusleiste am unteren Fensterrand:', bold: true }]);
		expect(items[1].children?.map((c) => text(c.parts))).toEqual([
			'Dateien, Systemprotokoll, Plugins',
			'Docker: Images, Volumes, Netzwerke',
		]);
		expect(text(items[2].parts)).toBe('Ein Punkt, der in der nächsten Zeile weitergeht.');
		const fixed = log.sections.find((s) => s.kind === 'fixed')!;
		expect(fixed.groups[0].title).toBe('');
		expect(fixed.groups[0].items[0].parts[0]).toEqual({
			text: 'Öffnen und Speichern über die Brücke ging bei den T-Apps nicht.',
			bold: true,
		});
		expect(counts(log)).toEqual({ new: 3, security: 0, fixed: 1 });
	});

	it('reads the older form with ### headings, bold leads and numbered points (0.61)', () => {
		const log = parseChangelog(v061);
		expect(log.lead).toBe('Fernansicht, neues Fensterdesign (PLAN 63, 64)');
		const neu = log.sections[0];
		expect(neu.groups.map((g) => g.title)).toEqual([
			'Fernansicht für Docker-Apps',
			'Fenster im neuen Design (UGOS/DSM-Stil)',
		]);
		// "**Fernansicht:** text" is a paragraph with a bold start, not a heading.
		expect(neu.groups[0].text[0][0]).toEqual({ text: 'Fernansicht:', bold: true });
		expect(neu.groups[0].items).toHaveLength(1);
		const fixed = log.sections[1];
		expect(fixed.count).toBe(2);
		expect(text(fixed.groups[0].items[0].parts)).toBe('1. Erst die Dienste.');
	});

	it('knows the three tagged headings in both languages, whatever the case', () => {
		const log = parseChangelog(
			'## NEW\n- a\n## Security:\n- b\n- c\n## fixed\n- d\n## Sicherheit\n- e\n## Neu und geändert\n- f\n',
		);
		expect(log.sections.map((s) => s.kind)).toEqual([
			'new',
			'security',
			'fixed',
			'security',
			'other',
		]);
		expect(counts(log)).toEqual({ new: 1, security: 3, fixed: 1 });
		expect(log.sections[4].title).toBe('Neu und geändert');
	});

	it('makes one plain block of a text without headings', () => {
		const log = parseChangelog(
			'Kleinere Verbesserungen der VM-Seite.\nUnd mehr.\n\nZweiter Absatz.',
		);
		expect(log.title).toBe('');
		expect(log.sections).toHaveLength(1);
		expect(log.sections[0].kind).toBe('intro');
		expect(log.sections[0].groups[0].text.map(text)).toEqual([
			'Kleinere Verbesserungen der VM-Seite. Und mehr.',
			'Zweiter Absatz.',
		]);
	});

	it('has nothing to show for a title alone, an empty text and a signature', () => {
		const one = parseChangelog('# TitanOS 0.46.5 — Sicherheitsupdate (Autopilot)');
		expect(one.lead).toBe('Sicherheitsupdate (Autopilot)');
		expect(one.sections).toEqual([]);
		expect(parseChangelog('')).toEqual({ title: '', lead: '', sections: [] });
		const signed = parseChangelog(
			'# T — x\n\n## Neu\n- a\n-----BEGIN PGP SIGNATURE-----\n\nabc=\n-----END PGP SIGNATURE-----\n',
		);
		expect(signed.sections).toHaveLength(1);
		expect(signed.sections[0].count).toBe(1);
	});

	it('starts a new part for a paragraph after bullets and ignores rules', () => {
		const log = parseChangelog('## Prüfung\n- a\n- b\n\nDanach Text.\n\n---\n\n- c\n');
		const groups = log.sections[0].groups;
		expect(groups.map((g) => [g.text.length, g.items.length])).toEqual([
			[0, 2],
			[1, 1],
		]);
		expect(log.sections[0].count).toBe(3);
	});

	it('never produces markup: tags and scripts stay text', () => {
		const log = parseChangelog(
			'## Neu\n- <img src=x onerror=alert(1)> und <script>alert(2)</script>\n- [link](https://evil.example) und [js](javascript:alert(3))\n',
		);
		const [first, second] = log.sections[0].groups[0].items;
		expect(first.parts).toEqual([
			{ text: '<img src=x onerror=alert(1)> und <script>alert(2)</script>' },
		]);
		expect(second.parts).toEqual([
			{ text: '[link](https://evil.example) und [js](javascript:alert(3))' },
		]);
	});
});

describe('inline', () => {
	it('links only to the project', () => {
		expect(
			inline('siehe [Release](https://github.com/ra5on/TitanOS/releases/tag/v0.75.0).'),
		).toEqual([
			{ text: 'siehe ' },
			{ text: 'Release', href: 'https://github.com/ra5on/TitanOS/releases/tag/v0.75.0' },
			{ text: '.' },
		]);
		// Real one of 0.72/0.73: another repository stays text.
		expect(inline('(Repo [TitanOS-Office](https://github.com/ra5on/TitanOS-Office), MIT)')).toEqual(
			[{ text: '(Repo [TitanOS-Office](https://github.com/ra5on/TitanOS-Office), MIT)' }],
		);
		expect(inline('https://github.com/ra5on/TitanOS/issues/5.')).toEqual([
			{
				text: 'https://github.com/ra5on/TitanOS/issues/5',
				href: 'https://github.com/ra5on/TitanOS/issues/5',
			},
			{ text: '.' },
		]);
		expect(inline('http://github.com/ra5on/TitanOS und https://example.org')).toEqual([
			{ text: 'http://github.com/ra5on/TitanOS und https://example.org' },
		]);
	});

	it('checks where the project address ends', () => {
		expect(projectLink('https://github.com/ra5on/TitanOS')).toBe(true);
		expect(projectLink('https://github.com/ra5on/TitanOS/releases')).toBe(true);
		expect(projectLink('https://github.com/ra5on/TitanOS#readme')).toBe(true);
		expect(projectLink('https://github.com/ra5on/TitanOS-Office')).toBe(false);
		expect(projectLink('https://github.com/ra5on/TitanOS.evil.example/x')).toBe(false);
		expect(projectLink('https://github.com/ra5on/TitanOS@evil.example')).toBe(false);
		expect(projectLink('https://github.com.evil.example/ra5on/TitanOS')).toBe(false);
		expect(projectLink('javascript:alert(1)')).toBe(false);
	});

	it('reads bold, code and code inside bold; leaves the rest as written', () => {
		expect(inline('**fett** und `code` und **mit `code` drin**')).toEqual([
			{ text: 'fett', bold: true },
			{ text: ' und ' },
			{ text: 'code', code: true },
			{ text: ' und ' },
			{ text: 'mit ', bold: true },
			{ text: 'code', bold: true, code: true },
			{ text: ' drin', bold: true },
		]);
		expect(inline('ein *Stern* und ** offen und `offen')).toEqual([
			{ text: 'ein *Stern* und ** offen und `offen' },
		]);
		expect(inline('')).toEqual([]);
	});
});

describe('helpers', () => {
	it('gives the lead of the first line (as the version chooser shows it)', () => {
		expect(leadOf('# TitanOS 0.46.2 — Kernel 6.12.115, neue Signierschlüssel\n\nMehr.')).toBe(
			'Kernel 6.12.115, neue Signierschlüssel',
		);
		expect(leadOf('\n\nKleinere Verbesserungen.')).toBe('Kleinere Verbesserungen.');
		expect(leadOf('')).toBe('');
	});

	it('builds the address of the release page from the version', () => {
		expect(releaseUrl('0.76.0')).toBe('https://github.com/ra5on/TitanOS/releases/tag/v0.76.0');
		expect(releaseUrl('0.76')).toBe('https://github.com/ra5on/TitanOS/releases/tag/v0.76.0');
		expect(releaseUrl('v0.46.5')).toBe('https://github.com/ra5on/TitanOS/releases/tag/v0.46.5');
		expect(releaseUrl('0.80.0-beta.1')).toBe(
			'https://github.com/ra5on/TitanOS/releases/tag/v0.80.0-beta.1',
		);
		expect(projectLink(releaseUrl('x/../../evil?a=b#c'))).toBe(true);
		expect(releaseUrl('x/../../evil?a=b#c')).not.toContain('/../');
	});

	it('cuts a section to its first bullets, group by group', () => {
		const section = parseChangelog(v075).sections[2];
		expect(section.count).toBe(3);
		const two = firstItems(section, 2);
		expect(two.map((s) => [s.group.title, s.items.length])).toEqual([['Sicherheit und Zugang', 2]]);
		const all = firstItems(section, section.count);
		expect(all.map((s) => [s.group.title, s.items.length])).toEqual([
			['Sicherheit und Zugang', 2],
			['Speicher', 1],
		]);
		// Text without bullets stays (a section of paragraphs has no limit).
		const back = parseChangelog(v075).sections[4];
		expect(firstItems(back, 6)).toHaveLength(1);
	});
});
