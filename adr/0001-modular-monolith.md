# ADR-0001: Modular Monolith

Status: Accepted

Use one Go codebase with separable API/worker/scheduler/MCP processes. Extract services only when scaling/isolation/ownership proves necessary.
