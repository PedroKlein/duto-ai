package plan

import (
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/PedroKlein/duto-ai/internal/config"
)

const (
	TypeObject  = "object"
	TypeArray   = "array"
	TypeString  = "string"
	TypeInteger = "integer"
	TypeNumber  = "number"
	TypeBoolean = "boolean"

	maxSchemaDepth = 16
)

var ErrInvalidSchema = errors.New("invalid schema")

type Schema struct {
	Type       string     `json:"type"`
	Properties []Property `json:"properties,omitempty"`
	Required   []string   `json:"required,omitempty"`
	Items      *Schema    `json:"items,omitempty"`
	MaxLength  int        `json:"max_length,omitempty"`
	MaxItems   int        `json:"max_items,omitempty"`
	Enum       []string   `json:"enum,omitempty"`
	Minimum    *float64   `json:"minimum,omitempty"`
	Maximum    *float64   `json:"maximum,omitempty"`
}

type Property struct {
	Name   string `json:"name"`
	Schema Schema `json:"schema"`
}

func compileProperties(inputs map[string]config.Input) ([]Property, error) {
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}

	sort.Strings(names)

	properties := make([]Property, 0, len(names))
	for _, name := range names {
		schema, err := compileSchema(inputs[name].Schema, 0)
		if err != nil {
			return nil, fmt.Errorf("input %q: %w", name, err)
		}

		properties = append(properties, Property{Name: name, Schema: schema})
	}

	return properties, nil
}

func compileSchema(source config.Schema, depth int) (Schema, error) {
	if depth > maxSchemaDepth {
		return Schema{}, ErrInvalidSchema
	}

	result := Schema{
		Type:      source.Type,
		Required:  slices.Clone(source.Required),
		MaxLength: source.MaxLength,
		MaxItems:  source.MaxItems,
		Enum:      slices.Clone(source.Enum),
	}

	if source.HasMinimum {
		value := source.Minimum
		result.Minimum = &value
	}

	if source.HasMaximum {
		value := source.Maximum
		result.Maximum = &value
	}

	if source.Items != nil {
		items, err := compileSchema(*source.Items, depth+1)
		if err != nil {
			return Schema{}, err
		}

		result.Items = &items
	}

	names := make([]string, 0, len(source.Properties))
	for name := range source.Properties {
		names = append(names, name)
	}

	sort.Strings(names)

	result.Properties = make([]Property, 0, len(names))
	for _, name := range names {
		property, err := compileSchema(source.Properties[name], depth+1)
		if err != nil {
			return Schema{}, err
		}

		result.Properties = append(result.Properties, Property{Name: name, Schema: property})
	}

	if err := validateSchema(result); err != nil {
		return Schema{}, err
	}

	return result, nil
}

func validateSchema(schema Schema) error {
	switch schema.Type {
	case TypeObject:
		return validateObject(schema)
	case TypeArray:
		return validateArray(schema)
	case TypeString:
		return validateString(schema)
	case TypeInteger, TypeNumber:
		return validateNumber(schema)
	case TypeBoolean:
		return validateBoolean(schema)
	default:
		return ErrInvalidSchema
	}
}

func validateArray(schema Schema) error {
	if schema.Items == nil || schema.MaxItems <= 0 || len(schema.Properties) != 0 || schema.MaxLength != 0 {
		return ErrInvalidSchema
	}

	return nil
}

func validateString(schema Schema) error {
	if (schema.MaxLength <= 0 && len(schema.Enum) == 0) || schema.Items != nil || len(schema.Properties) != 0 || schema.MaxItems != 0 {
		return ErrInvalidSchema
	}

	return nil
}

func validateNumber(schema Schema) error {
	if schema.Items != nil || len(schema.Properties) != 0 || schema.MaxLength != 0 || schema.MaxItems != 0 || invalidNumericBounds(schema) {
		return ErrInvalidSchema
	}

	return nil
}

func validateBoolean(schema Schema) error {
	if schema.Items != nil || len(schema.Properties) != 0 || schema.MaxLength != 0 || schema.MaxItems != 0 || schema.Minimum != nil || schema.Maximum != nil || len(schema.Enum) != 0 {
		return ErrInvalidSchema
	}

	return nil
}

func validateObject(schema Schema) error {
	if schema.Items != nil || schema.MaxLength != 0 || schema.MaxItems != 0 || len(schema.Enum) != 0 || schema.Minimum != nil || schema.Maximum != nil {
		return ErrInvalidSchema
	}

	properties := propertiesByName(schema.Properties)
	if len(properties) != len(schema.Properties) {
		return ErrInvalidSchema
	}

	seenRequired := make(map[string]struct{}, len(schema.Required))
	for _, name := range schema.Required {
		if _, exists := properties[name]; !exists {
			return ErrInvalidSchema
		}

		if _, exists := seenRequired[name]; exists {
			return ErrInvalidSchema
		}

		seenRequired[name] = struct{}{}
	}

	return nil
}

func invalidNumericBounds(schema Schema) bool {
	return schema.Minimum != nil && schema.Maximum != nil && *schema.Minimum > *schema.Maximum
}

func validateOutcome(output Schema) error {
	if output.Type != TypeObject || !slices.Contains(output.Required, "outcome") {
		return ErrInvalidSchema
	}

	outcome, exists := propertiesByName(output.Properties)["outcome"]
	if !exists || outcome.Schema.Type != TypeString || len(outcome.Schema.Enum) == 0 {
		return ErrInvalidSchema
	}

	return nil
}

func propertiesByName(properties []Property) map[string]Property {
	byName := make(map[string]Property, len(properties))
	for _, property := range properties {
		byName[property.Name] = property
	}

	return byName
}

func assignable(source, target Schema) bool {
	if source.Type != target.Type && (source.Type != TypeInteger || target.Type != TypeNumber) {
		return false
	}

	switch source.Type {
	case TypeString:
		return stringAssignable(source, target)
	case TypeArray:
		return (target.MaxItems == 0 || source.MaxItems <= target.MaxItems) && source.Items != nil && target.Items != nil && assignable(*source.Items, *target.Items)
	case TypeObject:
		return objectAssignable(source, target)
	case TypeInteger, TypeNumber:
		return numericAssignable(source, target)
	case TypeBoolean:
		return true
	default:
		return false
	}
}

func stringAssignable(source, target Schema) bool {
	if target.MaxLength > 0 && source.MaxLength > target.MaxLength {
		return false
	}

	if len(target.Enum) == 0 {
		return true
	}

	if len(source.Enum) == 0 {
		return false
	}

	for _, value := range source.Enum {
		if !slices.Contains(target.Enum, value) {
			return false
		}
	}

	return true
}

func objectAssignable(source, target Schema) bool {
	sourceProperties := propertiesByName(source.Properties)
	for _, targetProperty := range target.Properties {
		sourceProperty, exists := sourceProperties[targetProperty.Name]
		if !exists || !assignable(sourceProperty.Schema, targetProperty.Schema) {
			return false
		}

		if slices.Contains(target.Required, targetProperty.Name) && !slices.Contains(source.Required, targetProperty.Name) {
			return false
		}
	}

	return true
}

func numericAssignable(source, target Schema) bool {
	if target.Minimum != nil && (source.Minimum == nil || *source.Minimum < *target.Minimum) {
		return false
	}

	return target.Maximum == nil || (source.Maximum != nil && *source.Maximum <= *target.Maximum)
}
