package logger

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/colinmarc/hdfs"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"

	"github.com/xitongsys/parquet-go-source/mem"
	"github.com/xitongsys/parquet-go/parquet"
	"github.com/xitongsys/parquet-go/writer"
)

type HDFSLogger interface {
}

type hdfsLogger struct {
	client *hdfs.Client
}

func NewHDFSLogger(namenode string) *hdfsLogger {
	client, err := hdfs.New(namenode)

	util.PanicIfErr(err, "failed to create hdfs client")

	return &hdfsLogger{
		client: client,
	}
}

func (s *hdfsLogger) LogResource(composite message.CompositeAnalysis) error {
	fw, err := mem.NewMemFileWriter(string(composite.RequestID), nil)

	if err != nil {
		return err
	}

	pw, err := writer.NewParquetWriter(fw, message.ParquetSchema, 4)

	if err != nil {
		return errors.Wrap(err, "failed to create parquet writer")
	}

	pw.CompressionType = parquet.CompressionCodec_SNAPPY

	if err = pw.Write(composite); err != nil {
		return errors.Wrap(err, "failed to write parquet file")
	}

	if err = pw.WriteStop(); err != nil {
		return errors.Wrap(err, "failed to stop parquet write")
	}

	if _, err := fw.Seek(0, io.SeekStart); err != nil {
		return errors.Wrap(err, "failed to seek beginning of parquet file")
	}

	partition := fmt.Sprintf("/resource/%s", time.Now().Format("2006-01-02"))
	path := fmt.Sprintf("%s/%s", partition, string(composite.RequestID))

	if err = s.client.MkdirAll(partition, os.ModeDir); err != nil {
		return errors.Wrapf(err, "failed to create hdfs partition %s", partition)
	}

	hw, err := s.client.Create(path)

	if err != nil {
		return errors.Wrapf(err, "failed to create hdfs file %s", path)
	}

	if n, err := io.Copy(hw, fw); err != nil {
		return errors.Wrapf(err, "failed to write parquet to hdfs %s", path)
	} else if n == 0 {
		return fmt.Errorf("no data written to hdfs %s", path)
	}

	if err := hw.Flush(); err != nil {
		return errors.Wrapf(err, "failed to flush hdfs file %s", path)
	}

	if err := hw.Close(); err != nil {
		return errors.Wrapf(err, "failed to close hdfs file %s", path)
	}

	return nil
}
