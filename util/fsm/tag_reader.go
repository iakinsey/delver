package fsm

import (
	"io"
	"os"
)

var aPattern = []byte("a")
var hrefPattern = []byte("href=")
var aTerminatePattern = []byte(">\"'")
var closeTagPattern = []byte(">")
var tagQuotesPattern = []byte("'\"")

type tagReaderFsm struct {
	result  []string
	next    func() error
	hasNext bool
	file    *os.File
}

func NewTagReaderFSM() FSMStates {
	var result []string

	return &tagReaderFsm{
		hasNext: true,
		result:  result,
	}
}

func (s *tagReaderFsm) Next() error {
	return s.next()
}

func (s *tagReaderFsm) HasNext() bool {
	return s.hasNext
}

func (s *tagReaderFsm) GetResult() []string {
	return s.result
}

func (s *tagReaderFsm) Init(f *os.File) {
	s.file = f
	s.next = s.readTag
}

func (s *tagReaderFsm) readTag() error {
	match, err := MatchNext(s.file, aPattern, true)

	if s.checkError(err) {
		return err
	}

	if match {
		s.next = s.readATag
	} else {
		s.hasNext = false
	}

	return nil
}

func (s *tagReaderFsm) readATag() error {
	match, err := ReadUntilMatch(s.file, hrefPattern, closeTagPattern, true)

	if rtn, err := s.check(err, match); rtn {
		return err
	}

	matchBytes, err := MatchNextOr(s.file, tagQuotesPattern, true)

	if rtn, err := s.check(err, matchBytes != nil); rtn {
		return err
	}

	url, err := GetUntil(s.file, aTerminatePattern)

	if s.checkError(err) {
		return err
	}

	s.result = append(s.result, string(url))
	s.next = s.readTag

	return nil
}

func (s *tagReaderFsm) check(err error, match bool) (bool, error) {
	shouldReturn := err != nil || !match

	if shouldReturn {
		s.hasNext = false
	}

	if err == io.EOF {
		err = nil
	}

	return shouldReturn, err

}

func (s *tagReaderFsm) checkError(err error) bool {
	if err == io.EOF {
		s.hasNext = false
	}

	return err != nil
}
