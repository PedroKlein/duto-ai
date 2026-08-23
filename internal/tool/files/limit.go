package files

import (
	"encoding/json"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

func fitsResult(value any, limit int) bool {
	encoded, err := json.Marshal(value)

	return err == nil && len(encoded) <= limit
}

func fitReadResult(data []byte, truncated bool, limit int) (*ReadResult, error) {
	result := &ReadResult{Content: string(data), Truncated: truncated}
	if fitsResult(result, limit) {
		return result, nil
	}

	result.Truncated = true
	if !fitsResult(&ReadResult{Truncated: true}, limit) {
		return nil, dtool.ErrToolResultLimit
	}

	low, high := 0, len(data)
	for low < high {
		middle := low + (high-low+1)/2
		result.Content = string(data[:middle])

		if fitsResult(result, limit) {
			low = middle
		} else {
			high = middle - 1
		}
	}

	result.Content = string(data[:low])
	if !fitsResult(result, limit) {
		return nil, dtool.ErrToolResultLimit
	}

	return result, nil
}
