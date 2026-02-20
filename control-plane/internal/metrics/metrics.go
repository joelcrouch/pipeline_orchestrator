package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// RaftElectionsTotal counts leader elections (term increments).
	RaftElectionsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "raft_elections_total",
		Help: "Total number of Raft leader elections observed (term increments).",
	})

	// RaftTerm tracks the current Raft term.
	RaftTerm = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "raft_term",
		Help: "Current Raft term.",
	})

	// RaftState tracks the current Raft role: 0=Follower 1=Candidate 2=Leader 3=Shutdown.
	RaftState = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "raft_state",
		Help: "Current Raft state: 0=Follower, 1=Candidate, 2=Leader, 3=Shutdown.",
	})
)
