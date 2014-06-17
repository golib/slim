package parser

import (
	"bufio"
	"container/list"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
)

const (
	tokEOF = -(iota + 1)
	tokBlank
	tokIndent
	tokOutdent
	tokDoctype
	tokComment
	tokText
	tokTag
	tokId
	tokClass
	tokAttribute
	tokAssignment
	tokIf
	tokElseIf
	tokElse
	tokRange
	tokNamedBlock
	tokImport
	tokExtend
)

const (
	scnNewLine = iota
	scnLine
	scnEOF
)

var (
	rindent     = regexp.MustCompile(`^([ \t]*)`)
	rdoctype    = regexp.MustCompile(`^(?:!|doctype)\s+?(.*)`)
	rcomment    = regexp.MustCompile(`^(?:\/(!)?\s+?(.*)|\/\s*?\[\s*?if\s+?(.+)\s*?\]\s+?(.*))$`)
	rtext       = regexp.MustCompile(`^(\|)? ?(.*)$`)
	rtag        = regexp.MustCompile(`^(\w[-:\w]*)`)
	rid         = regexp.MustCompile(`^#([\w-]+)(?:\s*\?\s*(.*)$)?`)
	rclass      = regexp.MustCompile(`^\.([\w-]+)(?:\s*\?\s*(.*)$)?`)
	rattribute  = regexp.MustCompile(`^\[([\w\-]+)\s*(?:=\s*(\"([^\"\\]*)\"|([^\]]+)))?\](?:\s*\?\s*(.*)$)?`)
	rassignment = regexp.MustCompile(`^(\$[\w0-9\-_]*)?\s*=\s*(.+)$`)
	rif         = regexp.MustCompile(`^if\s*(.+)$`)
	relsif      = regexp.MustCompile(`^elsif\s*(.+)$`)
	relse       = regexp.MustCompile(`^else\s*`)
	rrange      = regexp.MustCompile(`^each\s+(\$[\w0-9\-_]*)(?:\s*,\s*(\$[\w0-9\-_]*))?\s+in\s+(.+)$`)
	rblock      = regexp.MustCompile(`^block\s+(?:(append|prepend)\s+)?([0-9a-zA-Z_\-\. \/]*)$`)
	rimport     = regexp.MustCompile(`^import\s+([0-9a-zA-Z_\-\. \/]*)$`)
	rextend     = regexp.MustCompile(`^extend\s+([0-9a-zA-Z_\-\. \/]*)$`)
)

type token struct {
	Kind  rune
	Value string
	Data  map[string]string
}

type scanner struct {
	reader  *bufio.Reader
	indents *list.List
	stash   *list.List

	buffer string
	line   int
	column int
	state  int32

	lastTokenLine   int
	lastTokenColumn int
	lastTokenSize   int

	readRaw bool
}

func newScanner(r io.Reader) *scanner {
	s := new(scanner)
	s.reader = bufio.NewReader(r)
	s.indents = list.New()
	s.stash = list.New()
	s.line = -1
	s.column = 0
	s.state = scnNewLine

	return s
}

func (s *scanner) Pos() SourcePosition {
	return SourcePosition{
		Line:        s.lastTokenLine + 1,
		Column:      s.lastTokenColumn + 1,
		Filename:    "",
		TokenLength: s.lastTokenSize,
	}
}

func (s *scanner) Next() *token {
	if s.readRaw {
		s.readRaw = false
		return s.scanRaw()
	}

	s.readline()

	if stashed := s.stash.Front(); stashed != nil {
		tok := s.stash.Remove(stashed)
		return tok.(*token)
	}

	switch s.state {
	case scnEOF:
		if outdent := s.indents.Back(); outdent != nil {
			s.indents.Remove(outdent)
			return &token{tokOutdent, "", nil}
		}

		return &token{tokEOF, "", nil}
	case scnNewLine:
		s.state = scnLine

		if tok := s.scanIndent(); tok != nil {
			return tok
		}

		return s.Next()
	case scnLine:
		if tok := s.scanDoctype(); tok != nil {
			return tok
		}

		if tok := s.scanCondition(); tok != nil {
			return tok
		}

		if tok := s.scanRange(); tok != nil {
			return tok
		}

		if tok := s.scanImport(); tok != nil {
			return tok
		}

		if tok := s.scanExtend(); tok != nil {
			return tok
		}

		if tok := s.scanBlock(); tok != nil {
			return tok
		}

		if tok := s.scanAssignment(); tok != nil {
			return tok
		}

		if tok := s.scanTag(); tok != nil {
			return tok
		}

		if tok := s.scanId(); tok != nil {
			return tok
		}

		if tok := s.scanClass(); tok != nil {
			return tok
		}

		if tok := s.scanAttribute(); tok != nil {
			return tok
		}

		if tok := s.scanComment(); tok != nil {
			return tok
		}

		if tok := s.scanText(); tok != nil {
			return tok
		}
	}

	return nil
}

func (s *scanner) scanRaw() *token {
	result := ""
	level := 0

	for {
		s.readline()

		switch s.state {
		case scnEOF:
			return &token{tokText, result, map[string]string{"Mode": "raw"}}
		case scnNewLine:
			s.state = scnLine

			if tok := s.scanIndent(); tok != nil {
				if tok.Kind == tokIndent {
					level++
				} else if tok.Kind == tokOutdent {
					level--
				} else {
					result = result + "\n"
					continue
				}

				if level < 0 {
					s.stash.PushBack(&token{tokOutdent, "", nil})

					if len(result) > 0 {
						result = strings.TrimRightFunc(result, unicode.IsSpace)
					}

					return &token{tokText, result, map[string]string{"Mode": "raw"}}
				}
			}
		case scnLine:
			if len(result) > 0 {
				result = result + "\n"
			}

			for i := 0; i < level; i++ {
				result += "\t"
			}

			result = result + s.buffer

			s.consume(len(s.buffer))
		}
	}

	return nil
}

func (s *scanner) scanIndent() *token {
	if len(s.buffer) == 0 {
		return &token{tokBlank, "", nil}
	}

	var head *list.Element
	for head = s.indents.Front(); head != nil; head = head.Next() {
		rvalue := head.Value.(*regexp.Regexp)

		match := rvalue.FindString(s.buffer)
		if len(match) == 0 {
			break
		}

		s.consume(len(match))
	}

	newIndent := rindent.FindString(s.buffer)

	if len(newIndent) != 0 && head == nil {
		s.indents.PushBack(regexp.MustCompile(regexp.QuoteMeta(newIndent)))

		s.consume(len(newIndent))

		return &token{tokIndent, newIndent, nil}
	}

	if len(newIndent) == 0 && head != nil {
		for head != nil {
			next := head.Next()

			s.indents.Remove(head)
			if next == nil {
				return &token{tokOutdent, "", nil}
			}

			s.stash.PushBack(&token{tokOutdent, "", nil})

			head = next
		}
	}

	if len(newIndent) != 0 && head != nil {
		panic("Mismatching indentation. Please use a coherent indent schema.")
	}

	return nil
}

func (s *scanner) scanDoctype() *token {
	if matches := rdoctype.FindStringSubmatch(s.buffer); len(matches) != 0 {
		if len(matches[1]) == 0 {
			matches[1] = "html"
		}

		s.consume(len(matches[0]))

		return &token{tokDoctype, matches[1], nil}
	}

	return nil
}

func (s *scanner) scanComment() *token {
	if matches := rcomment.FindStringSubmatch(s.buffer); len(matches) != 0 {
		var (
			mode    string
			content = matches[2]
		)
		switch matches[1] {
		case "":
			s.readRaw = true

			mode = "code"

			// ie condition comment
			if len(matches[3]) != 0 {
				mode = "condition"
				content = matches[4]
			}
		case "!":
			mode = "html"
		}

		s.consume(len(matches[0]))

		return &token{tokComment, content, map[string]string{"Mode": mode, "Condition": matches[3]}}
	}

	return nil
}

func (s *scanner) scanCondition() *token {
	if matches := rif.FindStringSubmatch(s.buffer); len(matches) != 0 {
		s.consume(len(matches[0]))
		return &token{tokIf, matches[1], nil}
	}

	if matches := relsif.FindStringSubmatch(s.buffer); len(matches) != 0 {
		s.consume(len(matches[0]))
		return &token{tokElseIf, matches[1], nil}
	}

	if matches := relse.FindStringSubmatch(s.buffer); len(matches) != 0 {
		s.consume(len(matches[0]))
		return &token{tokElse, "", nil}
	}

	return nil
}

func (s *scanner) scanRange() *token {
	if matches := rrange.FindStringSubmatch(s.buffer); len(matches) != 0 {
		s.consume(len(matches[0]))
		return &token{tokRange, matches[3], map[string]string{"Key": matches[1], "Value": matches[2]}}
	}

	return nil
}

func (s *scanner) scanAssignment() *token {
	if matches := rassignment.FindStringSubmatch(s.buffer); len(matches) != 0 {
		s.consume(len(matches[0]))
		return &token{tokAssignment, matches[2], map[string]string{"Variable": matches[1]}}
	}

	return nil
}

func (s *scanner) scanId() *token {
	if matches := rid.FindStringSubmatch(s.buffer); len(matches) != 0 {
		s.consume(len(matches[0]))
		return &token{tokId, matches[1], map[string]string{"Condition": matches[2]}}
	}

	return nil
}

func (s *scanner) scanClass() *token {
	if matches := rclass.FindStringSubmatch(s.buffer); len(matches) != 0 {
		s.consume(len(matches[0]))
		return &token{tokClass, matches[1], map[string]string{"Condition": matches[2]}}
	}

	return nil
}

func (s *scanner) scanAttribute() *token {
	if matches := rattribute.FindStringSubmatch(s.buffer); len(matches) != 0 {
		s.consume(len(matches[0]))

		if len(matches[3]) != 0 || matches[2] == "" {
			return &token{tokAttribute, matches[1], map[string]string{"Content": matches[3], "Mode": "raw", "Condition": matches[5]}}
		}

		return &token{tokAttribute, matches[1], map[string]string{"Content": matches[4], "Mode": "expression", "Condition": matches[5]}}
	}

	return nil
}

func (s *scanner) scanImport() *token {
	if matches := rimport.FindStringSubmatch(s.buffer); len(matches) != 0 {
		s.consume(len(matches[0]))
		return &token{tokImport, matches[1], nil}
	}

	return nil
}

func (s *scanner) scanExtend() *token {
	if matches := rextend.FindStringSubmatch(s.buffer); len(matches) != 0 {
		s.consume(len(matches[0]))
		return &token{tokExtend, matches[1], nil}
	}

	return nil
}

func (s *scanner) scanBlock() *token {
	if matches := rblock.FindStringSubmatch(s.buffer); len(matches) != 0 {
		s.consume(len(matches[0]))
		return &token{tokNamedBlock, matches[2], map[string]string{"Modifier": matches[1]}}
	}

	return nil
}

func (s *scanner) scanTag() *token {
	if matches := rtag.FindStringSubmatch(s.buffer); len(matches) != 0 {
		s.consume(len(matches[0]))
		return &token{tokTag, matches[1], nil}
	}

	return nil
}

func (s *scanner) scanText() *token {
	if matches := rtext.FindStringSubmatch(s.buffer); len(matches) != 0 {
		s.consume(len(matches[0]))

		mode := "inline"
		if matches[1] == "|" {
			mode = "piped"
		}

		return &token{tokText, matches[2], map[string]string{"Mode": mode}}
	}

	return nil
}

func (s *scanner) readline() {
	if len(s.buffer) > 0 {
		return
	}

	buf, err := s.reader.ReadString('\n')
	if err != nil {
		if err != io.EOF {
			panic(err)
		}

		if len(buf) == 0 {
			s.state = scnEOF
		} else {
			s.state = scnNewLine
		}
	} else {
		s.state = scnNewLine
	}

	s.buffer = strings.TrimRightFunc(buf, unicode.IsSpace)
	s.line += 1
	s.column = 0
	return
}

func (s *scanner) consume(runes int) {
	if len(s.buffer) < runes {
		panic(fmt.Sprintf("Unable to consume %d runes from buffer `%s`.", runes, s.buffer))
	}

	s.lastTokenLine = s.line
	s.lastTokenColumn = s.column
	s.lastTokenSize = runes

	s.buffer = s.buffer[runes:]
	s.column += runes
}
