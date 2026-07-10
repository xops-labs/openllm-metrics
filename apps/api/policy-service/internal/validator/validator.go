// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package validator validates policy documents against the F029 JSON Schema.
//
// This package handles ONLY structural / shape validation. It does not
// interpret rule semantics, evaluate budgets, order rules by precedence, or
// make enforcement decisions. All of those concerns belong to F030 and live in
// this repository. If you find yourself adding code that
// answers "should this request be allowed?" in this file, stop.
package validator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// Error is one structural validation finding. These map 1:1 to rows in
// control_plane.policy_validation_errors.
type Error struct {
	// Code is a stable machine-readable identifier (e.g. "missing_required",
	// "invalid_enum"). Callers should treat unknown codes as
	// "schema_violation".
	Code string `json:"code"`
	// Message is a human-readable description suitable for surfacing in the
	// UI / API response.
	Message string `json:"message"`
	// Path is a JSON Pointer (RFC 6901) at which the violation was detected,
	// e.g. "/rules/0/parameters/amount_usd". Empty for top-level violations.
	Path string `json:"path"`
}

// Validator compiles and applies the policy JSON Schema.
type Validator struct {
	schema *jsonschema.Schema
}

// New loads the JSON Schema from disk and returns a ready Validator.
func New(schemaPath string) (*Validator, error) {
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	sch, err := compiler.Compile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("validator: compile %s: %w", schemaPath, err)
	}
	return &Validator{schema: sch}, nil
}

// Validate runs the supplied JSON document through the compiled schema and
// returns the structured findings. A nil/empty slice means the document
// conforms to the schema. Validate never panics on invalid JSON; it returns
// a single Error with code="invalid_json" instead.
func (v *Validator) Validate(document []byte) []Error {
	var doc any
	if err := json.Unmarshal(document, &doc); err != nil {
		return []Error{{Code: "invalid_json", Message: err.Error()}}
	}
	if err := v.schema.Validate(doc); err != nil {
		var ve *jsonschema.ValidationError
		if asValidation(err, &ve) {
			return flatten(ve)
		}
		return []Error{{Code: "schema_violation", Message: err.Error()}}
	}
	return nil
}

// asValidation is a small type-assertion helper kept separate so flatten can
// stay simple. We don't pull in errors.As to avoid the extra import for one
// call site.
func asValidation(err error, target **jsonschema.ValidationError) bool {
	if ve, ok := err.(*jsonschema.ValidationError); ok {
		*target = ve
		return true
	}
	return false
}

// flatten walks the (possibly nested) validation error tree and produces a
// flat slice of Error structures, one per leaf violation. Leaf nodes are
// the most specific errors and are what operators actually need to fix.
func flatten(root *jsonschema.ValidationError) []Error {
	var out []Error
	var walk func(node *jsonschema.ValidationError)
	walk = func(node *jsonschema.ValidationError) {
		if node == nil {
			return
		}
		if len(node.Causes) == 0 {
			out = append(out, Error{
				Code:    deriveCode(node),
				Message: node.Message,
				Path:    node.InstanceLocation,
			})
			return
		}
		for _, child := range node.Causes {
			walk(child)
		}
	}
	walk(root)
	if len(out) == 0 {
		// Defensive: should not happen, but never return nil from a failed
		// schema validation.
		out = append(out, Error{
			Code:    "schema_violation",
			Message: root.Message,
			Path:    root.InstanceLocation,
		})
	}
	return out
}

// deriveCode produces a stable code from the underlying schema keyword.
// The santhosh-tekuri library exposes the keyword in KeywordLocation; we
// extract the trailing segment so callers can branch on it.
func deriveCode(node *jsonschema.ValidationError) string {
	kw := node.KeywordLocation
	if idx := strings.LastIndex(kw, "/"); idx >= 0 && idx+1 < len(kw) {
		kw = kw[idx+1:]
	}
	switch kw {
	case "required":
		return "missing_required"
	case "enum":
		return "invalid_enum"
	case "type":
		return "invalid_type"
	case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum":
		return "out_of_range"
	case "minLength", "maxLength":
		return "invalid_length"
	case "minItems", "maxItems":
		return "invalid_array_size"
	case "uniqueItems":
		return "duplicate_item"
	case "additionalProperties":
		return "unexpected_property"
	case "oneOf", "anyOf", "allOf":
		return "schema_alternative"
	case "format":
		return "invalid_format"
	case "":
		return "schema_violation"
	default:
		return kw
	}
}
