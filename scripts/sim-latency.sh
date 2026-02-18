#!/usr/bin/env bash
# scripts/sim-latency.sh
# Verify or update cross-cloud latency simulation at runtime.
#
# Usage:
#   bash scripts/sim-latency.sh status
#   bash scripts/sim-latency.sh test
#   bash scripts/sim-latency.sh set 75 10

set -e

COMMAND=${1:-status}

# Detect interfaces from inside the container
GCP_IFACE=$(docker exec gateway ip -o addr show | awk '/10\.20\./{print $2}')
AZURE_IFACE=$(docker exec gateway ip -o addr show | awk '/10\.30\./{print $2}')

case $COMMAND in

  status)
    echo "── tc rules on gateway ──────────────────────────"
    echo "GCP ($GCP_IFACE):"
    docker exec gateway tc qdisc show dev $GCP_IFACE
    echo "Azure ($AZURE_IFACE):"
    docker exec gateway tc qdisc show dev $AZURE_IFACE
    ;;

  test)
    echo "── Latency test ─────────────────────────────────"
    echo ""
    echo "▶ Intra-cloud (AWS→AWS): expect <1ms"
    docker exec gateway ping -c 4 10.10.0.1
    echo ""
    echo "▶ Cross-cloud (AWS→GCP): expect ~50ms"
    docker exec gateway ping -c 4 10.20.0.1
    echo ""
    echo "▶ Cross-cloud (AWS→Azure): expect ~75ms"
    docker exec gateway ping -c 4 10.30.0.1
    ;;

  set)
    NEW_LATENCY=${2:?'Usage: sim-latency.sh set <latency_ms> <jitter_ms>'}
    NEW_JITTER=${3:-5}
    echo "── Updating GCP latency to ${NEW_LATENCY}ms ± ${NEW_JITTER}ms ──"
    docker exec gateway tc qdisc change dev $GCP_IFACE root netem \
      delay "${NEW_LATENCY}ms" "${NEW_JITTER}ms" distribution normal
    echo "✅ Done. Run status to verify."
    ;;

  *)
    echo "Usage: sim-latency.sh [status|test|set <ms> <jitter>]"
    exit 1
    ;;

esac