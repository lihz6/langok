package main

import (
	"fmt"
	"log"
	"os"
	"text/scanner"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <filename>", os.Args[0])
	}
	file, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	scan := new(scanner.Scanner).Init(file)
	scan.Mode ^= scanner.SkipComments
	scan.Filename = os.Args[1]
	for tok := scan.Scan(); tok != scanner.EOF; tok = scan.Scan() {
		fmt.Println(scanner.TokenString(tok), scan.TokenText(), scan.Position)
	}
}

func tokenizer(filename string) func() (tok rune, token string, position scanner.Position) {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	scan := new(scanner.Scanner).Init(file)
	scan.Mode ^= scanner.SkipComments
	scan.Filename = filename
	return func() (tok rune, token string, position scanner.Position) {
		position = scan.Position
		token = scan.TokenText()
		tok = scan.Scan()
		return
	}
}
