# Flux Redis Cluster Agent Instructions

Whenever you are assigned a task in this workspace, follow these strict rules to ensure production-readiness and prevent context loss:

1. **Context First**: Before writing any code, always read `PLAN.md` and `STATUS.md` in the root directory to understand the architecture and current stage of development.
2. **Consult Prior Art**: Heavy inspiration should be drawn from `/root/flux-pg-cluster`. Always consult that repository for implementation details regarding the mock-api, deterministic TLS certificate generation, proxy architecture, and the Pytest testing infrastructure.
3. **Iterative Development**: Open `STATUS.md`, identify the first unchecked item, and implement ONLY that item. Do not attempt to complete multiple steps at once.
4. **State Tracking**: Once you finish your item, check it off in `STATUS.md`.
5. **Stop and Review**: After updating `STATUS.md`, summarize what you did for the user and stop executing tools. Wait for the user's review and approval before proceeding to the next step.
6. **Versioning & Docker Images**: On every functional change that affects the container image:
   - Bump the semver in `VERSION` (patch for fixes, minor for features).
   - Ensure `Dockerfile` embeds it via `-X main.version=$(cat VERSION)` and copies `VERSION` to `/app/VERSION`.
   - `flux-agent` must log `flux-redis-cluster flux-agent <subcommand> version=<VERSION>` at the start of `init`, `daemon`, and `proxy`; container startup runs `/app/flux-agent version` before `init`.
   - Rebuild and push `alihmahdavi/flux-redit-cluster:latest` after image-affecting changes unless the user says otherwise.
   - Tell the user the new version string and image digest so they can confirm nodes are running the expected build (`/app/flux-agent version` or container logs).
