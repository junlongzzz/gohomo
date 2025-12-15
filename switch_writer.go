package main

import (
	"io"
	"sync/atomic"
)

type SwitchWriter struct {
	enabled atomic.Bool
	writer  io.Writer
}

func NewSwitchWriter(w io.Writer, enabled bool) *SwitchWriter {
	sw := &SwitchWriter{
		writer: w,
	}
	sw.enabled.Store(enabled)
	return sw
}

func (s *SwitchWriter) Write(p []byte) (int, error) {
	if !s.enabled.Load() {
		// 不输出，但告诉调用方“已经写完了”，防止阻塞
		return len(p), nil
	}
	return s.writer.Write(p)
}

func (s *SwitchWriter) Switch(enabled bool) {
	s.enabled.Store(enabled)
}
