package util

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net/url"
	"os"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pkg/errors"
	"github.com/xitongsys/parquet-go-source/mem"
	"github.com/xitongsys/parquet-go/parquet"
	"github.com/xitongsys/parquet-go/writer"
)

func DedupeStrSlice(slice []string) (deduped []string) {
	if len(slice) == 0 {
		return deduped
	}

	keys := make(map[string]bool)

	for _, entity := range slice {
		if _, value := keys[entity]; !value {
			keys[entity] = true
			deduped = append(deduped, entity)
		}
	}
	return deduped
}

func HasSuffixes(str string, elems []string) bool {
	for _, elem := range elems {
		if strings.HasSuffix(str, elem) {
			return true
		}
	}

	return false
}

func ContainsAny(str string, substrs []string) bool {
	for _, substr := range substrs {
		if strings.Contains(str, substr) {
			return true
		}
	}

	return false
}

func ReadLines(file *os.File) (lines []string, err error) {
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func ResolveUrls(base *url.URL, urls []string) (result []string) {
	for _, rawUrl := range urls {
		u, err := url.Parse(rawUrl)

		if err != nil {
			continue
		}

		result = append(result, base.ResolveReference(u).String())
	}

	return
}

func GetSLD(host string) string {
	tokens := strings.Split(host, ".")

	if len(tokens) == 1 {
		return host
	}

	return tokens[len(tokens)-2]
}

func GetSLDAndTLD(host string) string {
	tokens := strings.Split(host, ".")
	size := len(tokens)

	if size == 1 {
		return host
	}

	return fmt.Sprintf("%s.%s", tokens[size-2], tokens[size-1])
}

func StringInSlice(a string, l []string) bool {
	for _, b := range l {
		if a == b {
			return true
		}
	}

	return false
}

func ByteArrayInSlice(a []byte, l [][]byte) bool {
	for _, b := range l {
		if bytes.Equal(a, b) {
			return true
		}
	}

	return false
}

func PanicIfErr(err error, msg string) {
	if err != nil {
		log.Panic(errors.Wrap(err, msg))
	}
}

func ToParquet(id string, schema string, msg interface{}) (io.Reader, error) {
	fw, err := mem.NewMemFileWriter(id, nil)

	if err != nil {
		return nil, errors.Wrap(err, "failed to create parquet mem writer")
	}

	pw, err := writer.NewParquetWriter(fw, schema, 4)

	if err != nil {
		return nil, errors.Wrap(err, "failed to create parquet writer")
	}

	pw.CompressionType = parquet.CompressionCodec_SNAPPY

	if err = pw.Write(msg); err != nil {
		return nil, errors.Wrap(err, "failed to write parquet file")
	}

	if err = pw.WriteStop(); err != nil {
		return nil, errors.Wrap(err, "failed to stop parquet write")
	}

	if _, err := fw.Seek(0, io.SeekStart); err != nil {
		return nil, errors.Wrap(err, "failed to seek beginning of parquet file")
	}

	return fw, nil
}

func CountDecimals(v float64) int {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	i := strings.IndexByte(s, '.')

	if i > -1 {
		return len(s) - i - 1
	}

	return 0
}

func RandomString(n int) string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	s := make([]rune, n)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}
