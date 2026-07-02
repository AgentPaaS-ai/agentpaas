// Package main is the AgentPaaS harness (agentpaas-harness) entry point.
//
// The harness runs as PID 1 inside agent containers, serves the HTTP
// lifecycle API on loopback, and manages the Python agent worker process.
// Configuration is read from AGENTPAAS_* environment variables.
package main