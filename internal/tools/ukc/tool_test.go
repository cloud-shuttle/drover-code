package ukc

import (
	"context"
	"encoding/json"
	"testing"
)

func TestTools_Execute(t *testing.T) {
	tests := []struct {
		name string
		tool interface {
			Execute(context.Context, json.RawMessage) (string, error)
		}
	}{
		{"Create", &Create{}},
		{"Delete", &Delete{}},
		{"DeleteAll", &DeleteAll{}},
		{"Exec", &Exec{}},
		{"List", &List{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Without manager
			_, err := tt.tool.Execute(context.Background(), json.RawMessage(`{}`))
			if err == nil {
				t.Errorf("expected error without manager")
			}

			// With manager, invalid input
			mgr := &Manager{}
			switch v := tt.tool.(type) {
			case *Create:
				v.M = mgr
			case *Delete:
				v.M = mgr
			case *DeleteAll:
				v.M = mgr
			case *Exec:
				v.M = mgr
			case *List:
				v.M = mgr
			}

			// With manager, invalid input
			_, err = tt.tool.Execute(context.Background(), json.RawMessage(`invalid json`))
			if err == nil && tt.name != "List" {
				t.Errorf("expected error with invalid json")
			}

			// With manager, missing required fields (for Create/Delete/Exec)
			if tt.name == "Create" || tt.name == "Delete" || tt.name == "Exec" {
				_, err = tt.tool.Execute(context.Background(), json.RawMessage(`{}`))
				if err == nil {
					t.Errorf("expected error with missing required fields")
				}
			}
		})
	}
}
