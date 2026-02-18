#!/usr/bin/env bash
set -e

LATENCY_MS=${CROSS_CLOUD_LATENCY_MS:-50}
JITTER_MS=${CROSS_CLOUD_JITTER_MS:-5}
AZURE_LATENCY_MS=${AZURE_LATENCY_MS:-75}

echo "🌐 Gateway starting..."
echo "   AWS↔GCP latency  : ${LATENCY_MS}ms ± ${JITTER_MS}ms"
echo "   AWS↔Azure latency: ${AZURE_LATENCY_MS}ms ± ${JITTER_MS}ms"

# ── Detect interfaces by subnet (order shuffles on every boot) ────
AWS_IFACE=$(ip -o addr show | awk '/10\.10\./{print $2}')
GCP_IFACE=$(ip -o addr show | awk '/10\.20\./{print $2}')
AZURE_IFACE=$(ip -o addr show | awk '/10\.30\./{print $2}')

echo "   AWS   → $AWS_IFACE"
echo "   GCP   → $GCP_IFACE"
echo "   Azure → $AZURE_IFACE"

# ── Apply tc-netem on GCP interface ──────────────────────────────
tc qdisc del dev $GCP_IFACE root 2>/dev/null || true
tc qdisc add dev $GCP_IFACE root netem \
    delay "${LATENCY_MS}ms" "${JITTER_MS}ms" distribution normal
echo "✅ tc-netem applied on $GCP_IFACE (GCP): ${LATENCY_MS}ms ± ${JITTER_MS}ms"

# ── Apply tc-netem on Azure interface ────────────────────────────
tc qdisc del dev $AZURE_IFACE root 2>/dev/null || true
tc qdisc add dev $AZURE_IFACE root netem \
    delay "${AZURE_LATENCY_MS}ms" "${JITTER_MS}ms" distribution normal
echo "✅ tc-netem applied on $AZURE_IFACE (Azure): ${AZURE_LATENCY_MS}ms ± ${JITTER_MS}ms"

echo ""
echo "Gateway running. Routing cross-cloud traffic..."
exec sleep infinity