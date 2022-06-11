package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"text/scanner"
	"unicode"
	"unicode/utf8"
)

// ----------------------------------------------------------------------------
// Error handling

type errorList []error

func (list errorList) Err() error {
	if len(list) == 0 {
		return nil
	}
	return list
}

func (list errorList) Error() string {
	switch len(list) {
	case 0:
		return "no errors"
	case 1:
		return list[0].Error()
	}
	return fmt.Sprintf("%s (and %d more errors)", list[0], len(list)-1)
}

func newError(pos scanner.Position, msg string) error {
	return errors.New(fmt.Sprintf("%s: %s", pos, msg))
}

// ----------------------------------------------------------------------------
// Internal representation

type (
	// An Expression node represents a production expression.
	Expression interface {
		// Pos is the position of the first character of the syntactic construct
		Pos() scanner.Position
	}

	// An Alternative node represents a non-empty list of alternative expressions.
	Alternative []Expression // x | y | z

	// A Sequence node represents a non-empty list of sequential expressions.
	Sequence []Expression // x y z

	// A Name node represents a production name.
	Name struct {
		StringPos scanner.Position
		String    string
	}

	// A Token node represents a literal.
	Token struct {
		StringPos scanner.Position
		String    string
	}

	// A List node represents a range of characters.
	Range struct {
		Begin, End *Token // begin ... end
	}

	// A Group node represents a grouped expression.
	Group struct {
		Lparen scanner.Position
		Body   Expression // (body)
	}

	// An Option node represents an optional expression.
	Option struct {
		Lbrack scanner.Position
		Body   Expression // [body]
	}

	// A Repetition node represents a repeated expression.
	Repetition struct {
		Lbrace scanner.Position
		Body   Expression // {body}
	}

	// A Production node represents an EBNF production.
	Production struct {
		Name *Name
		Expr Expression
	}

	// A Bad node stands for pieces of source code that lead to a parse error.
	Bad struct {
		TokPos scanner.Position
		Error  string // parser error message
	}

	// A Grammar is a set of EBNF productions. The map
	// is indexed by production name.
	//
	Grammar map[string]*Production
)

func (x Alternative) Pos() scanner.Position { return x[0].Pos() } // the parser always generates non-empty Alternative
func (x Sequence) Pos() scanner.Position    { return x[0].Pos() } // the parser always generates non-empty Sequences
func (x *Name) Pos() scanner.Position       { return x.StringPos }
func (x *Token) Pos() scanner.Position      { return x.StringPos }
func (x *Range) Pos() scanner.Position      { return x.Begin.Pos() }
func (x *Group) Pos() scanner.Position      { return x.Lparen }
func (x *Option) Pos() scanner.Position     { return x.Lbrack }
func (x *Repetition) Pos() scanner.Position { return x.Lbrace }
func (x *Production) Pos() scanner.Position { return x.Name.Pos() }
func (x *Bad) Pos() scanner.Position        { return x.TokPos }

// ----------------------------------------------------------------------------
// Grammar verification

func isLexical(name string) bool {
	ch, _ := utf8.DecodeRuneInString(name)
	return !unicode.IsUpper(ch)
}

type parser struct {
	errors  errorList
	scanner scanner.Scanner
	pos     scanner.Position // token position
	tok     rune             // one token look-ahead
	lit     string           // token literal
}

func (p *parser) next() {
	p.tok = p.scanner.Scan()
	p.pos = p.scanner.Position
	p.lit = p.scanner.TokenText()
}

func (p *parser) error(pos scanner.Position, msg string) {
	p.errors = append(p.errors, newError(pos, msg))
}

func (p *parser) errorExpected(pos scanner.Position, msg string) {
	msg = `expected "` + msg + `"`
	if pos.Offset == p.pos.Offset {
		// the error happened at the current position;
		// make the error message more specific
		msg += ", found " + scanner.TokenString(p.tok)
		if p.tok < 0 {
			msg += " " + p.lit
		}
	}
	p.error(pos, msg)
}

func (p *parser) expect(tok rune) scanner.Position {
	pos := p.pos
	if p.tok != tok {
		p.errorExpected(pos, scanner.TokenString(tok))
	}
	p.next() // make progress in any case
	return pos
}

func (p *parser) parseIdentifier() *Name {
	pos := p.pos
	name := p.lit
	p.expect(scanner.Ident)
	return &Name{pos, name}
}

func (p *parser) parseToken() *Token {
	pos := p.pos
	value := ""
	if p.tok == scanner.String || p.tok == scanner.RawString {
		value, _ = strconv.Unquote(p.lit)
		// Unquote may fail with an error, but only if the scanner found
		// an illegal string in the first place. In this case the error
		// has already been reported.
		p.next()
	} else {
		p.expect(scanner.String)
	}
	return &Token{pos, value}
}

// ParseTerm returns nil if no term was found.
func (p *parser) parseTerm() (x Expression) {
	pos := p.pos

	switch p.tok {
	case scanner.Ident:
		x = p.parseIdentifier()

	case scanner.String, scanner.RawString:
		tok := p.parseToken()
		x = tok
		const ellipsis = '…' // U+2026, the horizontal ellipsis character
		if p.tok == ellipsis {
			p.next()
			x = &Range{tok, p.parseToken()}
		}

	case '(':
		p.next()
		x = &Group{pos, p.parseExpression()}
		p.expect(')')

	case '[':
		p.next()
		x = &Option{pos, p.parseExpression()}
		p.expect(']')

	case '{':
		p.next()
		x = &Repetition{pos, p.parseExpression()}
		p.expect('}')
	case scanner.Comment:
		fmt.Println("comment")
	}

	return x
}

func (p *parser) parseSequence() Expression {
	var list Sequence

	for x := p.parseTerm(); x != nil; x = p.parseTerm() {
		list = append(list, x)
	}

	// no need for a sequence if list.Len() < 2
	switch len(list) {
	case 0:
		p.errorExpected(p.pos, "term")
		return &Bad{p.pos, "term expected"}
	case 1:
		return list[0]
	}

	return list
}

func (p *parser) parseExpression() Expression {
	var list Alternative

	for {
		list = append(list, p.parseSequence())
		if p.tok != '|' {
			break
		}
		p.next()
	}
	// len(list) > 0

	// no need for an Alternative node if list.Len() < 2
	if len(list) == 1 {
		return list[0]
	}

	return list
}

func (p *parser) parseProduction() *Production {
	name := p.parseIdentifier()
	p.expect('=')
	var expr Expression
	if p.tok != '.' {
		expr = p.parseExpression()
	}
	p.expect('.')
	return &Production{name, expr}
}

func (p *parser) parse(filename string, src io.Reader) Grammar {
	p.scanner.Init(src)
	p.scanner.Filename = filename
	p.next() // initializes pos, tok, lit

	grammar := make(Grammar)
	for p.tok != scanner.EOF {
		prod := p.parseProduction()
		name := prod.Name.String
		if _, found := grammar[name]; !found {
			grammar[name] = prod
		} else {
			p.error(prod.Pos(), name+" declared already")
		}
	}

	return grammar
}

// Parse parses a set of EBNF productions from source src.
// It returns a set of productions. Errors are reported
// for incorrect syntax and if a production is declared
// more than once; the filename is used only for error
// positions.
func Parse(filename string, src io.Reader) (Grammar, error) {
	var p parser
	grammar := p.parse(filename, src)
	return grammar, p.errors.Err()
}

type verifier struct {
	errors   errorList
	worklist []*Production
	reached  Grammar // set of productions reached from (and including) the root production
	grammar  Grammar
}

func (v *verifier) error(pos scanner.Position, msg string) {
	v.errors = append(v.errors, newError(pos, msg))
}

func (v *verifier) push(prod *Production) {
	name := prod.Name.String
	if _, found := v.reached[name]; !found {
		v.worklist = append(v.worklist, prod)
		v.reached[name] = prod
	}
}

func (v *verifier) verifyChar(x *Token) rune {
	s := x.String
	if utf8.RuneCountInString(s) != 1 {
		v.error(x.Pos(), "single char expected, found "+s)
		return 0
	}
	ch, _ := utf8.DecodeRuneInString(s)
	return ch
}

func (v *verifier) verifyExpr(expr Expression, lexical bool) {
	switch x := expr.(type) {
	case nil:
		// empty expression
	case Alternative:
		for _, e := range x {
			v.verifyExpr(e, lexical)
		}
	case Sequence:
		for _, e := range x {
			v.verifyExpr(e, lexical)
		}
	case *Name:
		// a production with this name must exist;
		// add it to the worklist if not yet processed
		if prod, found := v.grammar[x.String]; found {
			v.push(prod)
		} else {
			v.error(x.Pos(), "missing production "+x.String)
		}
		// within a lexical production references
		// to non-lexical productions are invalid
		if lexical && !isLexical(x.String) {
			v.error(x.Pos(), "reference to non-lexical production "+x.String)
		}
	case *Token:
		// nothing to do for now
	case *Range:
		i := v.verifyChar(x.Begin)
		j := v.verifyChar(x.End)
		if i >= j {
			v.error(x.Pos(), "decreasing character range")
		}
	case *Group:
		v.verifyExpr(x.Body, lexical)
	case *Option:
		v.verifyExpr(x.Body, lexical)
	case *Repetition:
		v.verifyExpr(x.Body, lexical)
	case *Bad:
		v.error(x.Pos(), x.Error)
	default:
		panic(fmt.Sprintf("internal error: unexpected type %T", expr))
	}
}

func (v *verifier) verify(grammar Grammar, start string) {
	// find root production
	root, found := grammar[start]
	if !found {
		var noPos scanner.Position
		v.error(noPos, "no start production "+start)
		return
	}

	// initialize verifier
	v.worklist = v.worklist[0:0]
	v.reached = make(Grammar)
	v.grammar = grammar

	// work through the worklist
	v.push(root)
	for {
		n := len(v.worklist) - 1
		if n < 0 {
			break
		}
		prod := v.worklist[n]
		v.worklist = v.worklist[0:n]
		v.verifyExpr(prod.Expr, isLexical(prod.Name.String))
	}

	// check if all productions were reached
	if len(v.reached) < len(v.grammar) {
		for name, prod := range v.grammar {
			if _, found := v.reached[name]; !found {
				v.error(prod.Pos(), name+" is unreachable")
			}
		}
	}
}

// Verify checks that:
//   - all productions used are defined
//   - all productions defined are used when beginning at start
//   - lexical productions refer only to other lexical productions
//
// Position information is interpreted relative to the file set fset.
func Verify(grammar Grammar, start string) error {
	var v verifier
	v.verify(grammar, start)
	return v.errors.Err()
}

func main() {
	file, err := os.Open("go.ebnf")
	if err != nil {
		fmt.Println(err)
		return
	}
	gram, err := Parse("go.ebnf", file)
	if err != nil {
		fmt.Println(err)
		return
	}
	if err = Verify(gram, "SourceFile"); err != nil {
		fmt.Println(err)
		return
	}
}
