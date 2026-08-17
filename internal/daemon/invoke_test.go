package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/ng/devagent/internal/model"
	"github.com/ng/devagent/internal/protocol"
)

func invokeCap(name string, impl model.Implementation) model.Capability {
	return model.Capability{Name: name, Implementation: impl}
}

func TestInvokeCore_NativeShell(t *testing.T) {
	ds := &DaemonServer{nativeHandlers: NewNativeHandlerRegistry()}
	cap := invokeCap("shell_exec", model.Implementation{Native: "shell", TimeoutMs: 5000})

	result, err := ds.invokeCore(context.Background(), "my_pc", cap, map[string]any{"command": "echo hello"})
	if err != nil {
		t.Fatalf("invokeCore failed: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("expected status ok, got %s", result.Status)
	}
	if result.Protocol != "" {
		t.Errorf("native path should not set protocol, got %s", result.Protocol)
	}
}

func TestInvokeCore_HALNotAvailable(t *testing.T) {
	ds := &DaemonServer{nativeHandlers: NewNativeHandlerRegistry()}
	cap := invokeCap("set_relay", model.Implementation{Proxy: "uart", CmdMap: map[string]model.CmdMap{
		"set_relay": {Cmd: 161, Fmt: "{pin} {state}"},
	}})

	result, err := ds.invokeCore(context.Background(), "shelf_01", cap, map[string]any{"pin": 1, "state": true})
	if err != nil {
		t.Fatalf("invokeCore failed: %v", err)
	}
	if result.Status != "hal_not_available" {
		t.Errorf("expected hal_not_available, got %s", result.Status)
	}
}

func TestInvokeCore_URPC(t *testing.T) {
	hal := &MockHAL{TType: TransportURP}
	ds := &DaemonServer{hal: hal, nativeHandlers: NewNativeHandlerRegistry()}
	cap := invokeCap("set_relay", model.Implementation{Proxy: "uart", CmdMap: map[string]model.CmdMap{
		"set_relay": {Cmd: 161, Fmt: "{pin} {state}"},
	}})

	result, err := ds.invokeCore(context.Background(), "shelf_01", cap, map[string]any{"pin": 1, "state": true})
	if err != nil {
		t.Fatalf("invokeCore failed: %v", err)
	}
	if result.Status != "OK" {
		t.Errorf("expected OK, got %s", result.Status)
	}
	if result.Protocol != "urpc" {
		t.Errorf("expected urpc protocol, got %s", result.Protocol)
	}
}

func TestInvokeCore_URPCPayloadFormatting(t *testing.T) {
	var gotPayload []byte
	hal := &MockHAL{
		TType: TransportURP,
		SendURFn: func(ctx context.Context, req *protocol.URPCRequest) (*protocol.URPCAck, error) {
			gotPayload = req.Payload
			if req.Cmd != 161 {
				t.Errorf("expected cmd 161, got %d", req.Cmd)
			}
			return &protocol.URPCAck{Seq: req.Seq, Status: protocol.StatusOK, QueueDepth: 2}, nil
		},
	}
	ds := &DaemonServer{hal: hal, nativeHandlers: NewNativeHandlerRegistry()}
	cap := invokeCap("set_relay", model.Implementation{Proxy: "uart", CmdMap: map[string]model.CmdMap{
		"set_relay": {Cmd: 161, Fmt: "{pin} {state}"},
	}})

	result, err := ds.invokeCore(context.Background(), "shelf_01", cap, map[string]any{"pin": 1, "state": true})
	if err != nil {
		t.Fatalf("invokeCore failed: %v", err)
	}
	if string(gotPayload) != "1 1" {
		t.Errorf("expected payload '1 1', got %q", gotPayload)
	}
	if result.Result.(map[string]any)["queue_depth"] != byte(2) {
		t.Errorf("expected queue_depth 2 in result")
	}
}

func TestInvokeCore_URPCSendError(t *testing.T) {
	hal := &MockHAL{
		TType: TransportURP,
		SendURFn: func(ctx context.Context, req *protocol.URPCRequest) (*protocol.URPCAck, error) {
			return nil, errors.New("serial timeout")
		},
	}
	ds := &DaemonServer{hal: hal, nativeHandlers: NewNativeHandlerRegistry()}
	cap := invokeCap("set_relay", model.Implementation{Proxy: "uart", CmdMap: map[string]model.CmdMap{
		"set_relay": {Cmd: 161, Fmt: "{pin} {state}"},
	}})

	_, err := ds.invokeCore(context.Background(), "shelf_01", cap, map[string]any{"pin": 1, "state": true})
	if err == nil {
		t.Fatal("expected send error")
	}
}

func TestInvokeCore_CmdMapNotFound(t *testing.T) {
	hal := &MockHAL{TType: TransportURP}
	ds := &DaemonServer{hal: hal, nativeHandlers: NewNativeHandlerRegistry()}
	cap := invokeCap("read_temp", model.Implementation{Proxy: "uart", CmdMap: map[string]model.CmdMap{}})

	result, err := ds.invokeCore(context.Background(), "shelf_01", cap, map[string]any{})
	if err != nil {
		t.Fatalf("invokeCore failed: %v", err)
	}
	if result.Status != "cmd_map_not_found" {
		t.Errorf("expected cmd_map_not_found, got %s", result.Status)
	}
}

func TestInvokeCore_DCPWithManualIntentID(t *testing.T) {
	var gotIntent uint16
	hal := &MockHAL{
		TType: TransportDCP,
		SendDFn: func(ctx context.Context, seq byte, intentID uint16, params map[string]any) (*protocol.DCPFrame, error) {
			gotIntent = intentID
			return &protocol.DCPFrame{Ver: protocol.DCPVersion, Kind: protocol.DCPReply, Seq: seq, IntentID: intentID}, nil
		},
	}
	ds := &DaemonServer{hal: hal, nativeHandlers: NewNativeHandlerRegistry()}
	cap := invokeCap("set_relay", model.Implementation{Proxy: "uart", Protocol: "DCP"})
	cap.IntentID = 4660

	result, err := ds.invokeCore(context.Background(), "shelf_01", cap, map[string]any{"pin": 1, "state": true})
	if err != nil {
		t.Fatalf("invokeCore failed: %v", err)
	}
	if gotIntent != 4660 {
		t.Errorf("expected manual intent 4660, got %d", gotIntent)
	}
	if result.Status != "ok" {
		t.Errorf("expected ok, got %s", result.Status)
	}
	if result.Protocol != "dcp" {
		t.Errorf("expected dcp protocol, got %s", result.Protocol)
	}
}

func TestInvokeCore_DCPComputedIntentID(t *testing.T) {
	expected := protocol.IntentID("shelf_01.set_relay")
	var gotIntent uint16
	hal := &MockHAL{
		TType: TransportDCP,
		SendDFn: func(ctx context.Context, seq byte, intentID uint16, params map[string]any) (*protocol.DCPFrame, error) {
			gotIntent = intentID
			return &protocol.DCPFrame{Ver: protocol.DCPVersion, Kind: protocol.DCPReply, Seq: seq, IntentID: intentID}, nil
		},
	}
	ds := &DaemonServer{hal: hal, nativeHandlers: NewNativeHandlerRegistry()}
	cap := invokeCap("set_relay", model.Implementation{Proxy: "uart", Protocol: "DCP"})

	_, err := ds.invokeCore(context.Background(), "shelf_01", cap, map[string]any{})
	if err != nil {
		t.Fatalf("invokeCore failed: %v", err)
	}
	if gotIntent != expected {
		t.Errorf("expected computed intent %d, got %d", expected, gotIntent)
	}
}

func TestInvokeCore_DCPErrorKind(t *testing.T) {
	hal := &MockHAL{
		TType: TransportDCP,
		SendDFn: func(ctx context.Context, seq byte, intentID uint16, params map[string]any) (*protocol.DCPFrame, error) {
			return &protocol.DCPFrame{Kind: protocol.DCPError, Payload: map[string]any{"error": "range violation"}}, nil
		},
	}
	ds := &DaemonServer{hal: hal, nativeHandlers: NewNativeHandlerRegistry()}
	cap := invokeCap("set_relay", model.Implementation{Proxy: "uart", Protocol: "DCP"})

	result, err := ds.invokeCore(context.Background(), "shelf_01", cap, map[string]any{})
	if err != nil {
		t.Fatalf("invokeCore failed: %v", err)
	}
	if result.Status != "dcp_error" {
		t.Errorf("expected dcp_error, got %s", result.Status)
	}
	if result.Result.(map[string]any)["error"] != "range violation" {
		t.Errorf("expected error message in result")
	}
}

func TestInvokeCore_DCPSendError(t *testing.T) {
	hal := &MockHAL{
		TType: TransportDCP,
		SendDFn: func(ctx context.Context, seq byte, intentID uint16, params map[string]any) (*protocol.DCPFrame, error) {
			return nil, errors.New("cbor encode failed")
		},
	}
	ds := &DaemonServer{hal: hal, nativeHandlers: NewNativeHandlerRegistry()}
	cap := invokeCap("set_relay", model.Implementation{Proxy: "uart", Protocol: "DCP"})

	_, err := ds.invokeCore(context.Background(), "shelf_01", cap, map[string]any{})
	if err == nil {
		t.Fatal("expected dcp send error")
	}
}

func TestInvokeCore_DCPSeqIncrements(t *testing.T) {
	var seqs []byte
	hal := &MockHAL{
		TType: TransportDCP,
		SendDFn: func(ctx context.Context, seq byte, intentID uint16, params map[string]any) (*protocol.DCPFrame, error) {
			seqs = append(seqs, seq)
			return &protocol.DCPFrame{Ver: protocol.DCPVersion, Kind: protocol.DCPReply, Seq: seq, IntentID: intentID}, nil
		},
	}
	ds := &DaemonServer{hal: hal, nativeHandlers: NewNativeHandlerRegistry()}
	cap := invokeCap("set_relay", model.Implementation{Proxy: "uart", Protocol: "DCP"})

	for i := 0; i < 3; i++ {
		if _, err := ds.invokeCore(context.Background(), "shelf_01", cap, map[string]any{}); err != nil {
			t.Fatalf("invokeCore failed: %v", err)
		}
	}
	if len(seqs) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(seqs))
	}
	if seqs[0] != 1 || seqs[1] != 2 || seqs[2] != 3 {
		t.Errorf("expected seq 1,2,3, got %d,%d,%d", seqs[0], seqs[1], seqs[2])
	}
}
