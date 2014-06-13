package slim

import (
	"bytes"
	"container/list"
	"errors"
	"fmt"
	goAst "go/ast"
	goParser "go/parser"
	goToken "go/token"
	"html/template"
	"io"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/golib/slim/parser"
)

var builtinFunctions = [...]string{
	"len",
	"print",
	"printf",
	"println",
	"urlquery",
	"js",
	"json",
	"index",
	"html",
	"unescaped",
}

// Compiler is the main interface of Slim Template Engine.
// In order to use an Slim template, it is required to create a Compiler and
// compile an Slim source to native Go template.
//	compiler := slim.New()
// 	// Parse the input file
//	err := compiler.ParseFile("./input.slim")
//	if err == nil {
//		// Compile input file to Go template
//		tpl, err := compiler.Compile()
//		if err == nil {
//			// Check built in html/template documentation for further details
//			tpl.Execute(os.Stdout, somedata)
//		}
//	}
type Compiler struct {
	// Compiler options
	Options
	filename     string
	node         parser.Noder
	indentLevel  int
	newline      bool
	buffer       *bytes.Buffer
	tempvarIndex int
}

// Create and initialize a new Compiler
func New() *Compiler {
	compiler := new(Compiler)
	compiler.filename = ""
	compiler.tempvarIndex = 0
	compiler.PrettyPrint = true
	compiler.Options = DefaultOptions

	return compiler
}

type Options struct {
	// Setting if pretty printing is enabled.
	// Pretty printing ensures that the output html is properly indented and in human readable form.
	// If disabled, produced HTML is compact. This might be more suitable in production environments.
	// Default: true
	PrettyPrint bool
	// Setting if line number emitting is enabled
	// In this form, Slim emits line number comments in the output template. It is usable in debugging environments.
	// Default: false
	LineNumbers bool
}

var DefaultOptions = Options{true, false}

// Parses and compiles the supplied slim template string. Returns corresponding Go Template (html/templates) instance.
// Necessary runtime functions will be injected and the template will be ready to be executed.
func Compile(input string, options Options) (*template.Template, error) {
	comp := New()
	comp.Options = options

	err := comp.Parse(input)
	if err != nil {
		return nil, err
	}

	return comp.Compile()
}

// Parses and compiles the contents of supplied filename. Returns corresponding Go Template (html/templates) instance.
// Necessary runtime functions will be injected and the template will be ready to be executed.
func CompileFile(filename string, options Options) (*template.Template, error) {
	comp := New()
	comp.Options = options

	err := comp.ParseFile(filename)
	if err != nil {
		return nil, err
	}

	return comp.Compile()
}

// Parse given raw slim template string.
func (c *Compiler) Parse(input string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New(r.(string))
		}
	}()

	parser, err := parser.NewStringParser(input)

	if err != nil {
		return
	}

	c.node = parser.Parse()
	return
}

// Parse the slim template file in given path
func (c *Compiler) ParseFile(filename string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New(r.(string))
		}
	}()

	parser, err := parser.NewFileParser(filename)

	if err != nil {
		return
	}

	c.node = parser.Parse()
	c.filename = filename
	return
}

// Compile slim and create a Go Template (html/templates) instance.
// Necessary runtime functions will be injected and the template will be ready to be executed.
func (c *Compiler) Compile() (*template.Template, error) {
	return c.CompileWithName(filepath.Base(c.filename))
}

// Same as Compile but allows to specify a name for the template
func (c *Compiler) CompileWithName(name string) (*template.Template, error) {
	return c.CompileWithTemplate(template.New(name))
}

// Same as Compile but allows to specify a template
func (c *Compiler) CompileWithTemplate(t *template.Template) (*template.Template, error) {
	data, err := c.CompileString()

	if err != nil {
		return nil, err
	}

	tpl, err := t.Funcs(funcMap).Parse(data)

	if err != nil {
		return nil, err
	}

	return tpl, nil
}

// Compile slim and write the Go Template source into given io.Writer instance
// You would not be using this unless debugging / checking the output. Please use Compile
// method to obtain a template instance directly.
func (c *Compiler) CompileWriter(out io.Writer) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New(r.(string))
		}
	}()

	c.buffer = new(bytes.Buffer)
	c.visit(c.node)

	if c.buffer.Len() > 0 {
		c.write("\n")
	}

	_, err = c.buffer.WriteTo(out)
	return
}

// Compile template and return the Go Template source
// You would not be using this unless debugging / checking the output. Please use Compile
// method to obtain a template instance directly.
func (c *Compiler) CompileString() (string, error) {
	var buf bytes.Buffer

	if err := c.CompileWriter(&buf); err != nil {
		return "", err
	}

	result := buf.String()

	return result, nil
}

func (c *Compiler) visit(node parser.Noder) {
	defer func() {
		if r := recover(); r != nil {
			if rs, ok := r.(string); ok && rs[:len("Slim Error")] == "Slim Error" {
				panic(r)
			}

			pos := node.Pos()

			if len(pos.Filename) > 0 {
				panic(fmt.Sprintf("Slim Error in <%s>: %v - Line: %d, Column: %d, Length: %d", pos.Filename, r, pos.Line, pos.Column, pos.TokenLength))
			} else {
				panic(fmt.Sprintf("Slim Error: %v - Line: %d, Column: %d, Length: %d", r, pos.Line, pos.Column, pos.TokenLength))
			}
		}
	}()

	switch node.(type) {
	case *parser.Block:
		c.visitBlock(node.(*parser.Block))
	case *parser.Doctype:
		c.visitDoctype(node.(*parser.Doctype))
	case *parser.Comment:
		c.visitComment(node.(*parser.Comment))
	case *parser.Tag:
		c.visitTag(node.(*parser.Tag))
	case *parser.Text:
		c.visitText(node.(*parser.Text))
	case *parser.Condition:
		c.visitCondition(node.(*parser.Condition))
	case *parser.Range:
		c.visitEach(node.(*parser.Range))
	case *parser.Assignment:
		c.visitAssignment(node.(*parser.Assignment))
	}
}

func (c *Compiler) write(value string) {
	c.buffer.WriteString(value)
}

func (c *Compiler) indent(offset int, newline bool) {
	if !c.PrettyPrint {
		return
	}

	if newline && c.buffer.Len() > 0 {
		c.write("\n")
	}

	for i := 0; i < c.indentLevel+offset; i++ {
		c.write("\t")
	}
}

func (c *Compiler) tempvar() string {
	c.tempvarIndex++
	return "$__slim_" + strconv.Itoa(c.tempvarIndex)
}

func (c *Compiler) escape(input string) string {
	return strings.Replace(strings.Replace(input, `\`, `\\`, -1), `"`, `\"`, -1)
}

func (c *Compiler) visitBlock(block *parser.Block) {
	for _, node := range block.Children {
		if _, ok := node.(*parser.Text); !block.CanInline() && ok {
			c.indent(0, true)
		}

		c.visit(node)
	}
}

func (c *Compiler) visitDoctype(doctype *parser.Doctype) {
	c.write(doctype.String())
}

func (c *Compiler) visitComment(comment *parser.Comment) {
	if comment.Silent {
		return
	}

	c.indent(0, false)

	if comment.Block == nil {
		c.write(`{{unescaped "<!-- ` + c.escape(comment.Value) + ` -->"}}`)
	} else {
		c.write(`<!-- ` + comment.Value)
		c.visitBlock(comment.Block)
		c.write(` -->`)
	}
}

func (c *Compiler) visitCondition(condition *parser.Condition) {
	c.write(`{{if ` + c.visitRawInterpolation(condition.Expression) + `}}`)
	c.visitBlock(condition.Positive)
	if condition.Negative != nil {
		c.write(`{{else}}`)
		c.visitBlock(condition.Negative)
	}
	c.write(`{{end}}`)
}

func (c *Compiler) visitEach(each *parser.Range) {
	if each.Block == nil {
		return
	}

	if len(each.Value) == 0 {
		c.write(`{{range ` + each.Key + ` := ` + c.visitRawInterpolation(each.Expression) + `}}`)
	} else {
		c.write(`{{range ` + each.Key + `, ` + each.Value + ` := ` + c.visitRawInterpolation(each.Expression) + `}}`)
	}
	c.visitBlock(each.Block)
	c.write(`{{end}}`)
}

func (c *Compiler) visitAssignment(assgn *parser.Assignment) {
	c.write(`{{` + assgn.Variable + ` := ` + c.visitRawInterpolation(assgn.Expression) + `}}`)
}

func (c *Compiler) visitTag(tag *parser.Tag) {
	type attrib struct {
		name      string
		value     string
		condition string
	}

	attribs := make(map[string]*attrib)

	for _, item := range tag.Attributes {
		attr := new(attrib)
		attr.name = item.Name

		if !item.IsRaw {
			attr.value = c.visitInterpolation(item.Value)
		} else if item.Value == "" {
			attr.value = ""
		} else {
			attr.value = `{{"` + item.Value + `"}}`
		}

		if len(item.Condition) != 0 {
			attr.condition = c.visitRawInterpolation(item.Condition)
		}

		if attr.name == "class" && attribs["class"] != nil {
			prevclass := attribs["class"]
			attr.value = ` ` + attr.value

			if len(attr.condition) > 0 {
				attr.value = `{{if ` + attr.condition + `}}` + attr.value + `{{end}}`
				attr.condition = ""
			}

			if len(prevclass.condition) > 0 {
				prevclass.value = `{{if ` + prevclass.condition + `}}` + prevclass.value + `{{end}}`
				prevclass.condition = ""
			}

			prevclass.value = prevclass.value + attr.value
		} else {
			attribs[item.Name] = attr
		}
	}

	c.indent(0, true)
	c.write("<" + tag.Name)

	for name, value := range attribs {
		if len(value.condition) > 0 {
			c.write(`{{if ` + value.condition + `}}`)
		}

		if value.value == "" {
			c.write(` ` + name)
		} else {
			c.write(` ` + name + `="` + value.value + `"`)
		}

		if len(value.condition) > 0 {
			c.write(`{{end}}`)
		}
	}

	if tag.IsAutoclose() {
		c.write(` />`)
	} else {
		c.write(`>`)

		if tag.Block != nil {
			if !tag.Block.CanInline() {
				c.indentLevel++
			}

			c.visitBlock(tag.Block)

			if !tag.Block.CanInline() {
				c.indentLevel--
				c.indent(0, true)
			}
		}

		c.write(`</` + tag.Name + `>`)
	}
}

var textInterpolateRegexp = regexp.MustCompile(`#\{(.*?)\}`)
var textEscapeRegexp = regexp.MustCompile(`\{\{(.*?)\}\}`)

func (c *Compiler) visitText(txt *parser.Text) {
	value := textEscapeRegexp.ReplaceAllStringFunc(txt.Value, func(value string) string {
		return `{{"{{"}}` + value[2:len(value)-2] + `{{"}}"}}`
	})

	value = textInterpolateRegexp.ReplaceAllStringFunc(value, func(value string) string {
		return c.visitInterpolation(value[2 : len(value)-1])
	})

	lines := strings.Split(value, "\n")
	for i := 0; i < len(lines); i++ {
		c.write(lines[i])

		if i < len(lines)-1 {
			c.write("\n")
			c.indent(0, false)
		}
	}
}

func (c *Compiler) visitInterpolation(value string) string {
	return `{{` + c.visitRawInterpolation(value) + `}}`
}

func (c *Compiler) visitRawInterpolation(value string) string {
	value = strings.Replace(value, "$", "__DOLLAR__", -1)
	expr, err := goParser.ParseExpr(value)
	if err != nil {
		panic("Unable to parse expression.")
	}
	value = strings.Replace(c.visitExpression(expr), "__DOLLAR__", "$", -1)
	return value
}

func (c *Compiler) visitExpression(outerexpr goAst.Expr) string {
	stack := list.New()

	pop := func() string {
		if stack.Front() == nil {
			return ""
		}

		val := stack.Front().Value.(string)
		stack.Remove(stack.Front())
		return val
	}

	var exec func(goAst.Expr)

	exec = func(expr goAst.Expr) {
		switch expr.(type) {
		case *goAst.BinaryExpr:
			{
				be := expr.(*goAst.BinaryExpr)

				exec(be.Y)
				exec(be.X)

				negate := false
				name := c.tempvar()
				c.write(`{{` + name + ` := `)

				switch be.Op {
				case goToken.ADD:
					c.write("__slim_add ")
				case goToken.SUB:
					c.write("__slim_sub ")
				case goToken.MUL:
					c.write("__slim_mul ")
				case goToken.QUO:
					c.write("__slim_quo ")
				case goToken.REM:
					c.write("__slim_rem ")
				case goToken.LAND:
					c.write("and ")
				case goToken.LOR:
					c.write("or ")
				case goToken.EQL:
					c.write("__slim_eql ")
				case goToken.NEQ:
					c.write("__slim_eql ")
					negate = true
				case goToken.LSS:
					c.write("__slim_lss ")
				case goToken.GTR:
					c.write("__slim_gtr ")
				case goToken.LEQ:
					c.write("__slim_gtr ")
					negate = true
				case goToken.GEQ:
					c.write("__slim_lss ")
					negate = true
				default:
					panic("Unexpected operator!")
				}

				c.write(pop() + ` ` + pop() + `}}`)

				if !negate {
					stack.PushFront(name)
				} else {
					negname := c.tempvar()
					c.write(`{{` + negname + ` := not ` + name + `}}`)
					stack.PushFront(negname)
				}
			}
		case *goAst.UnaryExpr:
			{
				ue := expr.(*goAst.UnaryExpr)

				exec(ue.X)

				name := c.tempvar()
				c.write(`{{` + name + ` := `)

				switch ue.Op {
				case goToken.SUB:
					c.write("__slim_minus ")
				case goToken.ADD:
					c.write("__slim_plus ")
				case goToken.NOT:
					c.write("not ")
				default:
					panic("Unexpected operator!")
				}

				c.write(pop() + `}}`)
				stack.PushFront(name)
			}
		case *goAst.ParenExpr:
			exec(expr.(*goAst.ParenExpr).X)
		case *goAst.BasicLit:
			stack.PushFront(expr.(*goAst.BasicLit).Value)
		case *goAst.Ident:
			name := expr.(*goAst.Ident).Name
			if len(name) >= len("__DOLLAR__") && name[:len("__DOLLAR__")] == "__DOLLAR__" {
				if name == "__DOLLAR__" {
					stack.PushFront(`.`)
				} else {
					stack.PushFront(`$` + expr.(*goAst.Ident).Name[len("__DOLLAR__"):])
				}
			} else {
				stack.PushFront(`.` + expr.(*goAst.Ident).Name)
			}
		case *goAst.SelectorExpr:
			se := expr.(*goAst.SelectorExpr)
			exec(se.X)
			x := pop()

			if x == "." {
				x = ""
			}

			name := c.tempvar()
			c.write(`{{` + name + ` := ` + x + `.` + se.Sel.Name + `}}`)
			stack.PushFront(name)
		case *goAst.CallExpr:
			ce := expr.(*goAst.CallExpr)

			for i := len(ce.Args) - 1; i >= 0; i-- {
				exec(ce.Args[i])
			}

			name := c.tempvar()
			builtin := false

			if ident, ok := ce.Fun.(*goAst.Ident); ok {
				for _, fname := range builtinFunctions {
					if fname == ident.Name {
						builtin = true
						break
					}
				}
			}

			if builtin {
				stack.PushFront(ce.Fun.(*goAst.Ident).Name)
				c.write(`{{` + name + ` := ` + pop())
			} else {
				exec(ce.Fun)
				c.write(`{{` + name + ` := call ` + pop())
			}

			for i := 0; i < len(ce.Args); i++ {
				c.write(` `)
				c.write(pop())
			}

			c.write(`}}`)

			stack.PushFront(name)
		default:
			panic("Unable to parse expression. Unsupported: " + reflect.TypeOf(expr).String())
		}
	}

	exec(outerexpr)
	return pop()
}
