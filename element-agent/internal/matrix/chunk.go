package matrix

import (
	"strings"

	"encoding/json/v2"
)

const envelopeReserve = 8192

func contentSize(body string) int {
	encoded, err := json.Marshal(messageContent(body), lenientJSON)
	if err != nil {
		return MaxEventBytes
	}
	return len(encoded)
}

func Chunk(body string) []string {
	if body == "" {
		return nil
	}
	budget := MaxEventBytes - envelopeReserve
	if contentSize(body) <= budget {
		return []string{body}
	}

	var chunks []string
	var current strings.Builder
	for _, segment := range splitKeepingNewlines(body) {
		for contentSize(segment) > budget {
			head := longestFitting(segment, budget)
			if current.Len() > 0 {
				chunks = append(chunks, current.String())
				current.Reset()
			}
			chunks = append(chunks, segment[:head])
			segment = segment[head:]
		}
		if current.Len() > 0 && contentSize(current.String()+segment) > budget {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		current.WriteString(segment)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

func splitKeepingNewlines(body string) []string {
	lines := strings.SplitAfter(body, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func longestFitting(s string, budget int) int {
	bounds := make([]int, 0, len(s)+1)
	for i := range s {
		bounds = append(bounds, i)
	}
	bounds = append(bounds, len(s))
	if len(bounds) < 2 {
		return len(s)
	}

	best := bounds[1]
	low, high := 1, len(bounds)-1
	for low <= high {
		mid := (low + high) / 2
		if contentSize(s[:bounds[mid]]) <= budget {
			best = bounds[mid]
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	return best
}
