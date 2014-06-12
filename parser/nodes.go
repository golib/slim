package parser

var selfClosingTags = [...]string{
	"meta",
	"img",
	"link",
	"input",
	"source",
	"area",
	"base",
	"col",
	"br",
	"hr",
}

var doctypes = map[string]string{
	"5":            `<!DOCTYPE html>`,
	"default":      `<!DOCTYPE html>`,
	"xml":          `<?xml version="1.0" encoding="utf-8" ?>`,
	"transitional": `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">`,
	"strict":       `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Strict//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd">`,
	"frameset":     `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Frameset//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-frameset.dtd">`,
	"1.1":          `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN" "http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd">`,
	"basic":        `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML Basic 1.1//EN" "http://www.w3.org/TR/xhtml-basic/xhtml-basic11.dtd">`,
	"mobile":       `<!DOCTYPE html PUBLIC "-//WAPFORUM//DTD XHTML Mobile 1.2//EN" "http://www.openmobilealliance.org/tech/DTD/xhtml-mobile12.dtd">`,
}

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

type Doctype struct {
	SourcePosition
	Value string
}

func newDoctype(value string) *Doctype {
	node := new(Doctype)
	node.Value = value
	return node
}

func (d *Doctype) String() string {
	if defined := doctypes[d.Value]; len(defined) != 0 {
		return defined
	}

	return `<!DOCTYPE ` + d.Value + `>`
}

type Comment struct {
	SourcePosition
	Value  string
	Block  *Block
	Silent bool
}

func newComment(value string) *Comment {
	node := new(Comment)
	node.Value = value
	node.Block = nil
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

func (t *Tag) IsSelfClosing() bool {
	for _, tag := range selfClosingTags {
		if tag == t.Name {
			return true
		}
	}

	return false
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
