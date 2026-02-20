package raft

import (
	"io"
	"log/slog"

	"github.com/hashicorp/raft"
)

type PipelineFSM struct{}

func (f *PipelineFSM) Apply(log *raft.Log) interface{} {
	slog.Info("FSM Apply",
		"index", log.Index,
		"term", log.Term,
		"type", log.Type.String(),
		"data_len", len(log.Data),
	)
	return nil
}

func (f *PipelineFSM) Snapshot() (raft.FSMSnapshot, error) {
	slog.Info("FSM Snapshot called")
	return &pipelineFSMSnapshot{}, nil
}

func (f *PipelineFSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	slog.Info("FSM Restore called")
	return nil
}

type pipelineFSMSnapshot struct{}

func (s *pipelineFSMSnapshot) Persist(sink raft.SnapshotSink) error {
	return sink.Close()
}

func (s *pipelineFSMSnapshot) Release() {}
