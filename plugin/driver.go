package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/containerd/fifo"
	"github.com/docker/docker/api/types/plugins/logdriver"
	"github.com/docker/docker/daemon/logger"
	protoio "github.com/gogo/protobuf/io"
	"github.com/pkg/errors"
	"io"
	"io/fs"
	"os"
	"sync"
	"syscall"
)

type driver struct {
	mu   sync.Mutex
	logs map[string]*dockerInput
}

type dockerInput struct {
	stream io.ReadCloser
	info   logger.Info
}

func newDriver() *driver {
	return &driver{
		logs: make(map[string]*dockerInput),
	}
}

func (d *driver) StartLogging(file string, logCtx logger.Info) error {
	fmt.Fprintf(os.Stdout, "StartLogging file: %s\n", file)
	d.mu.Lock()
	if _, exists := d.logs[file]; exists {
		d.mu.Unlock()
		return fmt.Errorf("logger for %q already exists", file)
	}
	d.mu.Unlock()

	f, err := fifo.OpenFifo(context.Background(), file, syscall.O_RDONLY, 0700)
	if err != nil {
		return errors.Wrapf(err, "error opening logger fifo: %q", file)
	}

	d.mu.Lock()
	lf := &dockerInput{f, logCtx}
	d.logs[file] = lf
	d.mu.Unlock()
	d.PrintState()
	go consumeLog(lf)
	return nil
}

func (d *driver) PrintState() {
	fmt.Fprintln(os.Stdout, "New Container added for logging : >")
	for k, v := range d.logs {
		fmt.Fprintf(os.Stdout, " %s = %s\n", k, v.info.ContainerID)
	}
}

func (d *driver) StopLogging(file string) error {
	fmt.Fprintf(os.Stdout, "Stop logging: %s\n", file)
	d.mu.Lock()
	lf, ok := d.logs[file]
	if ok {
		lf.stream.Close()
		delete(d.logs, file)
	}
	d.mu.Unlock()
	return nil
}

func consumeLog(lf *dockerInput) {
	dec := protoio.NewUint32DelimitedReader(lf.stream, binary.BigEndian, 1e6)
	defer dec.Close()
	var buf logdriver.LogEntry
	for {
		if err := dec.ReadMsg(&buf); err != nil {
			var s *fs.PathError

			if errors.As(err, &s) {
				fmt.Fprintf(os.Stdout, "Stream closed  %s\n", err.Error())
				lf.stream.Close()
				return
			}
			dec = protoio.NewUint32DelimitedReader(lf.stream, binary.BigEndian, 1e6)
		}

		//write message to stdout
		fmt.Fprintln(os.Stdout, fmt.Sprintf("%s: [%s] [%d] %s", lf.info.ContainerID, buf.Source, buf.TimeNano, buf.Line))
		buf.Reset()
	}
}

func (d *driver) ReadLogs(info logger.Info, config logger.ReadConfig) (io.ReadCloser, error) {
	return nil, nil
}
