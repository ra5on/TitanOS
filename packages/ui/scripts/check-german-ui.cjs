const fs = require('node:fs')
const path = require('node:path')
const ts = require('typescript')
const localeRoot = path.resolve(__dirname, '../public/locales')
const sourceRoot = path.resolve(__dirname, '../src')
const locales = Object.fromEntries(
	['en', 'de'].map((name) => [name, JSON.parse(fs.readFileSync(path.join(localeRoot, name + '.json'), 'utf8'))]),
)
function hasKey(values, key, context) {
	if (key in values) return true
	const base = context ? key + '_' + context : key
	return base in values || Object.keys(values).some((k) => k.replace(/_(zero|one|two|few|many|other)$/, '') === base)
}
const missing = {en: {}, de: {}}
let calls = 0
function visitFile(file) {
	const source = ts.createSourceFile(file, fs.readFileSync(file, 'utf8'), ts.ScriptTarget.Latest, true)
	function walk(n) {
		if (ts.isCallExpression(n) && ts.isIdentifier(n.expression) && ['t', 'maybeT'].includes(n.expression.text)) {
			const arg = n.arguments[0]
			if (arg && (ts.isStringLiteral(arg) || ts.isNoSubstitutionTemplateLiteral(arg))) {
				calls++
				let context
				const options = n.arguments[1]
				if (options && ts.isObjectLiteralExpression(options)) {
					const p = options.properties.find((p) => ts.isPropertyAssignment(p) && p.name.getText(source) === 'context')
					if (p && ts.isStringLiteral(p.initializer)) context = p.initializer.text
				}
				const line = source.getLineAndCharacterOfPosition(n.getStart(source)).line + 1
				const loc = path.relative(sourceRoot, file) + ':' + line
				for (const [name, values] of Object.entries(locales))
					if (!hasKey(values, arg.text, context)) (missing[name][arg.text] ??= []).push(loc)
			}
		}
		ts.forEachChild(n, walk)
	}
	walk(source)
}
function walkDir(dir) {
	for (const entry of fs.readdirSync(dir, {withFileTypes: true})) {
		const file = path.join(dir, entry.name)
		if (entry.isDirectory()) walkDir(file)
		else if (/\.tsx?$/.test(entry.name) && !/\.(unit\.)?test\./.test(entry.name)) visitFile(file)
	}
}
walkDir(sourceRoot)

console.log(`Scanned ${calls} literal translation calls using the TypeScript AST; comments ignored.`)
for (const [name, keys] of Object.entries(missing)) {
	console.log(name, `missing: ${Object.keys(keys).length}`)
	for (const [key, locations] of Object.entries(keys)) console.log(JSON.stringify(key), locations.join(', '))
}

if (Object.values(missing).some((values) => Object.keys(values).length)) process.exitCode = 1
