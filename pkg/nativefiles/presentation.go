package nativefiles

import "fmt"

func group() string {
	return `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`
}
func shape(id int, name string, lines []string, y, h, size int) string {
	paragraphs := ""
	for _, s := range lines {
		paragraphs += fmt.Sprintf(`<a:p><a:r><a:rPr lang="en-US" sz="%d"/><a:t>%s</a:t></a:r><a:endParaRPr lang="en-US"/></a:p>`, size, esc(s))
	}
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="609600" y="%d"/><a:ext cx="10972800" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr wrap="square"/><a:lstStyle/>%s</p:txBody></p:sp>`, id, esc(name), y, h, paragraphs)
}
func root(tag, body string) string {
	return xmlFile(`<p:` + tag + ` xmlns:a="` + drawingNS + `" xmlns:r="` + officeRel[:len(officeRel)-1] + `" xmlns:p="` + presentationNS + `">` + body + `</p:` + tag + `>`)
}

func presentation(parts map[string]string, d Document) string {
	ids, links, types := "", "", ""
	for i, s := range d.Slides {
		n := i + 1
		slide := fmt.Sprintf("ppt/slides/slide%d.xml", n)
		notes := fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", n)
		parts[slide] = root("sld", `<p:cSld name="`+esc(s.Title)+`"><p:spTree>`+group()+shape(2, "Title", []string{s.Title}, 365760, 914400, 3200)+shape(3, "Body", s.Body, 1463040, 4572000, 2000)+`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr>`)
		parts[fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", n)] = rels(rel("rId1", "slideLayout", "../slideLayouts/slideLayout1.xml") + rel("rId2", "notesSlide", fmt.Sprintf("../notesSlides/notesSlide%d.xml", n)))
		noteText := []string{s.Notes}
		noteText = append(noteText, s.Sources...)
		noteText = append(noteText, d.Sources...)
		parts[notes] = root("notes", `<p:cSld><p:spTree>`+group()+shape(2, "Notes", noteText, 0, 6858000, 1200)+`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr>`)
		parts[fmt.Sprintf("ppt/notesSlides/_rels/notesSlide%d.xml.rels", n)] = rels(rel("rId1", "slide", fmt.Sprintf("../slides/slide%d.xml", n)))
		ids += fmt.Sprintf(`<p:sldId id="%d" r:id="rId%d"/>`, 255+n, n+1)
		links += rel(fmt.Sprintf("rId%d", n+1), "slide", fmt.Sprintf("slides/slide%d.xml", n))
		types += override(slide, "application/vnd.openxmlformats-officedocument.presentationml.slide+xml") + override(notes, "application/vnd.openxmlformats-officedocument.presentationml.notesSlide+xml")
	}
	parts["ppt/presentation.xml"] = root("presentation", `<p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst>`+ids+`</p:sldIdLst><p:sldSz cx="12192000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/>`)
	parts["ppt/_rels/presentation.xml.rels"] = rels(rel("rId1", "slideMaster", "slideMasters/slideMaster1.xml") + links)
	parts["ppt/slideMasters/slideMaster1.xml"] = root("sldMaster", `<p:cSld><p:spTree>`+group()+`</p:spTree></p:cSld><p:clrMap accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" bg1="lt1" bg2="lt2" folHlink="folHlink" hlink="hlink" tx1="dk1" tx2="dk2"/><p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst><p:txStyles><p:titleStyle/><p:bodyStyle/><p:otherStyle/></p:txStyles>`)
	parts["ppt/slideMasters/_rels/slideMaster1.xml.rels"] = rels(rel("rId1", "slideLayout", "../slideLayouts/slideLayout1.xml") + rel("rId2", "theme", "../theme/theme1.xml"))
	parts["ppt/slideLayouts/slideLayout1.xml"] = root("sldLayout", `<p:cSld name="Blank"><p:spTree>`+group()+`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr>`)
	parts["ppt/slideLayouts/_rels/slideLayout1.xml.rels"] = rels(rel("rId1", "slideMaster", "../slideMasters/slideMaster1.xml"))
	colors := ""
	for _, c := range []struct{ name, color string }{{"dk1", "17212B"}, {"lt1", "FFFFFF"}, {"dk2", "35495E"}, {"lt2", "F4F5F7"}, {"accent1", "3155CB"}, {"accent2", "007D77"}, {"accent3", "946B00"}, {"accent4", "9D3970"}, {"accent5", "4A7D34"}, {"accent6", "B04F30"}, {"hlink", "3155CB"}, {"folHlink", "7444A1"}} {
		colors += `<a:` + c.name + `><a:srgbClr val="` + c.color + `"/></a:` + c.name + `>`
	}
	parts["ppt/theme/theme1.xml"] = xmlFile(`<a:theme xmlns:a="` + drawingNS + `" name="Humagen"><a:themeElements><a:clrScheme name="Humagen">` + colors + `</a:clrScheme><a:fontScheme name="Humagen"><a:majorFont><a:latin typeface="Aptos Display"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont><a:minorFont><a:latin typeface="Aptos"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme><a:fmtScheme name="Humagen"><a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst><a:lnStyleLst><a:ln w="9525"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:prstDash val="solid"/></a:ln></a:lnStyleLst><a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst><a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst></a:fmtScheme></a:themeElements></a:theme>`)
	parts["_rels/.rels"] = rels(rel("rId1", "officeDocument", "ppt/presentation.xml"))
	for name, m := range map[string]string{"ppt/presentation.xml": "presentation.main", "ppt/slideMasters/slideMaster1.xml": "slideMaster", "ppt/slideLayouts/slideLayout1.xml": "slideLayout"} {
		types += override(name, "application/vnd.openxmlformats-officedocument.presentationml."+m+"+xml")
	}
	return types + override("ppt/theme/theme1.xml", "application/vnd.openxmlformats-officedocument.theme+xml")
}
