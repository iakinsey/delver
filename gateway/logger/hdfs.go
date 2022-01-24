package logger

import (
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/colinmarc/hdfs"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/types/persist"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
)

type HDFSLogger interface {
}

type hdfsLogger struct {
	client *hdfs.Client
}

type parquetToWrite struct {
	Mapper func(message.CompositeAnalysis) (io.Reader, error)
	Base   string
}

type parquetAndPath struct {
	Reader    io.Reader
	Path      string
	Partition string
}

var parquetsToWrite = []parquetToWrite{
	{persist.CompositeToResourceParquet, "resource"},
	{persist.CompositeToResourceFeaturesParquet, "resource_features"},
	{persist.CompositeToParquetURI, "uri"},
}

func NewHDFSLogger(namenode string) *hdfsLogger {
	client, err := hdfs.New(namenode)

	util.PanicIfErr(err, "failed to create hdfs client")

	return &hdfsLogger{
		client: client,
	}
}

func GetParquets(composite message.CompositeAnalysis) (res []parquetAndPath, err error) {
	for _, toWrite := range parquetsToWrite {
		r, err := toWrite.Mapper(composite)

		if err != nil {
			return nil, errors.Wrap(err, "failed to create parquet file")
		}

		partition := path.Join("/", toWrite.Base, time.Now().Format("2006-01-02"))
		path := path.Join(partition, string(composite.RequestID))
		res = append(res, parquetAndPath{
			Reader:    r,
			Path:      path,
			Partition: partition,
		})
	}

	return
}

func (s *hdfsLogger) LogResource(composite message.CompositeAnalysis) error {
	parquetsAndPaths, err := GetParquets(composite)

	if err != nil {
		return errors.Wrap(err, "failed to get parquets")
	}

	for _, meta := range parquetsAndPaths {
		if err = s.client.MkdirAll(meta.Partition, os.ModeDir); err != nil {
			return errors.Wrapf(err, "failed to create hdfs partition %s", meta.Partition)
		}

		hw, err := s.client.Create(meta.Path)

		if err != nil {
			return errors.Wrapf(err, "failed to create hdfs file %s", meta.Path)
		}

		if n, err := io.Copy(hw, meta.Reader); err != nil {
			return errors.Wrapf(err, "failed to write parquet to hdfs %s", meta.Path)
		} else if n == 0 {
			return fmt.Errorf("no data written to hdfs %s", meta.Path)
		}

		if err := hw.Flush(); err != nil {
			return errors.Wrapf(err, "failed to flush hdfs file %s", meta.Path)
		}

		if err := hw.Close(); err != nil {
			return errors.Wrapf(err, "failed to close hdfs file %s", meta.Path)
		}
	}

	return nil
}
