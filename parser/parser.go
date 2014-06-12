package parser

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"strings"
)

func NewStringParser(input string) (*Parser, error) {
	return newParser(bytes.NewReader([]byte(input))), nil
}

func NewFileParser(filename string) (*Parser, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	parser := newParser(bytes.NewReader(data))
	parser.filename = filename
	return parser, nil
}

type Parser struct {
	scanner       *scanner
	parent        *Parser
	token         *token
	result        *Block
	filename      string
	filepath      string
	fileextension string
	namedBlocks   map[string]*NamedBlock
}

func newParser(r io.Reader) *Parser {
	p := new(Parser)
	p.scanner = newScanner(r)
	p.fileextension = ".html.slim"
	p.namedBlocks = make(map[string]*NamedBlock)
	return p
}

func (p *Parser) SetPath(path string) {
	p.filepath = path
	return
}

func (p *Parser) SetExtension(extension string) {
	p.fileextension = extension
	return
}

func (p *Parser) newFileParser(filename string) *Parser {
	if len(p.filepath) == 0 {
		panic("Unable to import/extend " + filename + " with empty filepath.")
	}

	filename = filepath.Join(p.filepath, filename)

	if !strings.HasSuffix(strings.ToLower(filename), p.fileextension) {
		filename += p.fileextension
	}

	parser, err := NewFileParser(filename)
	if err != nil {
		panic("Failed to import/extend " + filename + " with error " + err.Error())
	}

	return parser
}

func (p *Parser) Parse() *Block {
	if p.result != nil {
		return p.result
	}

	defer func() {
		if r := recover(); r != nil {
			if rs, ok := r.(string); ok && rs[:len("Slim Error")] == "Slim Error" {
				panic(r)
			}

			pos := p.pos()

			if len(pos.Filename) > 0 {
				panic(fmt.Sprintf("Slim Error in <%s>: %v - Line: %d, Column: %d, Length: %d", pos.Filename, r, pos.Line, pos.Column, pos.TokenLength))
			} else {
				panic(fmt.Sprintf("Slim Error: %v - Line: %d, Column: %d, Length: %d", r, pos.Line, pos.Column, pos.TokenLength))
			}
		}
	}()

	block := newBlock()

	for {
		p.scanToken()

		if p.token == nil || p.token.Kind == tokEOF {
			break
		}

		if p.token.Kind == tokBlank {
			continue
		}

		block.push(p.parseToken())
	}

	if p.parent != nil {
		p.parent.Parse()

		for _, prev := range p.parent.namedBlocks {
			ours := p.namedBlocks[prev.Name]

			if ours == nil {
				continue
			}

			switch ours.Modifier {
			case NamedBlockAppend:
				for i := 0; i < len(ours.Children); i++ {
					prev.push(ours.Children[i])
				}
			case NamedBlockPrepend:
				for i := len(ours.Children) - 1; i >= 0; i-- {
					prev.unshift(ours.Children[i])
				}
			default:
				prev.Children = ours.Children
			}
		}

		block = p.parent.result
	}

	p.result = block
	return block
}

func (p *Parser) pos() SourcePosition {
	pos := p.scanner.Pos()
	pos.Filename = p.filename
	return pos
}

func (p *Parser) scanToken() {
	p.token = p.scanner.Next()
}

func (p *Parser) parseToken() Noder {
	switch p.token.Kind {
	case tokIndent:
		return p.parseBlock(nil)
	case tokDoctype:
		return p.parseDoctype()
	case tokComment:
		return p.parseComment()
	case tokTag:
		return p.parseTag()
	case tokText:
		return p.parseText()
	case tokAssignment:
		return p.parseAssignment()
	case tokIf:
		return p.parseCondition()
	case tokEach:
		return p.parseEach()
	case tokNamedBlock:
		return p.parseNamedBlock()
	case tokImport:
		return p.parseImport()
	case tokExtend:
		return p.parseExtend()
	}

	panic(fmt.Sprintf("Unexpected token: %d", p.token.Kind))
}

func (p *Parser) expectToken(kind rune) *token {
	if p.token.Kind != kind {
		panic(fmt.Sprintf("Expect token kind %d, but got %d", kind, p.token.Kind))
	}

	tok := p.token

	p.scanToken()

	return tok
}

func (p *Parser) parseBlock(parent Noder) *Block {
	p.expectToken(tokIndent)

	block := newBlock()
	block.SourcePosition = p.pos()

	for {
		if p.token == nil || p.token.Kind == tokEOF || p.token.Kind == tokOutdent {
			break
		}

		if p.token.Kind == tokBlank {
			p.scanToken()
			continue
		}

		if p.token.Kind == tokId ||
			p.token.Kind == tokClass ||
			p.token.Kind == tokAttribute {
			tag, ok := parent.(*Tag)
			if !ok {
				panic("Conditional attributes must be placed immediately within a parent tag.")
			}

			attr := p.expectToken(p.token.Kind)
			cond := attr.Data["Condition"]

			switch attr.Kind {
			case tokId:
				tag.Attributes = append(tag.Attributes, Attribute{p.pos(), "id", attr.Value, true, cond})
			case tokClass:
				tag.Attributes = append(tag.Attributes, Attribute{p.pos(), "class", attr.Value, true, cond})
			case tokAttribute:
				tag.Attributes = append(tag.Attributes, Attribute{p.pos(), attr.Value, attr.Data["Content"], attr.Data["Mode"] == "raw", cond})
			}

			continue
		}

		block.push(p.parseToken())
	}

	p.expectToken(tokOutdent)

	return block
}

func (p *Parser) parseDoctype() *Doctype {
	tok := p.expectToken(tokDoctype)

	node := newDoctype(tok.Value)
	node.SourcePosition = p.pos()
	return node
}

func (p *Parser) parseComment() *Comment {
	tok := p.expectToken(tokComment)

	node := newComment(tok.Value)
	node.SourcePosition = p.pos()
	node.Silent = tok.Data["Mode"] == "silent"

	if p.token.Kind == tokIndent {
		node.Block = p.parseBlock(node)
	}

	return node
}

func (p *Parser) parseTag() *Tag {
	tok := p.expectToken(tokTag)

	tag := newTag(tok.Value)
	tag.SourcePosition = p.pos()

	ensureBlock := func() {
		if tag.Block == nil {
			tag.Block = newBlock()
		}
	}

readmore:
	switch p.token.Kind {
	case tokIndent:
		if tag.IsRawText() {
			p.scanner.readRaw = true
		}

		block := p.parseBlock(tag)
		if tag.Block == nil {
			tag.Block = block
		} else {
			for _, child := range block.Children {
				tag.Block.push(child)
			}
		}
	case tokId:
		id := p.expectToken(tokId)
		if len(id.Data["Condition"]) > 0 {
			panic("Conditional attributes(id) must be placed in a block within a tag.")
		}

		tag.Attributes = append(tag.Attributes, Attribute{p.pos(), "id", id.Value, true, ""})

		goto readmore
	case tokClass:
		klass := p.expectToken(tokClass)
		if len(klass.Data["Condition"]) > 0 {
			panic("Conditional attributes(class) must be placed in a block within a tag.")
		}

		tag.Attributes = append(tag.Attributes, Attribute{p.pos(), "class", klass.Value, true, ""})

		goto readmore
	case tokAttribute:
		attr := p.expectToken(tokAttribute)
		if len(attr.Data["Condition"]) > 0 {
			panic("Conditional attributes must be placed in a block within a tag.")
		}

		tag.Attributes = append(tag.Attributes, Attribute{p.pos(), attr.Value, attr.Data["Content"], attr.Data["Mode"] == "raw", ""})

		goto readmore
	case tokText:
		if p.token.Data["Mode"] != "piped" {
			ensureBlock()

			tag.Block.unshift(p.parseText())

			goto readmore
		}
	}

	return tag
}

func (p *Parser) parseText() *Text {
	tok := p.expectToken(tokText)

	node := newText(tok.Value, tok.Data["Mode"] == "raw")
	node.SourcePosition = p.pos()
	return node
}

func (p *Parser) parseAssignment() *Assignment {
	tok := p.expectToken(tokAssignment)

	node := newAssignment(tok.Data["Variable"], tok.Value)
	node.SourcePosition = p.pos()
	return node
}

func (p *Parser) parseCondition() *Condition {
	tok := p.expectToken(tokIf)

	node := newCondition(tok.Value)
	node.SourcePosition = p.pos()

readmore:
	switch p.token.Kind {
	case tokIndent:
		node.Positive = p.parseBlock(node)

		goto readmore
	case tokElseIf:
		p.expectToken(tokElseIf)
		if p.token.Kind != tokElseIf {
			panic("Unexpected token!")
		}

		node.Negative = newBlock()
		node.Negative.push(p.parseCondition())

		goto readmore
	case tokElse:
		p.expectToken(tokElse)

		if p.token.Kind == tokIf {
			node.Negative = newBlock()
			node.Negative.push(p.parseCondition())
		} else if p.token.Kind == tokIndent {
			node.Negative = p.parseBlock(node)
		} else {
			panic("Unexpected token!")
		}

		goto readmore
	}

	return node
}

func (p *Parser) parseEach() *Each {
	tok := p.expectToken(tokEach)

	node := newRange(tok.Data["Key"], tok.Data["Value"], tok.Value)
	node.SourcePosition = p.pos()

	if p.token.Kind == tokIndent {
		node.Block = p.parseBlock(node)
	}

	return node
}

func (p *Parser) parseNamedBlock() *Block {
	tok := p.expectToken(tokNamedBlock)

	if p.namedBlocks[tok.Value] != nil {
		panic("Multiple definitions of named blocks are not permitted. Block " + tok.Value + " has been redefined.")
	}

	block := newNamedBlock(tok.Value)
	block.SourcePosition = p.pos()

	if tok.Data["Modifier"] == "append" {
		block.Modifier = NamedBlockAppend
	} else if tok.Data["Modifier"] == "prepend" {
		block.Modifier = NamedBlockPrepend
	}

	if p.token.Kind == tokIndent {
		block.Block = *(p.parseBlock(nil))
	}

	p.namedBlocks[block.Name] = block

	if block.Modifier == NamedBlockDefault {
		return &block.Block
	}

	return newBlock()
}

func (p *Parser) parseImport() *Block {
	tok := p.expectToken(tokImport)

	node := p.newFileParser(tok.Value).Parse()
	node.SourcePosition = p.pos()
	return node
}

func (p *Parser) parseExtend() *Block {
	if p.parent != nil {
		panic("Unable to extend multiple parent templates.")
	}

	tok := p.expectToken(tokExtend)

	parser := p.newFileParser(tok.Value)
	parser.Parse()
	p.parent = parser
	return newBlock()
}
