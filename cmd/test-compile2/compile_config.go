package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

func main() {
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	p, err := policy.ParsePolicy(bytes.NewReader(data))
	if err != nil {
		panic(err)
	}
	if errs := policy.ValidatePolicy(p); policy.HasErrors(errs) {
		panic(fmt.Sprintf("validate: %v", errs))
	}
	out, err := policy.CompileGatewayConfig(p)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(out))
}
