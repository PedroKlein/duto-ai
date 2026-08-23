package compiler

import (
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/genai"

	"github.com/PedroKlein/duto-ai/internal/plan"
)

func toGenAISchema(source plan.Schema) *genai.Schema {
	schema := &genai.Schema{
		Type:     schemaType(source.Type),
		Required: slices.Clone(source.Required),
		Enum:     slices.Clone(source.Enum),
		Minimum:  source.Minimum,
		Maximum:  source.Maximum,
	}

	if source.MaxLength > 0 {
		value := int64(source.MaxLength)
		schema.MaxLength = &value
	}

	if source.MaxItems > 0 {
		value := int64(source.MaxItems)
		schema.MaxItems = &value
	}

	if source.Items != nil {
		schema.Items = toGenAISchema(*source.Items)
	}

	if len(source.Properties) > 0 {
		schema.Properties = make(map[string]*genai.Schema, len(source.Properties))

		schema.PropertyOrdering = make([]string, 0, len(source.Properties))
		for _, property := range source.Properties {
			schema.Properties[property.Name] = toGenAISchema(property.Schema)
			schema.PropertyOrdering = append(schema.PropertyOrdering, property.Name)
		}
	}

	return schema
}

func toJSONSchema(source plan.Schema) *jsonschema.Schema {
	schema := &jsonschema.Schema{
		Type:                 source.Type,
		Required:             slices.Clone(source.Required),
		Minimum:              source.Minimum,
		Maximum:              source.Maximum,
		AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
	}
	if len(source.Enum) > 0 {
		schema.Enum = make([]any, len(source.Enum))
		for i, value := range source.Enum {
			schema.Enum[i] = value
		}
	}

	if source.MaxLength > 0 {
		value := source.MaxLength
		schema.MaxLength = &value
	}

	if source.MaxItems > 0 {
		value := source.MaxItems
		schema.MaxItems = &value
	}

	if source.Items != nil {
		schema.Items = toJSONSchema(*source.Items)
	}

	if len(source.Properties) > 0 {
		schema.Properties = make(map[string]*jsonschema.Schema, len(source.Properties))

		schema.PropertyOrder = make([]string, 0, len(source.Properties))
		for _, property := range source.Properties {
			schema.Properties[property.Name] = toJSONSchema(property.Schema)
			schema.PropertyOrder = append(schema.PropertyOrder, property.Name)
		}
	}

	return schema
}

func schemaType(value string) genai.Type {
	switch value {
	case plan.TypeObject:
		return genai.TypeObject
	case plan.TypeArray:
		return genai.TypeArray
	case plan.TypeString:
		return genai.TypeString
	case plan.TypeInteger:
		return genai.TypeInteger
	case plan.TypeNumber:
		return genai.TypeNumber
	case plan.TypeBoolean:
		return genai.TypeBoolean
	default:
		return genai.TypeUnspecified
	}
}
