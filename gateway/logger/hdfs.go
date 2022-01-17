package logger

import (
	"github.com/colinmarc/hdfs"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
)

type HDFSLogger interface {
}

type hdfsLogger struct {
	client *hdfs.Client
}

func NewHDFSLogger(namenode string) Logger {
	client, err := hdfs.New(namenode)

	util.PanicIfErr(err, "failed to create hdfs client")

	return &hdfsLogger{
		client: client,
	}
}

func (s *hdfsLogger) LogResource(composite message.CompositeAnalysis) error {
	return nil
}
