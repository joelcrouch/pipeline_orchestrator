package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RaftElectionsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "raft_elections_total",
		Help: "Total number of Raft leader elections observed (term increments).",
	})

	RaftTerm = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "raft_term",
		Help: "Current Raft term.",
	})

	RaftState = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "raft_state",
		Help: "Current Raft state: 0=Follower, 1=Candidate, 2=Leader, 3=Shutdown.",
	})

	// RaftReplicationLatencyMs tracks time from raft.Apply() to commit confirmation.
	RaftReplicationLatencyMs = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "raft_replication_latency_ms",
		Help:    "Milliseconds from raft.Apply() call to commit confirmation.",
		Buckets: prometheus.ExponentialBuckets(1, 2, 12), // 1ms → ~4096ms
	})
)
