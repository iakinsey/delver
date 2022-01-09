package fsm

import (
	"io"
	"os"
)

var httpPrefixPattern = byte("h"[0])
var tagPrefixPattern = byte("<"[0])
var urlHintPattern = []byte("h<")
var nullPattern []byte
var aPattern = []byte("a")
var hrefPattern = []byte("href=")
var aTerminatePattern = []byte(">\"'")
var closeTagPattern = []byte(">")
var tagQuotesPattern = []byte("'\"")
var httpPattern = []byte("http")
var ttpPattern = []byte("ttp")
var httpsSuffixPattern = []byte("s")[0]
var finalHttpSuffixPattern = []byte(":")[0]
var doubleForwardSlashPattern = []byte("//")
var followsHttpPattern = []byte("s:")
var legalUrlChars = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~:/?#[]@!$%&()*+,;=")

type documentReaderFSM struct {
	result  []string
	next    func() error
	hasNext bool
	file    *os.File
}

func NewDocumentReaderFSM() FSMStates {
	var result []string

	return &documentReaderFSM{
		hasNext: true,
		result:  result,
	}
}

func (s *documentReaderFSM) Next() error {
	return s.next()
}

func (s *documentReaderFSM) HasNext() bool {
	return s.hasNext
}

func (s *documentReaderFSM) GetResult() []string {
	return s.result
}

func (s *documentReaderFSM) Init(f *os.File) {
	s.file = f
	s.next = s.readNewChar
}

func (s *documentReaderFSM) readNewChar() error {
	matchByte, err := ReadUntilMatchChars(s.file, urlHintPattern, nullPattern, true)

	if err == io.EOF {
		s.hasNext = false
		return nil
	}

	if rtn, err := s.check(err, matchByte != nil); rtn {
		return err
	}

	if *matchByte == httpPrefixPattern {
		s.next = s.readLink
	} else if *matchByte == tagPrefixPattern {
		s.next = s.readTag
	}

	return err
}

func (s *documentReaderFSM) check(err error, match bool) (bool, error) {
	shouldReturn := err != nil || !match

	if !match {
		s.next = s.readNewChar
	}

	if err != nil {
		s.hasNext = false
	}

	if err == io.EOF {
		err = nil
	}

	return shouldReturn, err
}

//////////////////////////////////////////////////////////////////////////////////////////
// Tag reading logic
//////////////////////////////////////////////////////////////////////////////////////////

func (s *documentReaderFSM) readTag() error {
	match, err := MatchNext(s.file, aPattern, true)

	if rtn, err := s.check(err, match); rtn {
		return err
	}

	if match {
		s.next = s.readATag
	}

	return nil
}

func (s *documentReaderFSM) readATag() error {
	match, err := ReadUntilMatch(s.file, hrefPattern, closeTagPattern, true)

	if rtn, err := s.check(err, match); rtn {
		return err
	}

	matchBytes, err := MatchNextOr(s.file, tagQuotesPattern, true)

	if rtn, err := s.check(err, matchBytes != nil); rtn {
		return err
	}

	url, err := GetUntil(s.file, aTerminatePattern)

	if rtn, err := s.check(err, matchBytes != nil); rtn {
		return err
	}

	s.result = append(s.result, string(url))
	s.next = s.readTag

	return nil
}

//////////////////////////////////////////////////////////////////////////////////////////
// Link reading logic
//////////////////////////////////////////////////////////////////////////////////////////

func (s *documentReaderFSM) readLink() error {
	match, err := MatchNext(s.file, ttpPattern, true)

	if rtn, err := s.check(err, match); rtn {
		return err
	} else if !match {
		s.next = s.readNewChar
	}

	nextChar, err := MatchNextOr(s.file, followsHttpPattern, true)

	if rtn, err := s.check(err, nextChar != nil); rtn {
		return err
	}

	data := []byte(httpPattern)

	if *nextChar == httpsSuffixPattern {
		data = append(data, httpsSuffixPattern)
		match, err := MatchNext(s.file, []byte{finalHttpSuffixPattern}, true)

		if rtn, err := s.check(err, match); rtn {
			return err
		}

		nextChar = &finalHttpSuffixPattern
	}

	if *nextChar != finalHttpSuffixPattern {
		return nil
	}

	data = append(data, finalHttpSuffixPattern)
	match, err = MatchNext(s.file, doubleForwardSlashPattern, true)

	if rtn, err := s.check(err, match); rtn {
		return err
	}

	data = append(data, doubleForwardSlashPattern...)

	result, err := GetUntilMismatch(s.file, legalUrlChars)
	match = len(result) > 0

	if match {
		data = append(data, result...)
		s.result = append(s.result, string(data))
	}

	_, err = s.check(err, match)

	return err
}
