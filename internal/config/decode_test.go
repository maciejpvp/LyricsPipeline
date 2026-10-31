package config

import "testing"

func TestStrictObjectBoundary(t *testing.T) {
	type fields struct {
		Count int    `json:"count"`
		Name  string `json:"name"`
	}
	for _, raw := range []string{`null`, `[]`, `{}`, `{"count":0}`, `{"count":null,"name":"x"}`, `{"count":0,"name":"x","extra":1}`, `{"count":0,"name":"x"} {}`, `{"count":"0","name":"x"}`} {
		t.Run(raw, func(t *testing.T) {
			if err := DecodeObject([]byte(raw), &fields{}); err == nil {
				t.Fatal("invalid object accepted")
			}
		})
	}
	var result fields
	if err := DecodeObject([]byte(`{"count":0,"name":""}`), &result); err != nil {
		t.Fatalf("explicit zero values rejected: %v", err)
	}
	for _, target := range []any{nil, fields{}, (*fields)(nil), &map[string]int{}, new(int)} {
		if err := DecodeObject([]byte(`{}`), target); err == nil {
			t.Fatal("invalid decoding target accepted")
		}
	}
}
