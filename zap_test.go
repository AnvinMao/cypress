package cypress

import (
	"encoding/json"
	"testing"

	"go.uber.org/zap"
)

func TestZapLogger(t *testing.T) {
	writer := NewBufferWriter()
	SetupLogger(LogLevelWarn, writer)
	zap.L().Info("test1")
	zap.L().Error("test2", zap.String("field1", "value1"))
	if len(writer.Buffer) != 1 {
		t.Error("only one log entry expected but got", len(writer.Buffer))
		return
	}

	type log struct {
		Message       string `json:"msg"`
		AdditionField string `json:"field1"`
	}

	l := log{}
	err := json.Unmarshal(writer.Buffer[0], &l)
	if err != nil {
		t.Error("bad log entry", err)
		return
	}

	if l.Message != "test2" || l.AdditionField != "value1" {
		t.Error("unexpected log entry", string(writer.Buffer[0]))
		return
	}
}
