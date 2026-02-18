# S0.1 — Simulated Multi-Cloud Docker Network
*5 points · Tags: Docker, Networking, Infra*

**As a** developer, **I want to** spin up isolated Docker networks that simulate AWS and GCP VPCs with realistic cross-cloud latency, **so that** I can develop and test distributed behavior locally without spending cloud credits.

### Acceptance Criteria
- [ ] `docker-compose up` creates two named bridge networks: `net-aws` and `net-gcp`
- [ ] A `tc-netem` sidecar or iptables rule imposes configurable latency (default 50ms RTT) on cross-network traffic
- [ ] Containers on `net-aws` cannot reach containers on `net-gcp` except through the gateway node
- [ ] Running `ping` from an AWS node to a GCP node shows ~50ms RTT; same-network ping is <1ms
- [ ] A single `make sim-up` command starts the full environment; `make sim-down` tears it down cleanly

### Technical Notes
- Use Docker bridge networks with `--subnet` for distinct CIDR ranges (e.g. `10.10.0.0/24` for AWS, `10.20.0.0/24` for GCP)
- `tc qdisc add dev eth0 root netem delay 50ms 5ms` can be run in a privileged init container
- Consider a gateway/router container attached to both networks as the cross-cloud hop
- Document the simulated topology in a README diagram so the team always knows which container = which cloud role



So for this job we need a couple files. ltes think about design:
new-aws:10.10.0.0./24 
net-gcp:10.20.0.0/24
net-azure:10.30.0.0/24  
We will need a gateway container attached to all three networks(networks + gateway for now, nodes get added later (s0.4)),and a privileged init container to apply tc-netem latency rules (be careful not to add shortere latency than rafte needs-thrahsing will occure)

Deliverables include docker-compose.yml skeleton.  here is what we will add:
```
docker/docker-compose.yml          ← docker-compose.yml
docker/gateway/Dockerfile          ← gateway-Dockerfile
docker/gateway/entrypoint.sh       ← gateway-entrypoint.sh
scripts/sim-latency.sh             ← sim-latency.sh
```