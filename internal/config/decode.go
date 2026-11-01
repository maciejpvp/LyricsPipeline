// Package config provides strict configuration boundary checks without application defaults.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// DecodeObject decodes a complete configuration object into a nonnil struct
// pointer. Every JSON-tagged field is required, including valid zero values.
func DecodeObject(data []byte, target any) error {
	value := reflect.ValueOf(target)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return errors.New("configuration target must be a nonnil pointer to a struct")
	}

	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("expected a configuration object, got null")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	schema := reflect.TypeOf(target).Elem()
	for i := 0; i < schema.NumField(); i++ {
		name := strings.Split(schema.Field(i).Tag.Get("json"), ",")[0]
		if name == "-" || !schema.Field(i).IsExported() {
			continue
		}
		if name == "" {
			name = schema.Field(i).Name
		}

		value, ok := fields[name]
		if !ok {
			return fmt.Errorf("missing required field %q; configure the complete object in Pulumi.yaml or the stack override", name)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("%s: null is not a valid setting", name)
		}
	}
	return nil
}
