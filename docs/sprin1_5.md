Sprint 1.5 Plan: Real Cloud Deployment (Terraform + Ansible)                                                                                                    
                                                                                                                                                                 
 Context                                                                                                                                                         

 The project currently simulates AWS/GCP/Azure using Docker bridge networks with tc-netem latency.
 Sprint 1.5 lifts the exact same application onto real cloud VMs — one per provider — using Terraform
 to provision infrastructure and Ansible to deploy. A matrix-multiply PyTorch task serves as the
 GPU-capable workload stand-in for an LLM pipeline.

 User decisions

 - GPU optional via var.enable_gpu (default false = CPU-only, ~$0.05/hr; true = T4, ~$0.50/hr each)
 - Inter-cloud: public static IPs + firewall rules (no VPN)
 - Storage: MinIO container on the AWS VM, publicly reachable by GCP/Azure workers

 Files to create

 Sprint doc

 - docs/sprint1.5.md — story cards in same format as sprint0.md

 Terraform

 terraform/
 ├── main.tf           — calls 3 modules, outputs IPs, remote state config
 ├── variables.tf      — enable_gpu, instance sizes, ssh_key_path, project IDs
 ├── outputs.tf        — aws_ip, gcp_ip, azure_ip (used by Ansible)
 ├── terraform.tfvars.example
 └── modules/
     ├── aws-node/
     │   ├── main.tf   — VPC, subnet, IGW, SG, EIP, EC2 (t3.large or g4dn.xlarge)
     │   ├── variables.tf
     │   └── outputs.tf
     ├── gcp-node/
     │   ├── main.tf   — VPC, subnet, firewall, static IP, Compute Engine instance
     │   ├── variables.tf
     │   └── outputs.tf
     └── azure-node/
         ├── main.tf   — RG, VNet, subnet, NSG, public IP, NIC, Linux VM
         ├── variables.tf
         └── outputs.tf

 Ports open between all 3 VMs: 22 (SSH), 7000 (Raft), 50051 (gRPC), 8080-8085 (HTTP health), 9000 (MinIO)

 Ansible

 ansible/
 ├── group_vars/all.yml          — shared vars (docker version, image names)
 ├── inventory/
 │   └── generate.sh             — runs `terraform output -json` → hosts.ini
 ├── roles/
 │   ├── docker/tasks/main.yml   — install Docker + nvidia-container-toolkit (when GPU)
 │   └── pipeline-node/
 │       ├── tasks/main.yml      — rsync source, write .env, docker compose up
 │       └── templates/
 │           ├── env.j2          — .env with real IPs from hostvars
 │           └── docker-compose.cloud.j2 — per-cloud compose (parameterized)
 └── playbooks/
     ├── deploy.yml              — roles: docker → pipeline-node on all 3 hosts
     └── teardown.yml            — docker compose down on all 3 hosts

 Per-cloud docker-compose (templated by Ansible)

 Key differences from local sim:
 - No gateway container (real latency, no tc-netem)
 - No custom Docker networks (default bridge)
 - RAFT_PEERS uses real public IPs from Terraform outputs
 - MINIO_ENDPOINT points to AWS VM public IP:9000
 - MinIO container only on AWS VM

 Matrix multiply task

 - worker/worker/tasks/__init__.py
 - worker/worker/tasks/matrix_multiply.py — PyTorch matmul, GPU if available else CPU fallback
   - Input payload: {"size": 2048}
   - Output: {"device": "cuda|cpu", "size": 2048, "duration_ms": ..., "result_norm": ...}

 Makefile targets

 cloud-init:    # terraform init
 cloud-plan:    # terraform plan
 cloud-up:      # terraform apply -auto-approve && ansible-playbook deploy.yml
 cloud-down:    # ansible-playbook teardown.yml && terraform destroy -auto-approve
 cloud-status:  # curl health endpoints on all 3 public IPs

 Scripts

 - scripts/cloud-deploy.sh — wrapper: generates inventory from terraform output, runs ansible

 Sprint doc structure (S1.5.1 – S1.5.4)

 - S1.5.1 (5pts) — Terraform: 3 VMs + networking + security groups
 - S1.5.2 (4pts) — Ansible: Docker install + deploy pipeline per cloud
 - S1.5.3 (3pts) — Matrix multiply task + end-to-end smoke test on real hardware
 - S1.5.4 (2pts) — Cost controls: spot/preemptible instances, teardown script, cost doc

 Verification

 1. make cloud-up — provisions VMs, deploys containers
 2. curl http://<aws_ip>:8080/health — all 3 CP nodes respond
 3. bash scripts/test-storage.sh adapted for cloud IPs
 4. docker exec worker-aws-1 python -c "from worker.tasks.matrix_multiply import run; print(run({'size':1024}))" on AWS VM
 5. make cloud-down — full teardown, no orphaned resources
