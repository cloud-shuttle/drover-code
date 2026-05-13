# Scaling Agentic Coding: Drover-Code and Unikraft Cloud

This document outlines the architecture, philosophy, and advantages of orchestrating remote agentic workers using **drover-code** and **Unikraft Cloud**. 

---

## 1. What is drover-code?

`drover-code` is a lightweight, agentic coding assistant compiled as a single static Go binary. Unlike many AI coding tools that rely on heavy Node.js or Bun environments, `drover-code` is engineered for portability and raw execution efficiency. It interfaces directly with the Anthropic Messages API.

While it features an interactive Terminal User Interface (TUI) for local developers, its superpower lies in its **Headless Mode**. This mode is specifically designed for non-interactive batch processing, allowing `drover-code` to be deployed as an isolated worker—ideal for executing one-shot prompts, applying massive refactors, or running test suites in ephemeral environments.

## 2. What is Unikraft Cloud?

**Unikraft Cloud** is a next-generation cloud platform built on the concept of **unikernels**. 

A unikernel takes a different approach to virtualization than traditional containers or Virtual Machines (VMs). Instead of running your application on top of a full, general-purpose operating system (like Ubuntu or Alpine), a unikernel compiles your application *together* with the absolute bare minimum OS components (network stack, file system, etc.) required to run it. 

The result is a highly specialized, single-purpose machine image. Unikraft Cloud provides the infrastructure to deploy these unikernels with extraordinary speed and density, combining the flexibility of containers with the security of hardware-level VMs.

## 3. Why Execute Code Away from Your Personal Laptop?

As AI agents become more autonomous, executing AI-generated code directly on a developer's laptop introduces significant bottlenecks and risks:

*   **Security & Blast Radius:** AI agents write code, run shell commands, and install dependencies. Running this locally exposes your personal files, credentials, and system state to potentially destructive mistakes or malicious hallucinated packages.
*   **Massive Parallelism:** A single developer might want an agent to independently research and refactor 10 different microservices simultaneously. A local laptop will choke on the CPU, RAM, and I/O demands of spinning up multiple language servers and test suites concurrently.
*   **Pristine Reproducibility:** The classic "it works on my machine" problem. Remote workers ensure that code is generated, compiled, and tested in a clean, standardized environment every single time.
*   **Resource Asymmetry:** AI models operate faster than local hardware can sometimes compile. Offloading compilation, testing, and linting to scalable cloud compute keeps the local experience fluid.

## 4. The Traditional Challenges of Remote Execution

Historically, moving development workloads to the cloud has been fraught with friction:

*   **Virtual Machines (EC2):** Booting a traditional VM takes anywhere from 30 seconds to several minutes. For an AI agent that needs to run a quick 2-second unit test, this latency is unacceptable. VMs are also heavy and expensive to keep idling.
*   **Containers (Docker/Kubernetes):** While faster than VMs, containers still carry generic OS overhead. More importantly, they share the host kernel. If an AI agent executes malicious code, container escape vulnerabilities are a real threat.
*   **Serverless Functions (AWS Lambda):** Lambdas offer fast scaling but are highly restrictive. They often lack the required system-level dependencies (like compilers, language servers, or CLI tools), have restrictive file systems, and are entirely stateless, making it extremely difficult to synchronize a complex local Git repository with the function.
*   **Synchronization Latency:** Keeping the local developer's IDE in perfect sync with the remote execution environment often relies on clunky `rsync` loops or slow network file systems, degrading the developer experience.

## 5. How Unikraft Cloud Smashes These Challenges

Unikraft Cloud provides a phenomenal execution environment that fundamentally changes the paradigm for remote agentic workers like `drover-code`:

### Millisecond Cold Starts
Unikernels are stripped of all generic OS bloat (no init systems, no background daemons). As a result, Unikraft instances boot in **milliseconds**. When `drover-code` needs to execute a task, a brand-new Unikraft worker can be provisioned and booted just-in-time, creating a serverless-like experience but for arbitrary stateful workloads.

### Hardware-Level Security (MicroVMs)
Unlike containers that share a kernel, Unikraft unikernels run as hardware-virtualized MicroVMs (via hypervisors like KVM). This means `drover-code` can execute untrusted, AI-generated code with the absolute maximum security boundary. You get the isolation of a VM without the performance penalty.

### Microscopic Footprint & Infinite Density
Because they only contain what the application strictly needs, Unikraft images are incredibly small (often single-digit megabytes) and require minimal RAM. This allows thousands of `drover-code` workers to be packed onto standard hardware. You can fan out 100 parallel agentic tasks for pennies.

### Perfect Synergy with `drover-code` Headless Mode
`drover-code` compiling to a single static Go binary makes it the perfect candidate for a unikernel. By wrapping `drover-code` in Unikraft, we create an ephemeral, instantly-booting "brain." 

The architecture workflow becomes seamless:
1. The local coordinator orchestrates a task.
2. A Unikraft worker boots in milliseconds, pre-loaded with `drover-code` in headless mode.
3. The codebase state is synced.
4. The LLM executes its task safely in hardware-isolated, high-performance compute.
5. The diff is reliably merged back to the local repository, and the worker instantly dies.

Ultimately, Unikraft Cloud allows `drover-code` to break free of local hardware constraints, delivering a secure, parallelized, and instantly scalable execution environment that feels as fast as running it locally.

## 6. Advanced Orchestration Features

To support production-grade workflows, the `drover-code` and Unikraft Cloud integration has been hardened with advanced orchestration capabilities:

### Dynamic Custom Toolchains (`drover-worker.Dockerfile`)
Not all AI tasks run on the same stack. The orchestrator automatically detects if a workspace requires specialized dependencies (like Python libraries, Rust compilers, or C-bindings) via a `drover-worker.Dockerfile`. It seamlessly compiles a custom OCI image using the base Unikraft Agent and provisions the cloud instance with this tailored environment, combining infinite flexibility with Kraftcloud's millisecond boot times.

### Resilient SSE Streaming
Running complex AI execution graphs remotely can take minutes. If the connection drops due to load balancers or network flakes, the execution shouldn't be lost. The Unikraft Agent maintains a stateful, in-memory event history buffer. Using `Last-Event-ID` mechanisms, the local orchestrator can transparently reconnect and replay any missed events, ensuring a flawless streaming transcript without dropping the MicroVM.

### Automated Acceptance & Auto-Merge
Delegating code generation to the cloud is only useful if it safely integrates back into your local workflow. With the `--accept-cmd` functionality, the orchestrator commits the cloud worker's diff to an isolated Git branch and triggers a local acceptance test (like `cargo test` or `npm run build`). If the tests pass, the remote agent's work is automatically merged into the developer's working branch, creating a completely hands-free, safe CI/CD loop for AI-generated code.

## 7. The Dual-Path Deployment Model: BYOC vs Drover Cloud SaaS

To maximize flexibility and security while providing a frictionless onboarding experience, the architecture is splitting into an **Open-Core / COSS** model. 

### Option 1: "Bring Your Own Cloud" (BYOC)
For enterprise users and security-conscious teams, `drover-code` will remain a powerful open-source CLI. 
*   Developers run `drover-code --coordinator-remote` locally.
*   They provide their own `$UKC_TOKEN` and Anthropic API key.
*   The local CLI directly provisions Unikraft instances on their personal Kraftcloud account.
*   **Advantage**: Proprietary code never passes through a third-party Drover server.

### Option 2: Drover Cloud (SaaS Platform)
For users who want zero-friction onboarding, we introduce the `drover-cloud` SaaS platform.
*   Developers run `drover-code --cloud`.
*   The CLI acts as a lightweight client. It compresses the workspace and securely uploads it to `api.drover.cloud`.
*   The `drover-cloud` backend server handles all the Kraftcloud instance provisioning, custom toolchain building, and LLM API keys under the hood.
*   **Advantage**: No need to set up Docker registries or Kraftcloud tokens. Enables team billing, centralized web dashboards, and GitHub App integrations for automated PR reviews.

This split ensures that the core orchestration logic remains battle-tested and open, while the SaaS platform handles the heavy lifting of infrastructure management.
