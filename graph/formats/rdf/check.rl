// Go code generated by go generate gonum.org/v1/gonum/graph/formats/rdf; DO NOT EDIT.

// Copyright ©2020 The Gonum Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdf

import (
	"fmt"
	"unicode"
)

%%{
	machine checkLabel;

	alphtype rune;

	include check "check_actions.rl";

	alphtype rune;

	PN_CHARS_BASE     = [A-Za-z]
	                  | 0x00c0 .. 0x00d6
	                  | 0x00d8 .. 0x00f6
	                  | 0x00f8 .. 0x02ff
	                  | 0x0370 .. 0x037d
	                  | 0x037f .. 0x1fff
	                  | 0x200c .. 0x200d
	                  | 0x2070 .. 0x218f
	                  | 0x2c00 .. 0x2fef
	                  | 0x3001 .. 0xd7ff
	                  | 0xf900 .. 0xfdcf
	                  | 0xfdf0 .. 0xfffd
	                  | 0x10000 .. 0xeffff
	                  ;

	PN_CHARS_U        = PN_CHARS_BASE | '_' | ':' ;

	PN_CHARS          = PN_CHARS_U
	                  | '-'
	                  | [0-9]
	                  | 0xb7
	                  | 0x0300 .. 0x036f
	                  | 0x203f .. 0x2040
	                  ;

	BLANK_NODE_LABEL  = (PN_CHARS_U | [0-9]) ((PN_CHARS | '.')* PN_CHARS)? ;

	value := BLANK_NODE_LABEL %Return @!Error ;

	write data;
}%%

func checkLabelText(data []rune) (err error) {
	var (
		cs, p int
		pe    = len(data)
		eof   = pe
	)

	%%write init;

	%%write exec;

	return ErrInvalidTerm
}

%%{
	machine checkLang;

	alphtype byte;

	include check "check_actions.rl";

	LANGTAG = '@' [a-zA-Z]+ ('-' [a-zA-Z0-9]+)* ;

	value := LANGTAG %Return @!Error ;

	write data;
}%%

func checkLangText(data []byte) (err error) {
	var (
		cs, p int
		pe    = len(data)
		eof   = pe
	)

	%%write init;

	%%write exec;

	return ErrInvalidTerm
}