// Package main is the AgentPaaS daemon (agentpaasd) entry point.
//
// The daemon binds to a Unix domain socket, serves the Control API gRPC
// service, and manages agent lifecycle. It enforces single-instance via
// flock, validates home directory permissions, and supports graceful
// shutdown on SIGTERM/SIGINT.
package main