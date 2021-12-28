package fsm

import (
	"io"
	"os"
)

/**
Read from the buffer until the specified string is matched.

Returns `true` if string is found.
Returns `false` if string is not found or term_chars are matched.

If rewind is set to `true`, set the buffer's cursor to its initial
value when this function was first called.
*/
func ReadUntilMatch(f *os.File, toMatch []byte, termChars []byte, rewind bool) (bool, error) {
	index := 0
	char := toMatch[0]
	startPos, err := f.Seek(0, io.SeekCurrent)

	if err != nil {
		return false, err
	}

	for {
		data := make([]byte, 1)
		_, err := io.ReadFull(f, data)

		if err == io.EOF {
			if rewind {
				if _, err := f.Seek(startPos, io.SeekStart); err != nil {
					return false, err
				}
			}
			return false, err
		}

		if err != nil {
			return false, err
		} else if data[0] == char {
			index += 1

			if index == len(toMatch) {
				return true, nil
			}
			char = toMatch[index]
		} else if stringInSlice(char, termChars) {
			if rewind {
				if _, err := f.Seek(startPos, io.SeekStart); err != nil {
					return false, err
				}
			}

			return false, nil
		} else {
			index = 0
			char = toMatch[0]
		}
	}
}

func ReadUntilMatchChars(f *os.File, chars []byte, termChars []byte, rewind bool) (*byte, error) {
	startPos, err := f.Seek(0, io.SeekCurrent)

	if err != nil {
		return nil, err
	}

	for {
		data := make([]byte, 1)
		_, err := io.ReadFull(f, data)

		if err == io.EOF {
			if rewind {
				if _, err := f.Seek(startPos, io.SeekStart); err != nil {
					return nil, err
				}
			}
			return nil, err
		}

		if err != nil {
			return &data[0], err
		} else if stringInSlice(data[0], termChars) {
			if rewind {
				if _, err := f.Seek(startPos, io.SeekStart); err != nil {
					return nil, err
				}
			}
			return nil, nil
		} else if stringInSlice(data[0], chars) {
			return nil, nil
		}
	}
}

func MatchNextOr(f *os.File, chars []byte, rewind bool) (*byte, error) {
	startPos, err := f.Seek(0, io.SeekCurrent)

	if err != nil {
		return nil, err
	}

	data := make([]byte, 1)

	if _, err := io.ReadFull(f, data); err != nil {
		return nil, err
	}

	if stringInSlice(data[0], chars) {
		return &data[0], nil
	}

	if rewind {
		if _, err := f.Seek(startPos, io.SeekStart); err != nil {
			return nil, err
		}
	}

	return nil, nil
}

func MatchNext(f *os.File, chars []byte, rewind bool) (bool, error) {
	startPos, err := f.Seek(0, io.SeekCurrent)

	if err != nil {
		return false, err
	}

	for _, char := range chars {
		data := make([]byte, 1)
		_, err := io.ReadFull(f, data)

		if err != nil {
			return false, err
		} else if char != data[0] {
			if rewind {
				if _, err := f.Seek(startPos, io.SeekStart); err != nil {
					return false, err
				}
			}

			return false, nil
		}
	}

	return true, nil
}

func GetUntil(f *os.File, termChars []byte) (result []byte, err error) {
	for {
		data := make([]byte, 1)
		_, err = io.ReadFull(f, data)

		if err != nil || stringInSlice(data[0], termChars) {
			return result, err
		}

		result = append(result, data[0])
	}
}

func GetUntilMismatch(f *os.File, legalChars []byte) (result []byte, err error) {
	for {
		data := make([]byte, 1)
		_, err = io.ReadFull(f, data)

		if err != nil || !stringInSlice(data[0], legalChars) {
			return result, err
		}

		result = append(result, data[0])
	}
}

func stringInSlice(a byte, list []byte) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}
