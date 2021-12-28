package fsm

import (
	"io"
	"os"
)

type linkReaderFsm struct {
	result  []string
	begin   bool
	next    func() error
	hasNext bool
	file    *os.File
}

var httpPattern = []byte("http")
var ttpPattern = []byte("ttp")
var httpsSuffixPattern = []byte("s")[0]
var finalHttpSuffixPattern = []byte(":")[0]
var doubleForwardSlashPattern = []byte("//")
var followsHttpPattern = []byte("s:")
var legalUrlChars = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~:/?#[]@!$%&()*+,;=")

func NewLinkReaderFSM() FSMStates {
	var result []string

	return &linkReaderFsm{
		result: result,
		begin:  true,
	}
}

func (s *linkReaderFsm) Next() error {
	return s.next()
}

func (s *linkReaderFsm) HasNext() bool {
	return s.hasNext
}

func (s *linkReaderFsm) GetResult() []string {
	return s.result
}

func (s *linkReaderFsm) Init(f *os.File) {
	s.file = f
	s.next = s.readLink
}

func (s *linkReaderFsm) readLink() error {
	match, err := MatchNext(s.file, ttpPattern, true)

	if rtn, err := s.check(err, match); rtn {
		return err
	}

	nextChar, err := MatchNextOr(s.file, followsHttpPattern, true)

	if rtn, err := s.check(err, nextChar != nil); rtn {
		return err
	}

	data := []byte(httpPattern)

	if *nextChar == httpsSuffixPattern {
		data = append(data, httpsSuffixPattern)
		match, err := MatchNext(s.file, followsHttpPattern, true)

		if rtn, err := s.check(err, match); rtn {
			return err
		}
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

func (s *linkReaderFsm) check(err error, match bool) (bool, error) {
	shouldReturn := err != nil || !match

	if shouldReturn {
		s.hasNext = false
	}

	if err == io.EOF {
		err = nil
	}

	return shouldReturn, err

}

func (s *linkReaderFsm) checkError(err error) bool {
	if err == io.EOF {
		s.hasNext = false
	}

	return err != nil
}
