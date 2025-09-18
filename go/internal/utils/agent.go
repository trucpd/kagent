package utils

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"

	"github.com/kagent-dev/kagent/go/internal/adk"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

// ComputeConfig unmarshals an agent's config and card and returns them as strings
func ComputeConfig(cfg *adk.AgentConfig, card *server.AgentCard) (string, string, uint64, error) {
	bCfg, err := json.Marshal(cfg)
	if err != nil {
		return "", "", 0, err
	}
	bCard, err := json.Marshal(card)
	if err != nil {
		return "", "", 0, err
	}

	hasher := sha256.New()
	hasher.Write(bCfg)
	hasher.Write(bCard)
	hash := hasher.Sum(nil)
	return string(bCfg), string(bCard), binary.BigEndian.Uint64(hash[:8]), nil
}
