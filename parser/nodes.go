package parser

import (
	"regexp"
)

// HTML const
const (
	FORMAT_HTML  = "html"
	FORMAT_XHTML = "xhtml"

	WRAPPER_BOTH = iota
	WRAPPER_COMMENT
	WRAPPER_CDATA
)

var (
	_XDOCTYPES = map[string]string{
		"1.1":          `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN" "http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd">`,
		"5":            `<!DOCTYPE html>`,
		"html":         `<!DOCTYPE html>`,
		"strict":       `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Strict//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd">`,
		"frameset":     `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Frameset//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-frameset.dtd">`,
		"mobile":       `<!DOCTYPE html PUBLIC "-//WAPFORUM//DTD XHTML Mobile 1.2//EN" "http://www.openmobilealliance.org/tech/DTD/xhtml-mobile12.dtd">`,
		"basic":        `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML Basic 1.1//EN" "http://www.w3.org/TR/xhtml-basic/xhtml-basic11.dtd">`,
		"transitional": `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">`,
	}

	_DOCTYPES = map[string]string{
		"5":            `<!DOCTYPE html>`,
		"html":         `<!DOCTYPE html>`,
		"strict":       `<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01//EN" "http://www.w3.org/TR/html4/strict.dtd">`,
		"frameset":     `<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01 Frameset//EN" "http://www.w3.org/TR/html4/frameset.dtd">`,
		"transitional": `<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN" "http://www.w3.org/TR/html4/loose.dtd">`,
	}

	_AUTOCLOSES = map[string]bool{
		"base":     true,
		"basefont": true,
		"bgsound":  true,
		"link":     true,
		"meta":     true,
		"area":     true,
		"br":       true,
		"embed":    true,
		"img":      true,
		"keygen":   true,
		"wbr":      true,
		"input":    true,
		"menuitem": true,
		"param":    true,
		"source":   true,
		"track":    true,
		"hr":       true,
		"col":      true,
		"frame":    true,
	}
)

type Noder interface {
	Pos() SourcePosition
}

type SourcePosition struct {
	Line        int
	Column      int
	Filename    string
	TokenLength int
}

func (s *SourcePosition) Pos() SourcePosition {
	return *s
}

type Wrapper struct {
	L string
	R string
}

func NewWrapper() *Wrapper {
	return &Wrapper{"<!--\n//<![CDATA[\n", "\n//]]>\n//-->"}
}

func NewCommentWrapper() *Wrapper {
	return &Wrapper{"<!--\n", "\n//-->"}
}

func NewCdataWrapper() *Wrapper {
	return &Wrapper{"\n//<![CDATA[\n", "\n//]]>\n"}
}

func NewConditionWrapper(condition string) *Wrapper {
	return &Wrapper{"<!--[if " + condition + "]>\n", "\n<![endif]>\n"}
}

type Doctype struct {
	SourcePosition
	Value  string
	Format string
}

func newDoctype(value, format string) *Doctype {
	node := new(Doctype)
	node.Value = value
	node.Format = format
	return node
}

// returns the actual string of doctype shortcut.
func (d *Doctype) String() string {
	var (
		dt   = ""
		ok   = false
		rxml = regexp.MustCompile(`^xml(\s+(.+?))?$`)
	)

	if rxml.MatchString(d.Value) {
		if d.Format == FORMAT_HTML {
			panic("Invalid xml directive with html format")
		}

		encoding := rxml.FindAllString(d.Value, 1)
		if len(encoding) == 1 {
			encoding = append(encoding, "utf-8")
		}

		dt = `<?xml version="1.0" encoding="` + encoding[1] + `" ?>`
		ok = true
	} else {
		switch d.Format {
		case FORMAT_HTML:
			dt, ok = _DOCTYPES[d.Value]
		case FORMAT_XHTML:
			dt, ok = _XDOCTYPES[d.Value]
		}
	}

	if ok == false {
		switch d.Format {
		case FORMAT_HTML:
			dt = _DOCTYPES["5"]
		case FORMAT_XHTML:
			dt = _XDOCTYPES["5"]
		default:
			dt = _DOCTYPES["5"]
		}
	}

	return dt
}

type Comment struct {
	SourcePosition
	Value   string
	Block   *Block
	Wrapper *Wrapper
	Silent  bool
}

func newComment(value string) *Comment {
	node := new(Comment)
	node.Value = value
	node.Block = nil
	node.Wrapper = nil
	node.Silent = false
	return node
}

type Text struct {
	SourcePosition
	Value string
	IsRaw bool
}

func newText(value string, raw bool) *Text {
	node := new(Text)
	node.Value = value
	node.IsRaw = raw
	return node
}

type Attribute struct {
	SourcePosition
	Name      string
	Value     string
	Condition string
	IsRaw     bool
}

type Tag struct {
	SourcePosition
	Name           string
	Block          *Block
	Attributes     []Attribute
	IsInterpolated bool
	IsRawHtml      bool
}

func newTag(name string) *Tag {
	node := new(Tag)
	node.Name = name
	node.Block = nil
	node.Attributes = make([]Attribute, 0)
	node.IsInterpolated = false
	node.IsRawHtml = false
	return node
}

// Whether is the tag autoclose?
func (t *Tag) IsAutoclose() bool {
	return _AUTOCLOSES[t.Name]
}

func (t *Tag) IsRawText() bool {
	return t.IsRawHtml || t.Name == "style" || t.Name == "script"
}

type Block struct {
	SourcePosition
	Children []Noder
}

func newBlock() *Block {
	block := new(Block)
	block.Children = make([]Noder, 0)
	return block
}

func (b *Block) push(node Noder) {
	b.Children = append(b.Children, node)
}

func (b *Block) unshift(node Noder) {
	b.Children = append([]Noder{node}, b.Children...)
}

func (b *Block) CanInline() bool {
	if len(b.Children) == 0 {
		return true
	}

	allText := true
	for _, child := range b.Children {
		if txt, ok := child.(*Text); !ok || txt.IsRaw {
			allText = false
			break
		}
	}

	return allText
}

const (
	NamedBlockDefault = iota
	NamedBlockAppend
	NamedBlockPrepend
)

type NamedBlock struct {
	Block
	Name     string
	Modifier int
}

func newNamedBlock(name string) *NamedBlock {
	node := new(NamedBlock)
	node.Name = name
	node.Block.Children = make([]Noder, 0)
	node.Modifier = NamedBlockDefault
	return node
}

type Statement struct {
	SourcePosition
	Expression string
}

func newStatement(expression string) *Statement {
	node := new(Statement)
	node.Expression = expression
	return node
}

type Assignment struct {
	SourcePosition
	Variable   string
	Expression string
}

func newAssignment(variable, expression string) *Assignment {
	node := new(Assignment)
	node.Variable = variable
	node.Expression = expression
	return node
}

type Condition struct {
	SourcePosition
	Positive   *Block
	Negative   *Block
	Expression string
}

func newCondition(expression string) *Condition {
	node := new(Condition)
	node.Expression = expression
	return node
}

type Range struct {
	SourcePosition
	Key        string
	Value      string
	Expression string
	Block      *Block
}

func newRange(key, value, expression string) *Range {
	node := new(Range)
	node.Key = key
	node.Value = value
	node.Expression = expression
	return node
}
