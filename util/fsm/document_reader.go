package fsm

import (
	"io"
	"os"
)

var httpPrefixPattern = byte("h"[0])
var tagPrefixPattern = byte("<"[0])
var urlHintPattern = []byte("h<")
var nullPattern []byte

type documentReaderFsm struct {
	file       *os.File
	linkReader FSMStates
	tagReader  FSMStates
}

func NewDocumentReaderFSM() FSMStates {
	return &documentReaderFsm{
		linkReader: NewLinkReaderFSM(),
		tagReader:  NewTagReaderFSM(),
	}
}

func (s *documentReaderFsm) HasNext() bool {
	return s.linkReader.HasNext() && s.tagReader.HasNext()
}

func (s *documentReaderFsm) GetResult() []string {
	return append(s.linkReader.GetResult(), s.tagReader.GetResult()...)
}

func (s *documentReaderFsm) Init(f *os.File) {
	s.file = f

	s.linkReader.Init(f)
	s.tagReader.Init(f)
}

func (s *documentReaderFsm) Next() error {
	matchByte, err := ReadUntilMatchChars(s.file, urlHintPattern, nullPattern, true)

	if rtn, err := s.check(err, matchByte != nil); rtn {
		return err
	}

	if *matchByte == httpPrefixPattern {
		err = s.linkReader.Next()
	} else if *matchByte == tagPrefixPattern {
		err = s.tagReader.Next()
	}

	return err
}

func (s *documentReaderFsm) check(err error, match bool) (bool, error) {
	shouldReturn := err != nil || !match

	if err == io.EOF {
		err = nil
	}

	return shouldReturn, err
}
