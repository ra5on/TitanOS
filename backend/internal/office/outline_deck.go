// OutlineDeck turns blank-line-separated groups into editable slides. It emits
// native DrawingML text boxes; supplied source text is always XML-escaped.
package office

import (
	"errors"
	"fmt"
	"strings"
)

const presentationNS = "http://schemas.openxmlformats.org/presentationml/2006/main"
const drawingNS = "http://schemas.openxmlformats.org/drawingml/2006/main"
const relNS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
const slideNamespaces = ` xmlns:p="` + presentationNS + `" xmlns:a="` + drawingNS + `" xmlns:r="` + relNS + `"`
const groupShape = `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`

func slideTextBox(id int, name string, y, height, size int, lines []string) string {
	var paras strings.Builder
	for _, line := range lines {
		fmt.Fprintf(&paras, `<a:p><a:r><a:rPr lang="de-DE" sz="%d"/><a:t>%s</a:t></a:r><a:endParaRPr lang="de-DE"/></a:p>`, size, xmlText(line))
	}
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="650000" y="%d"/><a:ext cx="10800000" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr><p:txBody><a:bodyPr wrap="square"><a:normAutofit/></a:bodyPr><a:lstStyle/>%s</p:txBody></p:sp>`, id, name, y, height, paras.String())
}

func deckRelationships(entries string) string {
	return `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + entries + `</Relationships>`
}
func deckRel(id int, kind, target string) string {
	return fmt.Sprintf(`<Relationship Id="rId%d" Type="%s/%s" Target="%s"/>`, id, relNS, kind, target)
}
func deckPart(name, kind string) string {
	return fmt.Sprintf(`<Override PartName="/%s" ContentType="application/vnd.openxmlformats-officedocument.presentationml.%s+xml"/>`, name, kind)
}

func OutlineDeck(outline string) ([]byte, error) {
	if len(outline) > 32768 || strings.TrimSpace(outline) == "" {
		return nil, errors.New("die Gliederung muss Text enthalten und in 32 KiB passen")
	}
	var groups [][]string
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(outline, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
			continue
		}
		if len(lines) > 0 {
			groups = append(groups, lines)
			lines = nil
		}
	}
	if len(lines) > 0 {
		groups = append(groups, lines)
	}
	if len(groups) > 50 {
		return nil, errors.New("maximal 50 Folien pro Entwurf")
	}
	var slides, parts, rels strings.Builder
	var entries [][2]string
	add := func(name, xml string) { entries = append(entries, [2]string{name, xml}) }
	parts.WriteString(deckPart("ppt/presentation.xml", "presentation.main"))
	parts.WriteString(deckPart("ppt/slideMasters/slideMaster1.xml", "slideMaster"))
	parts.WriteString(deckPart("ppt/slideLayouts/slideLayout1.xml", "slideLayout"))
	rels.WriteString(deckRel(1, "slideMaster", "slideMasters/slideMaster1.xml"))
	add("_rels/.rels", deckRelationships(deckRel(1, "officeDocument", "ppt/presentation.xml")))
	add("ppt/slideMasters/slideMaster1.xml", `<p:sldMaster`+slideNamespaces+`><p:cSld><p:spTree>`+groupShape+`</p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/><p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst><p:txStyles><p:titleStyle/><p:bodyStyle/><p:otherStyle/></p:txStyles></p:sldMaster>`)
	add("ppt/slideMasters/_rels/slideMaster1.xml.rels", deckRelationships(deckRel(1, "slideLayout", "../slideLayouts/slideLayout1.xml")))
	add("ppt/slideLayouts/slideLayout1.xml", `<p:sldLayout`+slideNamespaces+` type="blank" preserve="1"><p:cSld name="Leer"><p:spTree>`+groupShape+`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`)
	add("ppt/slideLayouts/_rels/slideLayout1.xml.rels", deckRelationships(deckRel(1, "slideMaster", "../slideMasters/slideMaster1.xml")))
	for i, g := range groups {
		num := i + 1
		part := fmt.Sprintf("slides/slide%d.xml", num)
		parts.WriteString(deckPart("ppt/"+part, "slide"))
		rels.WriteString(deckRel(num+1, "slide", part))
		fmt.Fprintf(&slides, `<p:sldId id="%d" r:id="rId%d"/>`, 256+i, num+1)
		shapes := slideTextBox(2, "Titel", 480000, 1100000, 3600, g[:1])
		if len(g) > 1 {
			shapes += slideTextBox(3, "Text", 1800000, 4400000, 2200, g[1:])
		}
		add("ppt/"+part, `<p:sld`+slideNamespaces+`><p:cSld><p:spTree>`+groupShape+shapes+`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`)
		add(fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", num), deckRelationships(deckRel(1, "slideLayout", "../slideLayouts/slideLayout1.xml")))
	}
	add("ppt/_rels/presentation.xml.rels", deckRelationships(rels.String()))
	add("ppt/presentation.xml", `<p:presentation`+slideNamespaces+`><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst>`+slides.String()+`</p:sldIdLst><p:sldSz cx="12192000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>`)
	add("[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>`+parts.String()+`</Types>`)
	return zipFiles(entries)
}
