// Package brain defines the Brain interface that fronts every LLM
// provider (cloud or local) and is the only thing the agent loop
// depends on. Provider implementations live in subpackages.
package brain
