# overview.
Im starting fresh because the last few times i have started this project, i have discorerd a litany of errors, causing drastic archtecture changes and i have also forgotten where i was and my notes/docs werent cutting it.  so here we are

We will be using conda because it will solve our gpu env for us (hopefully),adn we will use an environment.yml which will include pip installs. Hopefully we can avoid a standalone requirements.txt

So i took my old spirnt plan, and made two new sprint0 and sprint1 detailed sprint plans, with user stories and some semblance of assinging points to each task.  I will revisit the points after i completete the sprint. 
So onto the work.  

First i made a structure.  I have init-structure.sh i use for alot of repo making and i amended some details for this project.  So now we have the project strucutre.
## S0.1 — Simulated Multi-Cloud Docker Network
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
So quick aside we will be usin tc-netem to emutlate real traffic.  see 'man tc-netem' for details.
I also added in an 'azure' simulation as well as gcp.  The infra is 'defined' in docker/docker-compsose.yml. Essentailly ther is a gateway that sits on the three subnets(net-aws, net-gcp,net-azure), and this is where we can configure the different behaviors of the networs(latency, rtt, etc).
There is also a fairly vanilla gateway Dockerfile and a small smoke test script to ensure the networks are working the expected way (entrypoint.sh)

The make file ahs been created and we can make sim-up and make sim-down and run the littel experiment.  That pretty much hits the reqs. from s0.1.

### terminal output s01
```
make sim-up
docker compose -f docker/docker-compose.yml up --build -d
[+] Building 1.3s (12/12) FINISHED                                                                               
 => [internal] load local bake definitions                                                                  0.0s
 => => reading from stdin 588B                                                                              0.0s
 => [internal] load build definition from Dockerfile                                                        0.0s
 => => transferring dockerfile: 261B                                                                        0.0s
 => [internal] load metadata for docker.io/library/alpine:3.19                                              0.8s
 => [auth] library/alpine:pull token for registry-1.docker.io                                               0.0s
 => [internal] load .dockerignore                                                                           0.0s
 => => transferring context: 2B                                                                             0.0s
 => [1/4] FROM docker.io/library/alpine:3.19@sha256:6baf43584bcb78f2e5847d1de515f23499913ac9f12bdf834811a3  0.0s
 => [internal] load build context                                                                           0.0s
 => => transferring context: 35B                                                                            0.0s
 => CACHED [2/4] RUN apk add --no-cache iproute2 iputils bash                                               0.0s
 => CACHED [3/4] COPY entrypoint.sh /entrypoint.sh                                                          0.0s
 => CACHED [4/4] RUN chmod +x /entrypoint.sh                                                                0.0s
 => exporting to image                                                                                      0.0s
 => => exporting layers                                                                                     0.0s
 => => writing image sha256:1574250c6f57c806f8aaf3054701e3fadc7290dae39019895b4f857d120462b8                0.0s
 => => naming to docker.io/library/docker-gateway                                                           0.0s
 => resolving provenance for metadata file                                                                  0.0s
[+] up 2/3
 ✔ Image docker-gateway   Built                                                                              1.4s
 ✔ Network docker_net-aws Created                                                                            0.1s
[+] up 5/5 docker_net-gcp Creating                                                                           0.0s
 ✔ Image docker-gateway     Built                                                                            1.4s ✔ Network docker_net-aws   Created                                                                          0.1s ✔ Network docker_net-gcp   Created                                                                          0.1s
 ✔ Network docker_net-azure Created                                                                          0.1s
 ✔ Container gateway        Created                                                                          0.1s
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ docker logs gateway
🌐 Gateway starting...
   AWS↔GCP latency  : 50ms ± 5ms
   AWS↔Azure latency: 75ms ± 5ms
   AWS   → eth2
   GCP   → eth1
   Azure → eth0
✅ tc-netem applied on eth1 (GCP): 50ms ± 5ms
✅ tc-netem applied on eth0 (Azure): 75ms ± 5ms

Gateway running. Routing cross-cloud traffic...
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ bash scripts/sim-latency.sh test
── Latency test ─────────────────────────────────

▶ Intra-cloud (AWS→AWS): expect <1ms
PING 10.10.0.1 (10.10.0.1) 56(84) bytes of data.
64 bytes from 10.10.0.1: icmp_seq=1 ttl=64 time=0.095 ms
64 bytes from 10.10.0.1: icmp_seq=2 ttl=64 time=0.070 ms
64 bytes from 10.10.0.1: icmp_seq=3 ttl=64 time=0.117 ms
64 bytes from 10.10.0.1: icmp_seq=4 ttl=64 time=0.066 ms

--- 10.10.0.1 ping statistics ---
4 packets transmitted, 4 received, 0% packet loss, time 3095ms
rtt min/avg/max/mdev = 0.066/0.087/0.117/0.020 ms

▶ Cross-cloud (AWS→GCP): expect ~50ms
PING 10.20.0.1 (10.20.0.1) 56(84) bytes of data.
64 bytes from 10.20.0.1: icmp_seq=1 ttl=64 time=96.1 ms
64 bytes from 10.20.0.1: icmp_seq=2 ttl=64 time=48.6 ms
64 bytes from 10.20.0.1: icmp_seq=3 ttl=64 time=48.4 ms
64 bytes from 10.20.0.1: icmp_seq=4 ttl=64 time=51.7 ms

--- 10.20.0.1 ping statistics ---
4 packets transmitted, 4 received, 0% packet loss, time 3003ms
rtt min/avg/max/mdev = 48.413/61.215/96.124/20.196 ms

▶ Cross-cloud (AWS→Azure): expect ~75ms
PING 10.30.0.1 (10.30.0.1) 56(84) bytes of data.
64 bytes from 10.30.0.1: icmp_seq=1 ttl=64 time=154 ms
64 bytes from 10.30.0.1: icmp_seq=2 ttl=64 time=70.2 ms
64 bytes from 10.30.0.1: icmp_seq=3 ttl=64 time=72.7 ms
64 bytes from 10.30.0.1: icmp_seq=4 ttl=64 time=83.6 ms

--- 10.30.0.1 ping statistics ---
4 packets transmitted, 4 received, 0% packet loss, time 3002ms
rtt min/avg/max/mdev = 70.232/95.210/154.301/34.485 ms
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ docker ps
CONTAINER ID   IMAGE                COMMAND                  CREATED              STATUS                        PORTS                                         NAMES
646ebf49becf   docker-gateway       "/entrypoint.sh"         About a minute ago   Up About a minute (healthy)                                                 gateway
185a85508cae   postgres:15-alpine   "docker-entrypoint.s…"   10 days ago          Up 36 hours                   0.0.0.0:5433->5432/tcp, [::]:5433->5432/tcp   ml_eval_postgres
f0da256e8dc7   postgres:16          "docker-entrypoint.s…"   4 weeks ago          Up 36 hours                   0.0.0.0:6432->5432/tcp, [::]:6432->5432/tcp   pythoncrud-db-1
9c2b558fed96   postgres:15          "docker-entrypoint.s…"   6 weeks ago          Up 36 hours                                                                 agentic-postgres
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ make sim-down
docker compose -f docker/docker-compose.yml down -v
[+] down 4/4
 ✔ Container gateway        Removed                                                                         10.4s
 ✔ Network docker_net-gcp   Removed                                                                         0.3s
 ✔ Network docker_net-aws   Removed                                                                         0.2s
 ✔ Network docker_net-azure Removed                                                                         0.5s
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ docker ps
CONTAINER ID   IMAGE                COMMAND                  CREATED       STATUS        PORTS                                         NAMES
185a85508cae   postgres:15-alpine   "docker-entrypoint.s…"   10 days ago   Up 36 hours   0.0.0.0:5433->5432/tcp, [::]:5433->5432/tcp   ml_eval_postgres
f0da256e8dc7   postgres:16          "docker-entrypoint.s…"   4 weeks ago   Up 36 hours   0.0.0.0:6432->5432/tcp, [::]:6432->5432/tcp   pythoncrud-db-1
9c2b558fed96   postgres:15          "docker-entrypoint.s…"   6 weeks ago   Up 36 hours                                                 agentic-postgres


```


## s0.2 — Go Control Plane Skeleton & Build Pipeline  **DONE**
*3 points · Tags: Go, Infra*

**As a** developer, **I want to** scaffold the Go module for the control plane with a working build, lint, and test pipeline, **so that** every contributor can run `go build`, `go test ./...` and get a green baseline from day one.

### Acceptance Criteria
- [ ] `go build ./...` compiles without errors from a fresh clone
- [ ] `go test ./...` passes with at least one placeholder test per package
- [ ] `golangci-lint run` returns zero issues on the scaffold
- [ ] A Dockerfile builds the control plane binary into a minimal `scratch` or `distroless` image under 50MB
- [ ] `make build`, `make test`, and `make lint` targets all work from the repo root

### Technical Notes
- Use Go 1.22+ with modules. Suggested package layout: `cmd/orchestrator`, `internal/raft`, `internal/agent`, `internal/storage`
- Use `slog` (stdlib) for structured logging from the start — retrofitting logging is painful
- Wire up a minimal gRPC server in `cmd/orchestrator/main.go` so the container has a real entrypoint
- Pin `golangci-lint` version in a `.golangci.yml` to avoid CI drift

S0.2 is where we will start injecting go code inot the control plane.  We will be frogging around in some manner with evry go fil in control-plane.  Primarily we will be doing these:
```
control-plane/cmd/orchestrator/main.go        ← main.go
control-plane/cmd/orchestrator/main_test.go   ← main_test.go
control-plane/internal/raft/node.go           ← node.go (replace empty file)
control-plane/internal/raft/raft_test.go      ← raft_test.go (replace empty file)
control-plane/internal/agent/registry.go      ← registry.go (replace empty file)
control-plane/Dockerfile                      ← control-plane-Dockerfile
control-plane/.golangci.yml                   ← .golangci.yml
```

The rest of the go files, we will be adding 'package <correct_package> at the top of each file so it compiles.  

The meat and potatoes is in main.go. It's the entrypoint of the entire control plane — the main() function that runs when the container starts. Think of it as the wiring layer that connects everything together. Right now it does four things:
1. Sets up structured logging using Go's built-in slog package, outputting JSON so logs are machine-readable and easy to ship to a log aggregator later.
2. Starts a gRPC server on port 50051 — this is where Raft RPCs (RequestVote, AppendEntries) and worker RPCs (RegisterWorker, Heartbeat) will be registered as we build sprint1 .. Right now it only has the health check service registered, which is enough for Docker to know the container is alive.
3. Starts an HTTP debug server on port 8080 with two endpoints:

/health — returns {"status":"ok"}, used by Docker healthcheck and operators
/cluster-state — will eventually return the full list of workers and their status, currently returns an empty list

4. Handles graceful shutdown — listens for SIGTERM (what docker stop sends) and cleanly drains in-flight requests before exiting, rather than just killing the process mid-request.
As we build Sprint 1, main.go stays mostly the same — just add lines to register new gRPC services on the existing server, like:
```
// S1.2 — register Raft service
raftpb.RegisterRaftServer(grpcServer, raft.NewNode(nodeID))

// S1.4 — register Worker service  
workerpb.RegisterWorkerServer(grpcServer, agent.NewRegistry())
```
It is legitametly thin.  The real logic lies n intenal/. There is a simple test in the same directoyr-how go likes to do stuff.

golangci.yml is a config file for golangci-lint, a go linter that ahs many static analysis tools. Its kinda nice for making sure your code is correct.

Most of this stuff is just place holder code, that we will change after we really bring in the raft library, and actually make it work.



To get it to work:
```
cd control-plane
go get google.golang.org/grpc
go get google.golang.org/grpc/health
go mod tidy
cd ..
make build
make test
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.59.0    <<<---- DOwnlaod whatever is best for the go version you are using>>>
make lint
```

### terminal output
```
make build
cd control-plane && go build ./...
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ cd control-plane 
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator/control-plane$ go build ./...
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator/control-plane$ cd ..
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ make tests
make: Nothing to be done for 'tests'.
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ make test
cd control-plane && go test ./...
ok  	github.com/joelcrouch/pipeline-orchestrator/control-plane/cmd/orchestrator	(cached)
ok  	github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/agent	(cached) [no tests to run]
?   	github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/metrics	[no test files]
ok  	github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/raft	(cached)
?   	github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/scheduler	[no test files]
?   	github.com/joelcrouch/pipeline-orchestrator/control-plane/internal/storage	[no test files]
cd worker && python -m pytest
============================================== test session starts ==============================================
platform linux -- Python 3.11.8, pytest-8.4.2, pluggy-1.5.0
rootdir: /home/dell-linux-dev3/Projects/pipeline_orchestrator/worker
plugins: asyncio-1.2.0, Faker-20.0.3, anyio-4.2.0
asyncio: mode=Mode.STRICT, debug=False, asyncio_default_fixture_loop_scope=None, asyncio_default_test_loop_scope=function
collected 0 items                                                                                               

============================================= no tests ran in 0.01s =============================================
make: *** [Makefile:14: test] Error 5
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ make lint
cd control-plane && golangci-lint run
0 issues.
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ docker build -t cp-test control-plane/
[+] Building 1.2s (15/15) FINISHED                                                                docker:default
 => [internal] load build definition from Dockerfile                                                        0.0s
 => => transferring dockerfile: 330B                                                                        0.0s
 => [internal] load metadata for gcr.io/distroless/static-debian12:latest                                   0.4s
 => [internal] load metadata for docker.io/library/golang:1.25-alpine                                       0.9s
 => [auth] library/golang:pull token for registry-1.docker.io                                               0.0s
 => [internal] load .dockerignore                                                                           0.0s
 => => transferring context: 2B                                                                             0.0s
 => [stage-1 1/2] FROM gcr.io/distroless/static-debian12:latest@sha256:20bc6c0bc4d625a22a8fde3e55f6515709b  0.0s
 => [builder 1/6] FROM docker.io/library/golang:1.25-alpine@sha256:f6751d823c26342f9506c03797d2527668d095b  0.0s
 => [internal] load build context                                                                           0.0s
 => => transferring context: 929B                                                                           0.0s
 => CACHED [builder 2/6] WORKDIR /app                                                                       0.0s
 => CACHED [builder 3/6] COPY go.mod go.sum ./                                                              0.0s
 => CACHED [builder 4/6] RUN go mod download                                                                0.0s
 => CACHED [builder 5/6] COPY . .                                                                           0.0s
 => CACHED [builder 6/6] RUN CGO_ENABLED=0 GOOS=linux go build -o /orchestrator ./cmd/orchestrator          0.0s
 => CACHED [stage-1 2/2] COPY --from=builder /orchestrator /orchestrator                                    0.0s
 => exporting to image                                                                                      0.1s
 => => exporting layers                                                                                     0.0s
 => => writing image sha256:1531f84e677d586713ef554a8feaf699d7797fffd43083f630b80961ce95dc06                0.1s
 => => naming to docker.io/library/cp-test                                                                  0.0s
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ docker image inspect cp-test --format='{{.Size}}' | awk '{printf "%.1f MB\n", $1/1024/1024}'
18.9 MB

```

## s0.3 
here is where we will need our conda to be behaving properly.

make and create the env
```

```
if you added any changes do this:
```

```
UH-oh.  I knew this day would come.  I have not been exactly perfect using conda environments, and my 'base' environment is polluted.  I dont even really want  a base environment. Guess i should have taken that option when i installed conda.  But anyways, I ran into the circle of DOOM:
### Conda inconsistencies:
```
onda env create -f worker/environment.yml
/home/dell-linux-dev3/anaconda3/lib/python3.11/site-packages/conda/base/context.py:201: FutureWarning: Adding 'defaults' to channel list implicitly is deprecated and will be removed in 25.3. 

To remove this warning, please choose a default channel explicitly with conda's regular configuration system, e.g. by adding 'defaults' to the list of channels:

  conda config --add channels defaults

For more information see https://docs.conda.io/projects/conda/en/stable/user-guide/configuration/use-condarc.html

  deprecated.topic(
Collecting package metadata (repodata.json): | WARNING conda.models.version:get_matcher(563): Using .* with relational operator is superfluous and deprecated and will be removed in a future version of conda. Your spec was 1.9.0.*, but conda is ignoring the .* and treating it as 1.9.0
WARNING conda.models.version:get_matcher(563): Using .* with relational operator is superfluous and deprecated and will be removed in a future version of conda. Your spec was 1.8.0.*, but conda is ignoring the .* and treating it as 1.8.0
WARNING conda.models.version:get_matcher(563): Using .* with relational operator is superfluous and deprecated and will be removed in a future version of conda. Your spec was 1.6.0.*, but conda is ignoring the .* and treating it as 1.6.0
WARNING conda.models.version:get_matcher(563): Using .* with relational operator is superfluous and deprecated and will be removed in a future version of conda. Your spec was 1.7.1.*, but conda is ignoring the .* and treating it as 1.7.1
done
Solving environment: \ Killed

```

Best guesses: 1)OOM 2) conda inconsistencies => an environment that is impossible to solve, leading to a circular (closed-loop) of instructions from conda.

Also this:
```
onda install -n base -c conda-forge mamba
/home/dell-linux-dev3/anaconda3/lib/python3.11/site-packages/conda/base/context.py:201: FutureWarning: Adding 'defaults' to channel list implicitly is deprecated and will be removed in 25.3. 

To remove this warning, please choose a default channel explicitly with conda's regular configuration system, e.g. by adding 'defaults' to the list of channels:

  conda config --add channels defaults

For more information see https://docs.conda.io/projects/conda/en/stable/user-guide/configuration/use-condarc.html

  deprecated.topic(
Collecting package metadata (current_repodata.json): | WARNING conda.models.version:get_matcher(563): Using .* with relational operator is superfluous and deprecated and will be removed in a future version of conda. Your spec was 1.7.1.*, but conda is ignoring the .* and treating it as 1.7.1
done
Solving environment: / 
The environment is inconsistent, please check the package plan carefully
The following packages are causing the inconsistency:

  - defaults/linux-64::conda-repo-cli==1.0.75=py311h06a4308_0
  - defaults/linux-64::mypy==1.8.0=py311h5eee18b_0
  - defaults/linux-64::anaconda-client==1.12.3=py311h06a4308_0
  - defaults/linux-64::pydantic==1.10.12=py311h5eee18b_1
  - defaults/noarch::conda-token==0.4.0=pyhd3eb1b0_0
  - defaults/linux-64::anaconda-navigator==2.5.2=py311h06a4308_0
  - defaults/noarch::aioitertools==0.7.1=pyhd3eb1b0_0
  - defaults/linux-64::spyder==5.4.3=py311h06a4308_1
  - defaults/linux-64::ipykernel==6.28.0=py311h06a4308_0
  - defaults/linux-64::conda-build==24.1.2=py311h06a4308_0
  - defaults/linux-64::scrapy==2.8.0=py311h06a4308_0
  - defaults/linux-64::notebook==7.0.8=py311h06a4308_0
  - defaults/linux-64::widgetsnbextension==3.5.2=py311h06a4308_1

  ...
   defaults/linux-64::streamlit==1.30.0=py311h06a4308_0
  - defaults/linux-64::jupyterlab==4.0.11=py311h06a4308_0
  - defaults/linux-64::jupyter_console==6.6.3=py311h06a4308_0
  - defaults/linux-64::_anaconda_depends==2024.02=py311_mkl_1
  - defaults/noarch::conda-index==0.4.0=pyhd3eb1b0_0
  - defaults/noarch::ipywidgets==7.6.5=pyhd3eb1b0_2
  - defaults/linux-64::jupyter==1.0.0=py311h06a4308_9
  - defaults/linux-64::navigator-updater==0.4.0=py311h06a4308_1
  - defaults/linux-64::holoviews==1.18.3=py311h06a4308_0
  - defaults/linux-64::jupyter_server==2.10.0=py311h06a4308_0
  - defaults/linux-64::anaconda-catalogs==0.2.0=py311h06a4308_0
  - defaults/linux-64::nbclient==0.8.0=py311h06a4308_0
  - defaults/linux-64::bokeh==3.3.4=py311h92b7b1e_0
  - defaults/linux-64::anaconda-project==0.11.1=py311h06a4308_0
  - defaults/linux-64::intake==0.6.8=py311h06a4308_0
  - defaults/linux-64::arrow==1.2.3=py311h06a4308_1
  - defaults/linux-64::jupyterlab-variableinspector==3.1.0=py311h06a4308_0
  - defaults/linux-64::statsmodels==0.14.0=py311hf4808d0_0
  - defaults/linux-64::notebook-shim==0.2.3=py311h06a4308_0
  - defaults/linux-64::spyder-kernels==2.4.4=py311h06a4308_0
  - defaults/linux-64::twisted==23.10.0=py311h06a4308_0
/ unsuccessful initial attempt using frozen solve. Retrying with flexible solve.

CondaError: KeyboardInterrupt

```
I killed it cause it wasn't moving the needle.  So what to do?
I need a conda environmnet, dont want to absolutely blow everything up, uninstall conda, reinstall conda, blah blah blah. Maybe tonight.  

Ill just use micromamba:
```
curl -Ls https://micro.mamba.pm/api/micromamba/linux-64/latest | tar -xvj bin/micromamba

./bin/micromamba create -f worker/environment.yml
```
micromamba activate pipeline-worker  maybe?
OK. That is going to take a while.  Go drink some coffee.

OK. Lets go over what we did:
Install micromamba:
``` curl -Ls https://micro.mamba.pm/api/micromamba/linux-64/latest | tar -xvj bin/micromamba
bin/micromamba
```
Create the environment:
```./bin/micromamba create -f worker/environment.yml
```
Ran into some issues:

```
micromamba activate pipeline-worker
micromamba: command not found
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ conda  activate pipeline-worker

EnvironmentNameNotFound: Could not find conda environment: pipeline-worker
You can list all discoverable environments with `conda info --envs`.


(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ ./bin/micromamba env list 
  Name             Active  Path                                                          
───────────────────────────────────────────────────────────────────────────────────────────
  base                     /home/dell-linux-dev3/.local/share/mamba                      
  pipeline-worker          /home/dell-linux-dev3/.local/share/mamba/envs/pipeline-worker 
                   *       /home/dell-linux-dev3/anaconda3                               
                           /home/dell-linux-dev3/anaconda3/envs/agentic-cyber-defens     
                           /home/dell-linux-dev3/anaconda3/envs/agentic-cyber-defense    
                           /home/dell-linux-dev3/anaconda3/envs/dt-worker                
                           /home/dell-linux-dev3/anaconda3/envs/epic_env                 
                           /home/dell-linux-dev3/anaconda3/envs/eraas-env                
                           /home/dell-linux-dev3/anaconda3/envs/guitar-tab               
                           /home/dell-linux-dev3/anaconda3/envs/ml-eval-framework        
                           /home/dell-linux-dev3/anaconda3/envs/mlbook                   
                           /home/dell-linux-dev3/anaconda3/envs/multicloudtest           
                           /home/dell-linux-dev3/anaconda3/envs/ocean-health             
                           /home/dell-linux-dev3/anaconda3/envs/pytorch                  
                           /home/dell-linux-dev3/anaconda3/envs/raft-env                 
                           /home/dell-linux-dev3/anaconda3/envs/realtime-social-analytics
                           /home/dell-linux-dev3/anaconda3/envs/recsys-spark             
                           /home/dell-linux-dev3/anaconda3/envs/scale-ai-mvp             
                           /home/dell-linux-dev3/anaconda3/envs/tf-gpu                   
                           /home/dell-linux-dev3/anaconda3/envs/torchoptim               
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ ./bin/micromamba shell init -s bash -p ~/.local/share/mamba
The following argument was not expected: -p
Run with --help for more information.
(base) dell-linux-dev3@dell-linux-dev3-Precision-3591:~/Projects/pipeline_orchestrator$ ./bin/micromamba shell init -s bash --root-prefix ~/.local/share/mamba
Running `shell init`, which:
 - modifies RC file: "/home/dell-linux-dev3/.bashrc"
 - generates config for root prefix: "/home/dell-linux-dev3/.local/share/mamba"
 - sets mamba executable to: "/home/dell-linux-dev3/Projects/pipeline_orchestrator/bin/micromamba"
The following has been added in your "/home/dell-linux-dev3/.bashrc" file

# >>> mamba initialize >>>
# !! Contents within this block are managed by 'micromamba shell init' !!
export MAMBA_EXE='/home/dell-linux-dev3/Projects/pipeline_orchestrator/bin/micromamba';
export MAMBA_ROOT_PREFIX='/home/dell-linux-dev3/.local/share/mamba';
__mamba_setup="$("$MAMBA_EXE" shell hook --shell bash --root-prefix "$MAMBA_ROOT_PREFIX" 2> /dev/null)"
if [ $? -eq 0 ]; then
    eval "$__mamba_setup"
else
    alias micromamba="$MAMBA_EXE"  # Fallback on help from micromamba activate
fi
unset __mamba_setup
# <<< mamba initialize <<<

```
Those were resolved.  See above.
Restart the terminal/apply the change
```
source ~/.bashrc
```
Finally activate it:
```
micromamba activate pipeline-worker
```

Ok that was a bit of a sidequest, but I think we got to where we need to be.

Back to our fastapi code.

Ok. After writing the code into the correct files, fixing some typos i managed this:
```
cd worker
python -m pytest
```
All tests pass. There are some warnings, and I will (unlikely) fix those later.  Whats a little tech debt?

