# Daily Infrastructure (Sub-project 4a)

Date: 2026-09-13
Status: Approved for planning

## Problem

The permanent foundation (sub-project 2) now exists: a VPC with public subnets,
the OpenSearch security group, the IAM roles the cluster and its pods assume,
ECR repositories, the document corpus bucket, the Anthropic secret, and a
budget. None of it runs compute. To demo the assistant on AWS, the owner needs
the expensive, disposable half of the environment: an EKS cluster with worker
nodes and an OpenSearch domain, stood up at the start of a working session and
destroyed at the end so the account bills near zero while idle.

Sub-project 4 (the daily stack) bundles four independent concerns —
infrastructure, application deploy, image build/orchestration, and DNS/TLS —
too much for one spec. It is decomposed into three phases:

- **4a — Daily infrastructure (this spec):** the EKS cluster, managed node
  group, OpenSearch domain, and Pod Identity associations. Pure Terraform,
  AWS-provider-only, pointed at foundation's outputs.
- **4b — Application deploy:** Kubernetes manifests, the `ka-config` secret,
  the AWS Load Balancer Controller (Helm), index creation, the reseed
  Job/CronJob that reads the corpus from S3, the Ingress, and DNS/TLS.
- **4c — Images & orchestration:** build/push images to ECR, the
  morning-up / evening-down sequencing, and the teardown safety net.

4a is the critical path: nothing in 4b or 4c can run until a cluster and an
OpenSearch domain exist.

## Operating model

The owner runs `terraform apply` on this stack at the start of a session and
`terraform destroy` at the end. There is no "keep it warm" option: OpenSearch is
destroyed with the stack every time, so its index is gone on every teardown. The
document corpus lives in the foundation's S3 bucket and survives; 4b rebuilds the
index from S3 on each bring-up. Therefore **the OpenSearch index is disposable
and S3 is the source of truth** — 4a creates an empty domain and owns nothing
about its contents.

Bring-up is dominated by OpenSearch domain creation (~15–20 minutes); teardown
is dominated by OpenSearch deletion, which is also not instant. 4c's
orchestration waits on domain state; 4a's own resources are otherwise fast.

## Goals

- A `terraform/daily/` stack that creates an EKS cluster, one managed node group,
  and a single-node OpenSearch domain sized for a solo demo.
- The stack creates no IAM. It consumes foundation's roles and networking through
  `terraform_remote_state`, so the nightly `terraform destroy` never touches
  identity or the corpus.
- `terraform apply` followed by `terraform destroy` is clean and repeatable: no
  resource leaks that make a later destroy hang.
- The identity that applies the stack immediately has cluster admin, via an EKS
  access entry for foundation's `admin_principal_arn`.
- Pods can assume their foundation roles through Pod Identity: chat-api reaches
  Bedrock and Secrets Manager, ingestion reaches S3 and OpenSearch, and the load
  balancer controller's service account is pre-bound to its role so that when 4b
  installs the controller it can manage ALBs immediately.
- 4a installs no cluster workloads and no Helm releases; it creates no Ingress
  and no load balancer controller.
- Every value that could vary — Kubernetes version, instance types, node counts,
  OpenSearch engine version and sizing, namespaces, service-account names — comes
  from tfvars. The `.tf` files contain no literal configuration
  (`check-literals.sh` enforces this).
- `make tf-check` passes without credentials: fmt, validate with
  `init -backend=false`, `terraform test` against a mocked AWS provider, and the
  script tests.

## Non-Goals

- **Everything in 4b:** Kubernetes manifests, the `ka-config` secret, installing
  the AWS Load Balancer Controller (Helm), creating the OpenSearch index
  (`EnsureIndex`), the reseed Job/CronJob, the Ingress, DNS, and TLS. 4a leaves
  an empty domain and a cluster with no workloads.
- **Any Helm or Kubernetes provider in the stack.** 4a is AWS-provider-only so it
  stays offline-testable with `terraform test`; workloads are 4b's job.
- **Everything in 4c:** image build/push, morning-up/evening-down commands, the
  forgotten-teardown safety net.
- New IAM. Foundation owns all roles by design; 4a references their ARNs.
- OpenSearch fine-grained access control / master users. The domain is
  VPC-isolated and ephemeral; VPC isolation plus IAM is the access boundary.
- High availability. Single AZ, single data node, no dedicated master — this is a
  disposable demo cluster, not production.
- Spot instances for nodes. On-demand, so a demo does not lose nodes mid-session.
- CI/CD. Terraform runs only from the owner's laptop.

## Decisions

**Build with the community EKS module + a hand-rolled OpenSearch domain.**
`terraform-aws-modules/eks` v21 (`~> 21.25`, requires `aws >= 6.59`, satisfied by
foundation's `~> 6.64`) builds the cluster, managed node group, access entries,
and the core add-ons. OpenSearch is a single `aws_opensearch_domain` resource.
Fully hand-rolled EKS was rejected as a large, leak-prone surface.

**The AWS Load Balancer Controller is installed in 4b, not 4a.** AWS publishes no
managed EKS add-on for it (it is a built-in capability only of EKS Auto Mode,
which we are not using), so the only ways to install it are Helm or a raw
manifest — both cluster workloads, not infrastructure. Pulling the Helm or
Kubernetes provider into 4a would break its offline `terraform test` story.
Therefore 4a installs no controller and creates only the *Pod Identity
association* that pre-binds the controller's service account
(`kube-system/aws-load-balancer-controller`) to foundation's role; 4b
`helm install`s the controller into that already-bound service account.
Destroy-safety is a 4c ordering concern regardless of install method: the
Ingress (which provisions the real ALB) must be deleted before the cluster is
destroyed.

**Reuse foundation's IAM roles.** The EKS module accepts a pre-created cluster
role (`create_iam_role = false`, `iam_role_arn = ...`) and a pre-created node
role per node group (`create_iam_role = false`, `iam_role_arn = ...` inside the
`eks_managed_node_groups` entry), so 4a creates no IAM — foundation owns all of
it.

**Foundation is the source of truth for identity and networking.** 4a reads
everything it needs from foundation's outputs and declares no roles, subnets, or
security groups of its own.

**The OpenSearch index lifecycle belongs to 4b.** 4a creates an empty domain;
index creation and reseed from S3 are application concerns run as Kubernetes
workloads, keeping 4a free of app logic and cluster-workload dependencies.

## Stack layout

New stack at `terraform/daily/`, mirroring foundation's file-per-concern style:

- `backend.tf` — S3 state at key `daily/terraform.tfstate`, separate from
  foundation so the two have independent lifecycles.
- `data.tf` — `terraform_remote_state.foundation` reading foundation's outputs
  (VPC, subnets, OpenSearch SG, pod-role ARNs, ECR URLs, docs bucket, admin
  principal, region). The only coupling between the stacks.
- `versions.tf` — pins Terraform, the AWS provider, and the
  `terraform-aws-modules/eks` module version. Adds nothing that needs
  credentials for `make tf-check`.
- `eks.tf` — the EKS module invocation: cluster, managed node group, access
  entries, and the core add-ons (`vpc-cni`, `coredns`, `kube-proxy`,
  `eks-pod-identity-agent`).
- `opensearch.tf` — the hand-rolled `aws_opensearch_domain` and its config.
- `pod-identity.tf` — three `aws_eks_pod_identity_association` resources.
- `variables.tf` / `terraform.tfvars` — all knobs; no literals in `.tf`.
- `outputs.tf` — the 4a → 4b contract, plus `verify_expectations`.

Nothing in 4a is persistent; it is all built to be destroyed and recreated.

## EKS cluster, node group & access

The `terraform-aws-modules/eks` invocation, sized for a solo demo:

- **Cluster:** Kubernetes version pinned in tfvars (e.g. `1.31`). `vpc_id` and
  `subnet_ids` from foundation's remote state. Public API endpoint enabled
  (kubectl from the owner's laptop); private access also enabled so pods reach
  the API in-VPC.
- **IAM roles:** reuse foundation's `eks_cluster_role_arn` (module
  `create_iam_role = false` + `iam_role_arn`) and `eks_node_role_arn` (per
  node group: `create_iam_role = false` + `iam_role_arn`). Foundation owns
  identity by design; 4a creates no IAM role.
- **Managed node group:** `t3.medium`, on-demand, min 1 / desired 1 / max 2 (all
  in tfvars), in foundation's public subnets.
- **Access:** authentication mode `API` (access entries only, no aws-auth
  configmap). One access entry granting `AmazonEKSClusterAdminPolicy` to
  foundation's `admin_principal_arn`, so the applying identity can immediately
  `aws eks update-kubeconfig` and use kubectl.
- **Core addons (managed):** `vpc-cni`, `coredns`, `kube-proxy`, and
  `eks-pod-identity-agent`. The pod-identity agent is required for the
  associations below to function.

## OpenSearch domain

A hand-rolled `aws_opensearch_domain`, sized to be disposable:

- **Cluster config:** 1 × `t3.small.search` data node, no dedicated master,
  `zone_awareness_enabled = false` (single AZ). EBS `gp3`, ~20 GiB. Engine
  version pinned in tfvars (e.g. `OpenSearch_2.x`), matching what
  `services/chat-api/internal/opensearch` expects.
- **Networking (VPC mode):** placed in one of foundation's subnets (single node
  ⇒ exactly one subnet; take the first from the remote-state list), with
  foundation's `opensearch_security_group_id` attached. No public endpoint.
- **Access policy:** VPC-scoped, IAM-based. The chat-api and ingestion pods
  authenticate with their Pod Identity roles. Fine-grained access control off.
- **Encryption:** encryption-at-rest and node-to-node encryption on; TLS
  enforced (`enforce_https`).

The domain takes ~15 minutes to create and is slow to delete. 4c's orchestration
waits for it to become `Active` before reseed and must not assume instant
deletion on teardown.

## Pod Identity associations

Three `aws_eks_pod_identity_association` resources, each binding a foundation
pod-role ARN to a Kubernetes service account (namespace and SA name in tfvars so
4a and 4b agree without hardcoding):

| Foundation role                   | Namespace / ServiceAccount                    | Purpose                                            |
| --------------------------------- | --------------------------------------------- | -------------------------------------------------- |
| `pod_role_arns["chat-api"]`       | `default` / `chat-api`                        | Bedrock (embed/rerank/rewrite) + Anthropic secret  |
| `pod_role_arns["ingestion"]`      | `default` / `ingestion`                       | read the docs bucket from S3, write OpenSearch      |
| `pod_role_arns["aws-lb-controller"]` | `kube-system` / `aws-load-balancer-controller` | manage ALBs on the cluster's behalf            |

The associations `depends_on` the `eks-pod-identity-agent` addon so a fresh apply
does not race the agent install. 4a creates the *associations*; the
ServiceAccount objects are created in 4b. Pod Identity binds by name and does not
require the SA to exist at apply time, so this ordering keeps 4a app-free.

## Load balancer controller wiring & destroy safety

4a does **not** install the AWS Load Balancer Controller — AWS publishes no
managed add-on for it, and installing it via Helm or a manifest would pull a
cluster-workload provider into an otherwise AWS-provider-only stack. What 4a does
own is the **Pod Identity association** that pre-binds the controller's service
account (`kube-system/aws-load-balancer-controller`) to foundation's
`aws-lb-controller` role. Because associations bind by name and do not require
the service account (or the controller) to exist at apply time, 4b can later
`helm install` the controller into that service account and it has permissions
immediately. This keeps the IAM wiring in Terraform (foundation-owned identity)
while the workload lives in 4b.

Since 4a installs no controller and creates no Ingress, a plain `terraform
destroy` of 4a has no ALB to leak. The cross-layer hazard belongs to later
phases: once 4b creates an Ingress (which provisions a real ALB), that ALB and
its ENIs must be deleted *before* 4a destroys the cluster, or subnet/ENI-
dependent deletes hang. Two guardrails, both outside 4a:

1. **Ordering is 4c's contract:** evening-down deletes Kubernetes Ingress
   resources (releasing ALBs), waits, then runs `terraform destroy` on 4a.
2. **Documented recovery:** the destroy runbook records the failure signature
   (destroy hanging on a subnet/ENI dependency) and the manual fix (delete the
   orphaned ALB, retry). 4c's teardown safety net automates the check.

4a's own destroy is clean by construction; the cross-layer ordering is enforced
in 4c.

## Outputs (the 4a → 4b contract)

`outputs.tf` exposes only what 4b and the orchestration scripts consume:

- `cluster_name` — for `aws eks update-kubeconfig` and kubectl targeting.
- `cluster_endpoint` and `cluster_certificate_authority_data` — for scripted
  kubeconfig assembly.
- `cluster_version` — so 4b can assert compatibility.
- `opensearch_endpoint` — the VPC endpoint 4b bakes into `ka-config`.
- `node_group_name` — for teardown/scale checks in 4c.
- `region` — passed through from foundation.
- `verify_expectations` — the values `verify.sh` checks against the live account:
  cluster status `ACTIVE`, node count, OpenSearch `Active`, and the add-on list
  (`vpc-cni`, `coredns`, `kube-proxy`, `eks-pod-identity-agent`).

## Testing & validation

Following the repo's Terraform conventions (`make tf-check` = fmt, validate,
`terraform test` mocked, script tests — no credentials):

- **`terraform test`** with a mocked AWS provider: the EKS module receives
  foundation's VPC/subnets/roles; exactly three Pod Identity associations with
  the correct role → service-account mapping; OpenSearch is single-node, no
  multi-AZ, VPC-mode with foundation's SG; the admin access entry uses
  foundation's `admin_principal_arn`.
- **`check-literals.sh`** already forbids literal configuration in `.tf`; the new
  stack must pass it.
- **`verify.sh`** (owner-run, needs credentials, not in `tf-check`): post-apply
  live checks against `verify_expectations` — cluster ACTIVE, nodes Ready,
  OpenSearch Active, addons present.
- **`init -backend=false`** for validate, per CLAUDE.md — never touches the real
  backend.
- No Go tests: 4a is pure infrastructure. Application wiring (config, index,
  reseed) is 4b.
