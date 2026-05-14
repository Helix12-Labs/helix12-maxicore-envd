// Package codec implements Manus 1:1 custom JSON-codec for Connect-RPC.
//
// Manus's sandbox-runtime uses a snake_case JSON-codec instead of the default
// camelCase. RAM symbols verified:
//
//	sbx-go-svc/pkg/runtime.snakeCaseJSONCodec.Marshal
//	sbx-go-svc/pkg/runtime.snakeCaseJSONCodec.Name
//	sbx-go-svc/pkg/runtime.snakeCaseJSONCodec.Unmarshal
//
// This codec must be registered with Connect-RPC handlers/clients via
//
//	connect.WithCodec(&codec.SnakeCaseJSONCodec{})
//
// so that protobuf field names are emitted in snake_case (matching the
// `.proto` source names) and accepted in snake_case on the wire.
//
// Why this matters: protojson default emits camelCase JSON (matching the
// generated Go-field names). Manus emits snake_case (matching the original
// proto field names). For 1:1 wire-compatibility with Manus orchestrator
// or any 3rd-party client written against the Manus schema, we MUST use
// snake_case.
package codec

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// codecName must match what Connect-RPC uses for the `application/json`
// content-type. Returning "json" registers this as the JSON-codec.
const codecName = "json"

// SnakeCaseJSONCodec implements connect.Codec with snake_case JSON encoding.
type SnakeCaseJSONCodec struct{}

// Name returns the codec identifier used in Connect-RPC's Content-Type negotiation.
func (c *SnakeCaseJSONCodec) Name() string { return codecName }

// Marshal serializes a protobuf message to snake_case JSON.
//
// UseProtoNames=true   → field names in JSON match the .proto definition (snake_case)
// EmitUnpopulated=true → emit zero-valued fields (Manus's behaviour for stable wire format)
func (c *SnakeCaseJSONCodec) Marshal(message any) ([]byte, error) {
	if message == nil {
		return nil, errors.New("snake_case codec: nil message")
	}
	pm, ok := message.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("snake_case codec: not a proto.Message, got %T", message)
	}
	opts := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: true,
	}
	return opts.Marshal(pm)
}

// Unmarshal deserializes snake_case JSON into a protobuf message.
//
// DiscardUnknown=true  → tolerate unknown fields (forward-compat with future
//
//	schema changes from Manus orchestrator)
func (c *SnakeCaseJSONCodec) Unmarshal(data []byte, message any) error {
	if message == nil {
		return errors.New("snake_case codec: nil message")
	}
	pm, ok := message.(proto.Message)
	if !ok {
		return fmt.Errorf("snake_case codec: not a proto.Message, got %T", message)
	}
	opts := protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}
	return opts.Unmarshal(data, pm)
}
