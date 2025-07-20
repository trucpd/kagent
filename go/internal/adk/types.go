package adk

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"

	"trpc.group/trpc-go/trpc-a2a-go/server"
)

type StreamableHTTPConnectionParams struct {
	Url              string            `json:"url"`
	Headers          map[string]string `json:"headers"`
	Timeout          float64           `json:"timeout"`
	ReadTimeout      float64           `json:"read_timeout"`
	TerminateOnClose bool              `json:"terminate_on_close"`
}

type HttpMcpServerConfig struct {
	Params StreamableHTTPConnectionParams `json:"params"`
	Tools  []string                       `json:"tools"`
}

type SseConnectionParams struct {
	Url         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	Timeout     float64           `json:"timeout"`
	ReadTimeout float64           `json:"read_timeout"`
}

type SseMcpServerConfig struct {
	Params SseConnectionParams `json:"params"`
	Tools  []string            `json:"tools"`
}

type AgentConfig struct {
	AgentCard   server.AgentCard      `json:"agent_card"`
	Name        string                `json:"name"`
	Model       string                `json:"model"`
	Description string                `json:"description"`
	Instruction string                `json:"instruction"`
	HttpTools   []HttpMcpServerConfig `json:"http_tools"`
	SseTools    []SseMcpServerConfig  `json:"sse_tools"`
	Agents      []AgentConfig         `json:"agents"`
}

var _ sql.Scanner = &AgentConfig{}

func (a *AgentConfig) Scan(value interface{}) error {
	return json.Unmarshal(value.([]byte), a)
}

var _ driver.Valuer = &AgentConfig{}

func (a AgentConfig) Value() (driver.Value, error) {
	return json.Marshal(a)
}
