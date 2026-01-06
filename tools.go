//go:build tools
// +build tools

// Package tools manages tool dependencies for code generation
package tools

import (
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"
)
