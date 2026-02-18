# High-Performance Data Pipeline Orchestrator (Refactored Consensus Core)

## Project Overview & Technical Specification

🎯 **What This System Actually Does**
The Data Pipeline Orchestrator is a **research-grade distributed control plane** that coordinates the movement and processing of large-scale ML training data across multiple compute nodes in a multi-cloud environment. It acts as the **fault-tolerant mission control** for critical ML workloads.

**Real-World Problem It Solves:**

- Guarantees **zero data loss** and **ultra-fast recovery** (MTTR < 90s) across cloud boundaries despite node failures.
- Conducts empirical analysis on **cross-cloud network impact** on consensus protocols (Raft) and data movement.
- Optimizes data flow to achieve **10+ GB/s sustained throughput** for massive training jobs.

---

## 📋 Functional Requirements (Refined)

### Core Data Movement & Processing

- **MapReduce Core:** Specialized processing engine for parallel Map (compute) and synchronous Reduce (aggregation) tasks.
- **Durable State Persistence:** All intermediate and final data is written immediately to globally accessible, durable cloud storage (S3/GCS).
- **Data Integrity:** Compute verifiable checksums on all processed data and final output.

### System Management (Consensus-Driven)

- **Fault-Tolerant Coordination:** Orchestrator Control Plane maintains state using a **Raft-like consensus algorithm**.
- **Adaptive Failover:** Implement and measure **Warm Standby** (CPU-only VM pre-provisioned) failover to minimize RTO while managing GPU cost.
- **Cost-RTO Analysis:** Empirically measure the Recovery Time Objective (RTO) against the operational cost of different failover strategies.

---

## 🚀 Sprint Plan - 4 Sprints (8 weeks)

### Sprint 1: Foundation & Fault-Tolerant Control Plane (Week 1-2)

**Goal:** Implement the Raft-like Distributed Consensus model for the Orchestrator Control Plane.

| Story                                   | Description                                                                                                   | Acceptance Criteria                                                                            | Effort   |
| :-------------------------------------- | :------------------------------------------------------------------------------------------------------------ | :--------------------------------------------------------------------------------------------- | :------- |
| **Story 1.1: Raft State & Log**         | Implement the core Raft data structures: Log, Term, and State (Follower, Candidate, Leader).                  | Raft log can be replicated across 3 nodes.                                                     | 8 points |
| **Story 1.2: Consensus Protocol Core**  | Implement Leader Election and Log Replication mechanics. Messages must traverse AWS-GCP boundary.             | Leader is elected within 15 seconds after planned failure; Log is consistent across all peers. | 8 points |
| **Story 1.3: Node Agent Communication** | Implement Agent heartbeat and status update protocol using the Raft Leader as the centralized point of truth. | Nodes can send status updates, and the Leader's view of the cluster state is consistent.       | 5 points |

**Sprint 1 Deliverables:**

- Working cluster of 6+ nodes with health monitoring.
- **Fault-Tolerant Orchestrator Control Plane** running a Raft-like consensus.
- Proven ability to elect a new Leader and maintain state consistency across clouds.

---

### Sprint 2: High-Throughput MapReduce Core (Week 3-4)

**Goal:** Implement the specialized MapReduce core and validate high-throughput execution by successfully passing the Distributed Matrix Multiplication test.

| Story                                            | Description                                                                                                                                                        | Acceptance Criteria                                                                               | Effort    |
| :----------------------------------------------- | :----------------------------------------------------------------------------------------------------------------------------------------------------------------- | :------------------------------------------------------------------------------------------------ | :-------- |
| **Story 2.1: Matrix Chunking & Manifest**        | Implement logic to slice $\mathbf{A}, \mathbf{B}$ matrices and generate the $\mathbf{Map\text{-}Task\text{-}Manifest}$.                                            | Can generate verifiable Map tasks for a $\mathbf{>100\text{GB}}$ matrix dataset.                  | 8 points  |
| **Story 2.2: Map Task Core (Computation)**       | Implement worker logic to execute the Map task ($\mathbf{A}_{i,j} \times \mathbf{B}_{j,k}$) and reliably push partial results to durable storage (S3/GCS).         | Workers achieve high utilization and partial results are verifiable by URI.                       | 8 points  |
| **Story 2.3: Shuffle/Reduce Core (Aggregation)** | Implement the Orchestration logic to chain Map $\to$ Reduce. Implement the Reduce worker to pull **cross-cloud** partial products and perform the final summation. | Final product matrix $\mathbf{C}$ is $\mathbf{100\%}$ numerically accurate (checksum validation). | 13 points |

**Sprint 2 Deliverables:**

- Complete **MapReduce Core** operational across AWS and GCP nodes.
- **Pass/Fail:** Successful, $\mathbf{100\%}$ accurate computation of the $\mathbf{>100\text{GB}}$ Distributed Matrix Multiplication.
- Established **Throughput Baseline (RQ1)** for normal operations.

---

### Sprint 3: Adaptive Failover & Research Analysis (Week 5-6)

**Goal:** Implement production-grade fault tolerance, dynamic optimization, and conduct the core empirical analysis (RTO vs. Cost).

| Story                                                           | Description                                                                                                                                                                | Acceptance Criteria                                                                                                           | Effort    |
| :-------------------------------------------------------------- | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | :---------------------------------------------------------------------------------------------------------------------------- | :-------- |
| **Story 3.1: Adaptive Failover System**                         | Implement **Warm Standby** (CPU-only VM pool) and the logic to quickly **attach the GPU/Accelerator** upon detected failure.                                               | System successfully recovers from single node failure by re-scheduling to a Warm Standby VM.                                  | 13 points |
| **Story 3.2: RTO vs. Cost Trade-Off Analysis ($\mathbf{RQ4}$)** | Implement metrics to measure and compare RTO for **Cold Start** vs. **Warm Standby** failover strategies.                                                                  | Empirical data quantifies the cost difference and RTO improvement (e.g., RTO $\mathbf{<90\text{ seconds}}$ for Warm Standby). | 8 points  |
| **Story 3.3: Data Consistency & Recovery**                      | Implement distributed **checkpointing** of the training state (Model Weights/Optimizer State) and the ability to resume work from the last checkpoint with zero data loss. | Zero data loss guarantee verified during failover; Checkpoint recovery time is measured.                                      | 8 points  |

**Sprint 3 Deliverables:**

- **Fault-Tolerant System** validated against single node failures using the Matrix and CNN/Protein workloads.
- **Empirical Analysis Report** detailing the cost-vs-RTO trade-offs (RQ4).
- **Data Consistency Guarantees** verified with checkpointing and recovery.

---

### Sprint 4: Observability & Production Features (Week 7-8)

**Goal:** Complete monitoring, debugging, operational tools, and finalize the Raft latency analysis (RQ3) using the LLM workload.

| Story                                                     | Description                                                                                                                                                   | Acceptance Criteria                                                                                               | Effort   |
| :-------------------------------------------------------- | :------------------------------------------------------------------------------------------------------------------------------------------------------------ | :---------------------------------------------------------------------------------------------------------------- | :------- |
| **Story 4.1: Metrics & Monitoring Dashboard**             | Real-time performance metrics collection (CPU, I/O, Network Latency). Web-based dashboard for system visualization.                                           | Sub-second metric updates; Critical $\mathbf{RTO}$ and $\mathbf{Throughput}$ metrics visualized in Grafana/React. | 8 points |
| **Story 4.2: Raft Latency Analysis ($\mathbf{RQ3}$)**     | Test LLM/Chatbot training across clouds. Measure how $\mathbf{cross\text{-}cloud\text{-}latency}$ impacts the Raft consensus time and overall training speed. | Empirical data on the impact of network jitter/latency on Leader election and job completion.                     | 8 points |
| **Story 4.3: Centralized Logging & Operational Controls** | Aggregate logs from all nodes for debugging. Implement safe operational controls (start/stop/pause functionality) and emergency circuit breakers.             | Searchable logs with efficient filtering; Operators can control the pipeline safely.                              | 5 points |
| **Story 4.4: Performance Analysis Tools**                 | Bottleneck detection tool that automatically flags slow Map/Reduce tasks or high cross-cloud latency links.                                                   | Identify performance issues automatically and flag the slowest $\mathbf{AWS}\leftrightarrow\mathbf{GCP}$ path.    | 5 points |

**Sprint 4 Deliverables:**

- Complete monitoring and observability platform.
- **Raft/Consensus Analysis Report** quantifying the challenge of cross-cloud coordination (RQ3).
- Final system ready for deployment and continuous performance analysis.

---

## 🔧 Technical Architecture (Refactored)

**Components:**

- **Consensus Control Plane ($\mathbf{Raft\text{-}Managed}$):** The replicated state machine (AWS, GCP, Azure) that maintains the **Global Task Manifest** and handles Leader Election/Failover.
- **Task Scheduler:** Service that assigns tasks (Map/Reduce) to Node Agents based on the **Load Balancer** policy.
- **Node Agents:** Worker processes on each compute node responsible for $\mathbf{Map}/\mathbf{Reduce}$ execution and health reporting.
- **Durable Storage Manager:** Coordinates immediate writing of all intermediate/final results to neutral cloud storage (S3/GCS) and manages **Checkpoints**.
- **Metrics Collector:** Performance monitoring and alerting (Prometheus/Grafana).

**Key Design Decisions:**

- **Distributed Consensus:** Raft protocol for fault-tolerant state management and rapid Leader failover.
- **Decoupled Compute:** Warm Standby VMs (CPU-only) used to achieve fast RTO while managing GPU cost.
- **Idempotent MapReduce:** All workers write results to durable storage, allowing failed tasks to be safely re-run without duplication.
- **Multi-Cloud Network Awareness:** Routing decisions, Raft timeouts, and load balancing are adapted based on measured cross-cloud latency.

---

## 📊 Success Metrics (Updated)

**Performance Targets:**

- **Throughput:** $\mathbf{10+ GB/s}$ sustained data movement (validated by Matrix Mult. test).
- **Cluster Utilization:** $\mathbf{90\%+}$ efficiency under normal conditions (RQ1).
- **Availability:** $\mathbf{99.9\%}$ uptime with guaranteed automatic recovery.

**Operational Metrics:**

- **RTO (Mean Time to Recovery):** $\mathbf{<90\text{ seconds}}$ for Warm Standby failover (primary research target, RQ4).
- **Data Loss:** **Zero tolerance** for training data loss, verified by checksum on final output.
- **Consensus Impact:** Empirical data on how cross-cloud latency impacts Raft election time (RQ3).

**ASIDE/EXTRA Info**
The matrix multiplication will behave as a stand in for real ML model data.
The following is an absolute guess about how hard it will be to actually implement these and deploy them/get meaningful data:

##### ML Workload Integration Effort (Post-Sprint 2 Core)

This table outlines the additional, focused effort required to adapt the core MapReduce engine for diverse ML workloads.

| ML Model                     | Primary Work Required                                                                                                                                                                                                                                                                               | Effort Estimate            |
| :--------------------------- | :-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | :------------------------- |
| **Matrix Multiplication**    | _0% (Already complete as the MapReduce core validation workload)_                                                                                                                                                                                                                                   | N/A                        |
| **CNN (Plate Recognition)**  | Low to Moderate. Adapt the **Ingestion Engine** for image files, perform basic preprocessing (resize/normalize), and convert them into GPU-friendly tensors. Map workers need logic to run the pre-trained model's forward/backward pass.                                                           | ~4–6 points (1–1.5 days)   |
| **Kaggle Protein Structure** | Moderate. Adapt the **Ingestion Engine** to parse sequence/graph data (e.g., FASTA/PDB files) and convert them into domain-specific embeddings or graph structures. This involves specialized libraries.                                                                                            | ~6–8 points (1.5–2 days)   |
| **Chatbot/LLM**              | Moderate to High. Adapt **Ingestion** for tokenization and sequence padding. The **Map Worker** must handle the complex gradient synchronization (**All-Reduce**) that stresses the network, often requiring specific high-performance communication libraries (like NCCL or custom gRPC handlers). | ~10–13 points (2.5–3 days) |

The kaggle competition ML model is on the way to done. I think there is stanford question/answer dataset we can use to train the chatbot, and I need to src probably about 1000 images of plates and label them for the CNN. So a not insubstantial amount of data for the project. Anyways starting anew.